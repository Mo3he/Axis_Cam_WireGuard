// Copyright (C) 2024  Mo3he
// SPDX-License-Identifier: GPL-3.0-or-later

// WireGuard userspace VPN for Axis cameras (ACAP).
// Runs entirely in userspace via wireguard-go + gVisor netstack — no kernel TUN
// device, no CAP_NET_ADMIN, no root required.
//
// Network access model:
//   - Transparent TCP port forwarding for common camera ports (80, 443, 554)
//   - SOCKS5 proxy on port 1080 for full access to any camera port (WireGuard peer → camera)
//   - HTTP CONNECT proxy on port 8080 (set http://127.0.0.1:8080 in camera global proxy settings)
//   - Outbound SOCKS5 on localhost:1080 — camera services (e.g. MQTT) → WireGuard → internet
//
// Config is read from CONFIG_FILE (written by the C ACAP bridge via axparameter).
// Reloads on SIGUSR1 or when the config file modification time changes.

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
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

const defaultConfigPath = "/usr/local/packages/wireguardconfig/config.txt"

// transparentPorts are forwarded directly: WireGuard-IP:port → 127.0.0.1:port.
var transparentPorts = []int{80, 443, 554}

// socks5Port is the SOCKS5 proxy port on the WireGuard interface (not host network).
const socks5Port = 1080

// Config holds parsed WireGuard settings from the config file.
type Config struct {
	PrivateKey          string
	ListenPort          string
	Endpoint            string
	PeerPubKey          string
	AllowedIPs          string
	ClientIP            string
	HTTPProxyPort       string
	OutboundSOCKS5Port  string
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &Config{
		AllowedIPs:         "0.0.0.0/0",
		ClientIP:           "10.0.0.2/24",
		HTTPProxyPort:      "8080",
		OutboundSOCKS5Port: "1080",
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
		}
	}
	return cfg, scanner.Err()
}

// base64ToHex converts a standard base64-encoded WireGuard key to lowercase hex,
// which is the format expected by the wireguard-go UAPI.
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
	fmt.Fprintf(&sb, "allowed_ip=%s\n", cfg.AllowedIPs)
	if cfg.Endpoint != "" {
		// The WireGuard UAPI requires an IP:port endpoint, not a hostname.
		// Resolve the hostname here so that DNS names are accepted.
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

// portCandidates builds a list of ports to try, starting from the user's
// configured port (parsed from a string).  Falls back to fallbackStart if
// the string is empty or not a valid number.  n consecutive ports are
// returned so there are always fallbacks in case of transient conflicts.
func portCandidates(configured string, fallbackStart int, n int) []int {
	start := fallbackStart
	if configured != "" {
		var p int
		if _, err := fmt.Sscan(configured, &p); err == nil && p > 0 && p < 65536 {
			start = p
		}
	}
	out := make([]int, n)
	for i := range out {
		out[i] = start + i
	}
	return out
}

func startTunnel(cfg *Config) (*tunnel, error) {
	prefix, err := netip.ParsePrefix(cfg.ClientIP)
	if err != nil {
		return nil, fmt.Errorf("invalid client IP %q: %w", cfg.ClientIP, err)
	}
	localAddr := prefix.Addr()

	tun, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{localAddr},
		[]netip.Addr{},
		1420,
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

	// Build port candidate lists starting from the user-configured port.
	// If that port is busy (e.g. another ACAP) the next 3 are tried as a
	// fallback, but the user can always avoid the conflict by picking a
	// different port in the settings.
	httpCandidates := portCandidates(cfg.HTTPProxyPort, 8080, 4)
	socks5Candidates := portCandidates(cfg.OutboundSOCKS5Port, 1080, 4)

	// Handshake monitor: polls WireGuard peer state every 15 s and logs
	// "WireGuard handshake ok" / "WireGuard handshake lost" so the UI can
	// show true connected/disconnected status based on cryptographic proof,
	// not just whether the process started.
	t.wg.Add(1)
	go t.runHandshakeMonitor()

	// Transparent forwarders for common camera ports
	for _, port := range transparentPorts {
		t.wg.Add(1)
		go t.runTCPProxy(localAddr, port, fmt.Sprintf("127.0.0.1:%d", port))
	}

	// SOCKS5 proxy for full access to any camera port
	t.wg.Add(1)
	go t.runSOCKS5(localAddr, socks5Port)

	// HTTP CONNECT proxy on localhost so the camera can route its own outbound
	// HTTP/HTTPS traffic through WireGuard via the global proxy setting.
	t.wg.Add(1)
	go t.runHTTPProxy(httpCandidates)

	// Outbound SOCKS5 on localhost so camera services (e.g. MQTT) can route
	// connections through WireGuard. Configure those services to use
	// SOCKS5 127.0.0.1:<port> (first free port from candidates).
	t.wg.Add(1)
	go t.runOutboundSOCKS5(socks5Candidates)

	return t, nil
}

// runHandshakeMonitor polls the WireGuard device every 15 s via IpcGet and
// logs whether the peer has completed a recent handshake.  A handshake is
// considered "ok" if it occurred within the last 3 minutes (2× the 90-second
// WireGuard handshake expiry; keepalive is 25 s so a healthy tunnel will
// re-handshake well within that window).
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

		// IpcGet returns the UAPI device state.  We look for:
		//   last_handshake_time_sec=<unix-seconds>
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

	listenAddr := net.TCPAddrFromAddrPort(netip.AddrPortFrom(localAddr, uint16(port)))
	ln, err := t.tnet.ListenTCP(listenAddr)
	if err != nil {
		slog.Error("proxy listen", "port", port, "err", err)
		return
	}
	go func() { <-t.stopCh; ln.Close() }()

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
	defer src.Close()
	dstConn, err := net.DialTimeout("tcp", dst, 10*time.Second)
	if err != nil {
		return
	}
	defer dstConn.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(dstConn, src); done <- struct{}{} }()
	go func() { io.Copy(src, dstConn); done <- struct{}{} }()
	<-done
}

// runSOCKS5 listens on the WireGuard IP and handles SOCKS5 CONNECT requests,
// forwarding each to 127.0.0.1:<requested-port> on the local host.
// Only the port from the client's request is honoured; the destination address
// is always localhost so this proxy cannot be used as a general open proxy.
func (t *tunnel) runSOCKS5(localAddr netip.Addr, port int) {
	defer t.wg.Done()

	listenAddr := net.TCPAddrFromAddrPort(netip.AddrPortFrom(localAddr, uint16(port)))
	ln, err := t.tnet.ListenTCP(listenAddr)
	if err != nil {
		slog.Error("socks5 listen", "err", err)
		return
	}
	go func() { <-t.stopCh; ln.Close() }()
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

// handleSOCKS5 implements the SOCKS5 server-side handshake (RFC 1928).
// Only CONNECT is supported; the destination host is always replaced with
// 127.0.0.1 so the proxy only reaches local camera services.
func handleSOCKS5(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(30 * time.Second))

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
	c.Write([]byte{0x05, 0x00})

	// Request: VER CMD RSV ATYP ...
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x01 { // only CONNECT
		c.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // command not supported
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
		c.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // address type not supported
		return
	}

	dst := fmt.Sprintf("127.0.0.1:%d", port)
	c.SetDeadline(time.Time{})

	dstConn, err := net.DialTimeout("tcp", dst, 10*time.Second)
	if err != nil {
		c.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // host unreachable
		return
	}
	defer dstConn.Close()

	// Success reply
	c.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1,
		byte(port >> 8), byte(port)})

	done := make(chan struct{}, 2)
	go func() { io.Copy(dstConn, c); done <- struct{}{} }()
	go func() { io.Copy(c, dstConn); done <- struct{}{} }()
	<-done
}

// runHTTPProxy listens on 127.0.0.1:port (host network) and handles HTTP CONNECT
// requests by tunnelling the connection through the WireGuard netstack. Plain
// HTTP requests (non-CONNECT) are also forwarded. Set the camera's global proxy
// to http://127.0.0.1:<port> to route outbound camera traffic through the VPN.
func (t *tunnel) runHTTPProxy(candidates []int) {
	defer t.wg.Done()

	var ln net.Listener
	for _, port := range candidates {
		var err error
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
		slog.Warn("http proxy port unavailable, trying next", "port", port, "err", err)
	}
	if ln == nil {
		slog.Error("http proxy listen", "candidates", candidates, "err", "all ports in use")
		return
	}
	go func() { <-t.stopCh; ln.Close() }()
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

// dialViaWG resolves hostport using the host OS DNS resolver, then connects to
// the resulting IP through the WireGuard netstack so the traffic exits via VPN.
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
	defer c.Close()
	c.SetDeadline(time.Now().Add(30 * time.Second))

	rd := bufio.NewReader(c)

	// Read the request line: METHOD target HTTP/x.x
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
			fmt.Fprintf(c, "%s 502 Bad Gateway\r\n\r\n", httpVer)
			return
		}
		defer upstream.Close()

		c.SetDeadline(time.Time{})
		fmt.Fprintf(c, "%s 200 Connection established\r\n\r\n", httpVer)

		done := make(chan struct{}, 2)
		go func() { io.Copy(upstream, rd); done <- struct{}{} }()
		go func() { io.Copy(c, upstream); done <- struct{}{} }()
		<-done
	} else {
		// Plain HTTP: rewrite absolute URI to relative, forward to remote host.
		u, err := url.Parse(target)
		if err != nil {
			fmt.Fprintf(c, "%s 400 Bad Request\r\n\r\n", httpVer)
			return
		}
		host := u.Host
		if !strings.Contains(host, ":") {
			host += ":80"
		}
		relativePath := u.RequestURI()

		// Collect headers so we can forward them verbatim.
		var headerLines []string
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
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
			fmt.Fprintf(c, "%s 502 Bad Gateway\r\n\r\n", httpVer)
			return
		}
		defer upstream.Close()

		c.SetDeadline(time.Time{})
		fmt.Fprintf(upstream, "%s %s %s\r\n", method, relativePath, httpVer)
		for _, h := range headerLines {
			upstream.Write([]byte(h))
		}

		done := make(chan struct{}, 2)
		go func() { io.Copy(upstream, rd); done <- struct{}{} }()
		go func() { io.Copy(c, upstream); done <- struct{}{} }()
		<-done
	}
}

// runOutboundSOCKS5 listens on 127.0.0.1:port (host network) and handles SOCKS5
// CONNECT requests by tunnelling the connection through the WireGuard netstack.
// This is the "outbound" direction: camera services → SOCKS5 → WireGuard → internet.
// Configure camera services (e.g. MQTT) to use SOCKS5 at 127.0.0.1:<port>.
func (t *tunnel) runOutboundSOCKS5(candidates []int) {
	defer t.wg.Done()

	var ln net.Listener
	for _, port := range candidates {
		var err error
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
		slog.Warn("outbound socks5 port unavailable, trying next", "port", port, "err", err)
	}
	if ln == nil {
		slog.Error("outbound socks5 listen", "candidates", candidates, "err", "all ports in use")
		return
	}
	go func() { <-t.stopCh; ln.Close() }()
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

// handleOutboundSOCKS5 implements the SOCKS5 server-side handshake (RFC 1928)
// and forwards the accepted connection to the real destination via WireGuard.
func (t *tunnel) handleOutboundSOCKS5(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(30 * time.Second))

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
	c.Write([]byte{0x05, 0x00}) // no auth required

	// Request
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	if buf[0] != 0x05 || buf[1] != 0x01 {
		c.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
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
		c.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	c.SetDeadline(time.Time{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	upstream, err := t.dialViaWG(ctx, hostport)
	cancel()
	if err != nil {
		c.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	// Success
	c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	done := make(chan struct{}, 2)
	go func() { io.Copy(upstream, c); done <- struct{}{} }()
	go func() { io.Copy(c, upstream); done <- struct{}{} }()
	<-done
}

// multiHandler fans slog records out to stderr and (when available) syslog,
// so that logs are visible both via systemd journal (ACAP 4) and via
// systemlog.cgi (ACAP 3, where the init wrapper does not capture stderr).
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
	_ = h.stderr.Handle(ctx, r)
	if h.sys != nil {
		msg := r.Message
		r.Attrs(func(a slog.Attr) bool {
			msg += " " + a.Key + "=" + fmt.Sprint(a.Value.Any())
			return true
		})
		switch {
		case r.Level >= slog.LevelError:
			_ = h.sys.Err(msg)
		case r.Level >= slog.LevelWarn:
			_ = h.sys.Warning(msg)
		default:
			_ = h.sys.Info(msg)
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
	slog.Info("wireguard-userspace starting", "config", configPath)

	app := &appState{configPath: configPath}

	// Seed lastMod from the file so the 30s ticker doesn't trigger a
	// spurious reload on its first fire (which would restart the tunnel
	// before the 25s keepalive has a chance to initiate the handshake).
	if info, err := os.Stat(configPath); err == nil {
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
			info, err := os.Stat(configPath)
			if err == nil && info.ModTime().After(app.lastMod) {
				app.lastMod = info.ModTime()
				slog.Info("config file changed — reloading")
				app.reload()
			}
		}
	}
}

// ── app state ────────────────────────────────────────────────────────────────

type appState struct {
	mu         sync.Mutex
	current    *tunnel
	configPath string
	lastMod    time.Time
}

func (a *appState) reload() {
	cfg, err := loadConfig(a.configPath)
	if err != nil {
		slog.Error("load config", "err", err)
		return
	}
	if cfg.PrivateKey == "" || cfg.PeerPubKey == "" {
		slog.Info("config incomplete — waiting for keys to be set")
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
		return
	}
	a.current = t
	slog.Info("WireGuard tunnel up",
		"ip", cfg.ClientIP,
		"endpoint", cfg.Endpoint,
		"socks5_port", socks5Port,
		"http_proxy_port", cfg.HTTPProxyPort,
		"outbound_socks5_port", cfg.OutboundSOCKS5Port,
	)
}
