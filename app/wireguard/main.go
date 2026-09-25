// Copyright (C) 2024  Mo3he
// SPDX-License-Identifier: GPL-3.0-or-later

// WireGuard userspace VPN for Axis cameras (ACAP): wireguard-go on a gVisor
// netstack, so no kernel TUN device, CAP_NET_ADMIN or root is needed.
// Inbound: forwarded camera ports and a SOCKS5 proxy on the tunnel IP.
// Outbound: HTTP CONNECT and SOCKS5 proxies on 127.0.0.1 that exit via the tunnel.
// Reloads the config written by config_updater.c on SIGUSR1 or file change.

package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"log/syslog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

const defaultConfigPath = "/usr/local/packages/wireguardconfig/config.txt"

// defaultForwardPorts are used when ForwardPorts has no valid entry.
var defaultForwardPorts = []int{80, 443, 554}

// maxForwardPorts caps how many listeners a config can ask for.
const maxForwardPorts = 16

// socks5Port is the SOCKS5 proxy port on the WireGuard interface (not host network).
const socks5Port = 1080

// netstack MTU bounds: 1420 fits the WireGuard overhead on a 1500-byte path,
// 576 is the IPv4 minimum reassembly buffer size.
const (
	defaultMTU = 1420
	minMTU     = 576
	maxMTU     = 1500
)

// Config holds parsed WireGuard settings from the config file.
type Config struct {
	PrivateKey         string
	ListenPort         string
	Endpoint           string
	PeerPubKey         string
	AllowedIPs         string
	ClientIP           string
	HTTPProxyPort      string
	OutboundSOCKS5Port string
	ForwardPorts       string
	MTU                string
}

// parseMTU returns defaultMTU for empty, unparseable or out-of-range values.
func parseMTU(value string) int {
	mtu, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || mtu < minMTU || mtu > maxMTU {
		return defaultMTU
	}
	return mtu
}

// parseForwardPorts returns up to maxForwardPorts unique valid ports, or the defaults if none.
func parseForwardPorts(value string) []int {
	ports := make([]int, 0, maxForwardPorts)
	seen := make(map[int]bool, maxForwardPorts)
	for _, field := range strings.Split(value, ",") {
		port, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
		if len(ports) == maxForwardPorts {
			break
		}
	}
	if len(ports) == 0 {
		return defaultForwardPorts
	}
	return ports
}

//nolint:gocyclo // one branch per config key; splitting it would not make it clearer
func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	cfg := &Config{
		AllowedIPs:         "0.0.0.0/0",
		ClientIP:           "10.0.0.2/24",
		HTTPProxyPort:      "8080",
		OutboundSOCKS5Port: "1080",
		ForwardPorts:       "80,443,554",
		MTU:                strconv.Itoa(defaultMTU),
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "private_key":
			cfg.PrivateKey = val
		case "listen_port":
			cfg.ListenPort = val
		case "endpoint":
			cfg.Endpoint = val
		case "peer_public_key":
			cfg.PeerPubKey = val
		case "allowed_ips":
			cfg.AllowedIPs = val
		case "client_ip":
			cfg.ClientIP = val
		case "http_proxy_port":
			cfg.HTTPProxyPort = val
		case "outbound_socks5_port":
			cfg.OutboundSOCKS5Port = val
		case "forward_ports":
			cfg.ForwardPorts = val
		case "mtu":
			cfg.MTU = val
		}
	}
	return cfg, scanner.Err()
}

// base64ToHex converts a base64 WireGuard key to the hex form the UAPI expects.
func base64ToHex(b64 string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func buildUAPI(cfg *Config) (string, error) {
	privHex, err := base64ToHex(cfg.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("invalid private key: %w", err)
	}
	peerHex, err := base64ToHex(cfg.PeerPubKey)
	if err != nil {
		return "", fmt.Errorf("invalid peer public key: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "private_key=%s\n", privHex)
	if cfg.ListenPort != "" {
		fmt.Fprintf(&sb, "listen_port=%s\n", cfg.ListenPort)
	}
	fmt.Fprintf(&sb, "public_key=%s\n", peerHex)
	for _, cidr := range strings.Split(cfg.AllowedIPs, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr != "" {
			fmt.Fprintf(&sb, "allowed_ip=%s\n", cidr)
		}
	}
	if cfg.Endpoint != "" {
		// The UAPI only accepts IP:port, so resolve hostnames here.
		host, port, err := net.SplitHostPort(cfg.Endpoint)
		if err != nil {
			return "", fmt.Errorf("invalid endpoint %q: %w", cfg.Endpoint, err)
		}
		if net.ParseIP(host) == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			addrs, err := net.DefaultResolver.LookupHost(ctx, host)
			cancel()
			if err != nil {
				return "", fmt.Errorf("resolve endpoint hostname %q: %w", host, err)
			}
			if len(addrs) == 0 {
				return "", fmt.Errorf("no addresses for endpoint hostname %q", host)
			}
			host = addrs[0]
		}
		fmt.Fprintf(&sb, "endpoint=%s\n", net.JoinHostPort(host, port))
	}
	fmt.Fprintf(&sb, "persistent_keepalive_interval=25\n")
	return sb.String(), nil
}

// tunnel holds a running WireGuard+netstack instance and all its proxies.
type tunnel struct {
	dev    *device.Device
	tnet   *netstack.Net
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func (t *tunnel) close() {
	close(t.stopCh)
	t.wg.Wait()
	t.dev.Close()
}

// parsePort returns configured as a port number, or defaultPort if empty or invalid.
func parsePort(configured string, defaultPort int) int {
	if configured != "" {
		var p int
		if _, err := fmt.Sscan(configured, &p); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	return defaultPort
}

func startTunnel(cfg *Config) (*tunnel, error) {
	prefix, err := netip.ParsePrefix(cfg.ClientIP)
	if err != nil {
		return nil, fmt.Errorf("invalid client IP %q: %w", cfg.ClientIP, err)
	}
	localAddr := prefix.Addr()

	mtu := parseMTU(cfg.MTU)
	tun, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{localAddr},
		[]netip.Addr{},
		mtu,
	)
	if err != nil {
		return nil, fmt.Errorf("create netstack TUN: %w", err)
	}

	logger := device.NewLogger(device.LogLevelError, "wireguard: ")
	dev := device.NewDevice(tun, conn.NewStdNetBind(), logger)

	uapi, err := buildUAPI(cfg)
	if err != nil {
		dev.Close()
		return nil, err
	}
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, fmt.Errorf("ipc set: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("device up: %w", err)
	}

	t := &tunnel{dev: dev, tnet: tnet, stopCh: make(chan struct{})}

	httpPort := parsePort(cfg.HTTPProxyPort, 8080)
	socks5OutPort := parsePort(cfg.OutboundSOCKS5Port, 1080)

	// The UI derives connected/disconnected from these handshake log lines.
	t.wg.Add(1)
	go t.runHandshakeMonitor()

	for _, port := range parseForwardPorts(cfg.ForwardPorts) {
		t.wg.Add(1)
		go t.runTCPProxy(localAddr, port, fmt.Sprintf("127.0.0.1:%d", port))
	}

	t.wg.Add(1)
	go t.runSOCKS5(localAddr, socks5Port)

	t.wg.Add(1)
	go t.runHTTPProxy(httpPort)

	t.wg.Add(1)
	go t.runOutboundSOCKS5(socks5OutPort)

	return t, nil
}

// runHandshakeMonitor logs handshake ok/lost every 15 s. A handshake older than
// 3 min (WireGuard's REJECT_AFTER_TIME; healthy peers rekey every 2 min) is lost.
//
//nolint:gocyclo
func (t *tunnel) runHandshakeMonitor() {
	defer t.wg.Done()

	const pollInterval = 15 * time.Second
	const handshakeMaxAge = 3 * time.Minute

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	wasConnected := false
	firstPoll := true

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
		}

		buf := &strings.Builder{}
		if err := t.dev.IpcGetOperation(buf); err != nil {
			slog.Warn("handshake monitor: IpcGet failed", "err", err)
			continue
		}

		var lastHS time.Time
		for _, line := range strings.Split(buf.String(), "\n") {
			if val, ok := strings.CutPrefix(line, "last_handshake_time_sec="); ok {
				val = strings.TrimSpace(val)
				if val != "0" {
					var sec int64
					if _, err := fmt.Sscan(val, &sec); err == nil && sec > 0 {
						lastHS = time.Unix(sec, 0)
					}
				}
				break
			}
		}

		connected := !lastHS.IsZero() && time.Since(lastHS) < handshakeMaxAge
		if connected && (!wasConnected || firstPoll) {
			slog.Info("WireGuard handshake ok", "last_handshake", lastHS.Format(time.RFC3339))
		} else if !connected && (wasConnected || firstPoll) {
			slog.Warn("WireGuard handshake lost")
		}
		wasConnected = connected
		firstPoll = false
	}
}

// runTCPProxy listens on localAddr:port inside the WireGuard netstack and
// forwards each accepted connection to dstAddr on the host.
func (t *tunnel) runTCPProxy(localAddr netip.Addr, port int, dstAddr string) {
	defer t.wg.Done()

	if port < 0 || port > 65535 {
		slog.Error("invalid port", "port", port)
		return
	}
	listenAddr := net.TCPAddrFromAddrPort(netip.AddrPortFrom(localAddr, uint16(port)))
	ln, err := t.tnet.ListenTCP(listenAddr)
	if err != nil {
		slog.Error("proxy listen", "port", port, "err", err)
		return
	}
	go func() { <-t.stopCh; _ = ln.Close() }()
	slog.Info("transparent forwarder ready", "listen", listenAddr, "dst", dstAddr)

	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
				slog.Error("proxy accept", "port", port, "err", err)
				time.Sleep(time.Second)
				continue
			}
		}
		go relay(c, dstAddr)
	}
}

// relay opens a connection to dst and bidirectionally copies data.
func relay(src net.Conn, dst string) {
	defer src.Close() //nolint:errcheck
	dstConn, err := net.DialTimeout("tcp", dst, 10*time.Second)
	if err != nil {
		return
	}
	defer dstConn.Close() //nolint:errcheck
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(dstConn, src); done <- struct{}{} }() //nolint:errcheck
	go func() { _, _ = io.Copy(src, dstConn); done <- struct{}{} }() //nolint:errcheck
	<-done
}

// runSOCKS5 serves SOCKS5 CONNECT on the tunnel IP. Only the requested port is
// honoured; the destination is always 127.0.0.1 so it cannot be an open proxy.
func (t *tunnel) runSOCKS5(localAddr netip.Addr, port int) {
	defer t.wg.Done()

	if port < 0 || port > 65535 {
		slog.Error("invalid port", "port", port)
		return
	}
	listenAddr := net.TCPAddrFromAddrPort(netip.AddrPortFrom(localAddr, uint16(port)))
	ln, err := t.tnet.ListenTCP(listenAddr)
	if err != nil {
		slog.Error("socks5 listen", "err", err)
		return
	}
	go func() { <-t.stopCh; _ = ln.Close() }()
	slog.Info("SOCKS5 proxy ready", "addr", listenAddr)

	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
				slog.Error("socks5 accept", "err", err)
				time.Sleep(time.Second)
				continue
			}
		}
		go handleSOCKS5(c)
	}
}

// handleSOCKS5 handles one RFC 1928 CONNECT request, always dialing 127.0.0.1.
func handleSOCKS5(c net.Conn) {
	defer c.Close()                                     //nolint:errcheck
	_ = c.SetDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck

	buf := make([]byte, 257)

	// Greeting: VER NMETHODS METHODS
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return
	}
	if buf[0] != 0x05 {
		return
	}
	nmethods := int(buf[1])
	if _, err := io.ReadFull(c, buf[:nmethods]); err != nil {
		return
	}
	// Reply: no authentication required
	_, _ = c.Write([]byte{0x05, 0x00}) //nolint:errcheck

	// Request: VER CMD RSV ATYP ...
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x01 { // only CONNECT
		_, _ = c.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) //nolint:errcheck // command not supported
		return
	}

	var port uint16
	switch buf[3] {
	case 0x01: // IPv4
		if _, err := io.ReadFull(c, buf[:6]); err != nil {
			return
		}
		port = binary.BigEndian.Uint16(buf[4:6])
	case 0x03: // domain name
		if _, err := io.ReadFull(c, buf[:1]); err != nil {
			return
		}
		nameLen := int(buf[0])
		if _, err := io.ReadFull(c, buf[:nameLen+2]); err != nil {
			return
		}
		port = binary.BigEndian.Uint16(buf[nameLen : nameLen+2])
	case 0x04: // IPv6
		if _, err := io.ReadFull(c, buf[:18]); err != nil {
			return
		}
		port = binary.BigEndian.Uint16(buf[16:18])
	default:
		_, _ = c.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) //nolint:errcheck // address type not supported
		return
	}

	dst := fmt.Sprintf("127.0.0.1:%d", port)
	_ = c.SetDeadline(time.Time{}) //nolint:errcheck

	dstConn, err := net.DialTimeout("tcp", dst, 10*time.Second)
	if err != nil {
		_, _ = c.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) //nolint:errcheck // host unreachable
		return
	}
	defer dstConn.Close() //nolint:errcheck

	// Success reply
	_, _ = c.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, //nolint:errcheck
		byte(port >> 8), byte(port)}) //nolint:gosec

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(dstConn, c); done <- struct{}{} }() //nolint:errcheck
	go func() { _, _ = io.Copy(c, dstConn); done <- struct{}{} }() //nolint:errcheck
	<-done
}

// runHTTPProxy serves an HTTP proxy (CONNECT and plain) on 127.0.0.1:port that
// exits via the tunnel; point the camera's global proxy at it.
func (t *tunnel) runHTTPProxy(port int) {
	defer t.wg.Done()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		slog.Error("http proxy listen failed — port is in use, change HTTPProxyPort in settings", "port", port, "err", err)
		return
	}
	go func() { <-t.stopCh; _ = ln.Close() }()
	slog.Info("HTTP CONNECT proxy ready", "addr", ln.Addr())

	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
				slog.Error("http proxy accept", "err", err)
				time.Sleep(time.Second)
				continue
			}
		}
		go t.handleHTTPProxy(c)
	}
}

// dialViaWG resolves with the host DNS (the netstack has none) and dials through the tunnel.
func (t *tunnel) dialViaWG(ctx context.Context, hostport string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %s", host)
	}
	return t.tnet.DialContext(ctx, "tcp", net.JoinHostPort(addrs[0], port))
}

// handleHTTPProxy serves one client connection from the HTTP CONNECT proxy.
func (t *tunnel) handleHTTPProxy(c net.Conn) {
	defer c.Close()                                     //nolint:errcheck
	_ = c.SetDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck

	rd := bufio.NewReader(c)

	requestLine, err := rd.ReadString('\n')
	if err != nil {
		return
	}
	parts := strings.SplitN(strings.TrimSpace(requestLine), " ", 3)
	if len(parts) != 3 {
		return
	}
	method, target, httpVer := parts[0], parts[1], parts[2]

	if strings.ToUpper(method) == "CONNECT" {
		// HTTPS tunnel: drain headers, reply 200, then relay raw bytes.
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			if strings.TrimSpace(line) == "" {
				break
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		upstream, err := t.dialViaWG(ctx, target)
		cancel()
		if err != nil {
			_, _ = fmt.Fprintf(c, "%s 502 Bad Gateway\r\n\r\n", httpVer)
			return
		}
		defer upstream.Close() //nolint:errcheck

		_ = c.SetDeadline(time.Time{}) //nolint:errcheck
		_, _ = fmt.Fprintf(c, "%s 200 Connection established\r\n\r\n", httpVer)

		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(upstream, rd); done <- struct{}{} }() //nolint:errcheck
		go func() { _, _ = io.Copy(c, upstream); done <- struct{}{} }()  //nolint:errcheck
		<-done
	} else {
		// Plain HTTP: rewrite absolute URI to relative, forward to remote host.
		u, err := url.Parse(target)
		if err != nil {
			_, _ = fmt.Fprintf(c, "%s 400 Bad Request\r\n\r\n", httpVer)
			return
		}
		host := u.Host
		if !strings.Contains(host, ":") {
			host += ":80"
		}
		relativePath := u.RequestURI()

		var headerLines []string
		for {
			line, readErr := rd.ReadString('\n')
			if readErr != nil {
				return
			}
			headerLines = append(headerLines, line)
			if strings.TrimSpace(line) == "" {
				break
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		upstream, err := t.dialViaWG(ctx, host)
		cancel()
		if err != nil {
			_, _ = fmt.Fprintf(c, "%s 502 Bad Gateway\r\n\r\n", httpVer)
			return
		}
		defer upstream.Close() //nolint:errcheck

		_ = c.SetDeadline(time.Time{}) //nolint:errcheck
		_, _ = fmt.Fprintf(upstream, "%s %s %s\r\n", method, relativePath, httpVer)
		for _, h := range headerLines {
			_, _ = upstream.Write([]byte(h)) //nolint:errcheck
		}

		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(upstream, rd); done <- struct{}{} }() //nolint:errcheck
		go func() { _, _ = io.Copy(c, upstream); done <- struct{}{} }()  //nolint:errcheck
		<-done
	}
}

// runOutboundSOCKS5 serves SOCKS5 on 127.0.0.1:port for camera services (e.g.
// MQTT) whose traffic should exit via the tunnel.
func (t *tunnel) runOutboundSOCKS5(port int) {
	defer t.wg.Done()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		slog.Error("outbound socks5 listen failed — port is in use, change OutboundSOCKS5Port in settings", "port", port, "err", err)
		return
	}
	go func() { <-t.stopCh; _ = ln.Close() }()
	slog.Info("Outbound SOCKS5 proxy ready", "addr", ln.Addr())

	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
				slog.Error("outbound socks5 accept", "err", err)
				time.Sleep(time.Second)
				continue
			}
		}
		go t.handleOutboundSOCKS5(c)
	}
}

// handleOutboundSOCKS5 handles one RFC 1928 CONNECT request, dialing via WireGuard.
func (t *tunnel) handleOutboundSOCKS5(c net.Conn) {
	defer c.Close()                                     //nolint:errcheck
	_ = c.SetDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck

	buf := make([]byte, 257)

	// Greeting
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return
	}
	if buf[0] != 0x05 {
		return
	}
	nmethods := int(buf[1])
	if _, err := io.ReadFull(c, buf[:nmethods]); err != nil {
		return
	}
	_, _ = c.Write([]byte{0x05, 0x00}) //nolint:errcheck // no auth required

	// Request
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x01 {
		_, _ = c.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) //nolint:errcheck
		return
	}

	var hostport string
	switch buf[3] {
	case 0x01: // IPv4
		if _, err := io.ReadFull(c, buf[:6]); err != nil {
			return
		}
		ip := net.IP(buf[:4]).String()
		port := binary.BigEndian.Uint16(buf[4:6])
		hostport = net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	case 0x03: // domain name
		if _, err := io.ReadFull(c, buf[:1]); err != nil {
			return
		}
		nameLen := int(buf[0])
		if _, err := io.ReadFull(c, buf[:nameLen+2]); err != nil {
			return
		}
		host := string(buf[:nameLen])
		port := binary.BigEndian.Uint16(buf[nameLen : nameLen+2])
		hostport = net.JoinHostPort(host, fmt.Sprintf("%d", port))
	case 0x04: // IPv6
		if _, err := io.ReadFull(c, buf[:18]); err != nil {
			return
		}
		ip := net.IP(buf[:16]).String()
		port := binary.BigEndian.Uint16(buf[16:18])
		hostport = net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	default:
		_, _ = c.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) //nolint:errcheck
		return
	}

	_ = c.SetDeadline(time.Time{}) //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	upstream, err := t.dialViaWG(ctx, hostport)
	cancel()
	if err != nil {
		_, _ = c.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) //nolint:errcheck
		return
	}
	defer upstream.Close() //nolint:errcheck

	// Success
	_, _ = c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) //nolint:errcheck

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, c); done <- struct{}{} }() //nolint:errcheck
	go func() { _, _ = io.Copy(c, upstream); done <- struct{}{} }() //nolint:errcheck
	<-done
}

// multiHandler logs to stderr and syslog: stderr reaches the journal on ACAP 4,
// but the ACAP 3 init wrapper drops it, so systemlog.cgi needs syslog.
type multiHandler struct {
	stderr *slog.TextHandler
	sys    *syslog.Writer // nil if syslog unavailable
}

func newMultiHandler(w *os.File) *multiHandler {
	h := &multiHandler{stderr: slog.NewTextHandler(w, nil)}
	sw, err := syslog.New(syslog.LOG_USER|syslog.LOG_INFO, "wireguardconfig")
	if err == nil {
		h.sys = sw
	}
	return h
}

func (h *multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.stderr.Enabled(ctx, l)
}
func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.stderr.WithAttrs(attrs)
}
func (h *multiHandler) WithGroup(name string) slog.Handler {
	return h.stderr.WithGroup(name)
}
func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	_ = h.stderr.Handle(ctx, r) //nolint:errcheck
	if h.sys != nil {
		msg := r.Message
		r.Attrs(func(a slog.Attr) bool {
			msg += " " + a.Key + "=" + fmt.Sprint(a.Value.Any())
			return true
		})
		switch {
		case r.Level >= slog.LevelError:
			_ = h.sys.Err(msg) //nolint:errcheck
		case r.Level >= slog.LevelWarn:
			_ = h.sys.Warning(msg) //nolint:errcheck
		default:
			_ = h.sys.Info(msg) //nolint:errcheck
		}
	}
	return nil
}

func main() {
	configPath := defaultConfigPath
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	slog.SetDefault(slog.New(newMultiHandler(os.Stderr)))
	slog.Info("wireguard-userspace starting", "config", configPath) //nolint:gosec

	app := &appState{configPath: configPath}

	// Seed lastMod so the first 30 s tick doesn't restart the tunnel before the
	// 25 s keepalive has started the handshake.
	if info, err := os.Stat(configPath); err == nil { //nolint:gosec
		app.lastMod = info.ModTime()
	}

	app.reload()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGUSR1:
				slog.Info("SIGUSR1 received — reloading config")
				app.reload()
			default:
				slog.Info("shutting down")
				app.mu.Lock()
				if app.current != nil {
					app.current.close()
				}
				app.mu.Unlock()
				return
			}
		case <-ticker.C:
			info, err := os.Stat(configPath) //nolint:gosec
			if err == nil && info.ModTime().After(app.lastMod) {
				app.lastMod = info.ModTime()
				slog.Info("config file changed — reloading")
				app.reload()
			} else if app.retryPending() {
				slog.Info("tunnel not up, retrying")
				app.reload()
			}
		}
	}
}

// ── app state ────────────────────────────────────────────────────────────────

type appState struct {
	mu          sync.Mutex
	current     *tunnel
	configPath  string
	lastMod     time.Time
	startFailed bool
}

// retryPending reports whether the last attempt to bring the tunnel up failed
// and left nothing running. An incomplete config is not a failure, so a camera
// that has not been configured yet never enters the retry loop.
func (a *appState) retryPending() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current == nil && a.startFailed
}

func (a *appState) setStartFailed(failed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startFailed = failed
}

func (a *appState) reload() {
	cfg, err := loadConfig(a.configPath)
	if err != nil {
		slog.Error("load config", "err", err)
		a.setStartFailed(true)
		return
	}
	if cfg.PrivateKey == "" || cfg.PeerPubKey == "" {
		slog.Info("config incomplete — waiting for keys to be set")
		a.setStartFailed(false)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.current != nil {
		a.current.close()
		a.current = nil
	}

	t, err := startTunnel(cfg)
	if err != nil {
		slog.Error("start tunnel", "err", err)
		a.startFailed = true
		return
	}
	a.current = t
	a.startFailed = false
	slog.Info("WireGuard tunnel up",
		"ip", cfg.ClientIP,
		"endpoint", cfg.Endpoint,
		"mtu", parseMTU(cfg.MTU),
		"socks5_port", socks5Port,
		"http_proxy_port", cfg.HTTPProxyPort,
		"outbound_socks5_port", cfg.OutboundSOCKS5Port,
	)
}
