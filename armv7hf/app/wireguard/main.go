// WireGuard userspace VPN for Axis cameras (ACAP).
// Runs entirely in userspace via wireguard-go + gVisor netstack — no kernel TUN
// device, no CAP_NET_ADMIN, no root required.
//
// Network access model:
//   - Transparent TCP port forwarding for common camera ports (80, 443, 554)
//     → VPN peers can browse/stream directly to the WireGuard IP with no config
//   - SOCKS5 proxy on port 1080 → full access to any camera port without
//     needing per-port forwarders; configure your browser/client once
//
// Config is read from CONFIG_FILE (written by the C ACAP binary).
// Reloads on SIGUSR1 or when the config file modification time changes.

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
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

// socks5Port is the SOCKS5 proxy port on the WireGuard interface.
// Clients connect here to reach any port on the camera.
const socks5Port = 1080

// Config holds parsed WireGuard settings from the config file.
type Config struct {
	PrivateKey string
	ListenPort string
	Endpoint   string
	PeerPubKey string
	AllowedIPs string
	ClientIP   string
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &Config{
		AllowedIPs: "0.0.0.0/0",
		ClientIP:   "10.0.0.2/24",
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
		fmt.Fprintf(&sb, "endpoint=%s\n", cfg.Endpoint)
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

	// Transparent forwarders for common camera ports
	for _, port := range transparentPorts {
		t.wg.Add(1)
		go t.runTCPProxy(localAddr, port, fmt.Sprintf("127.0.0.1:%d", port))
	}

	// SOCKS5 proxy for full access to any camera port
	t.wg.Add(1)
	go t.runSOCKS5(localAddr, socks5Port)

	return t, nil
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

func main() {
	configPath := defaultConfigPath
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	slog.Info("wireguard-userspace starting", "config", configPath)

	var current *tunnel

	// Seed lastMod from the file so the 30s ticker doesn't trigger a
	// spurious reload on its first fire (which would restart the tunnel
	// before the 25s keepalive has a chance to initiate the handshake).
	var lastMod time.Time
	if info, err := os.Stat(configPath); err == nil {
		lastMod = info.ModTime()
	}

	reload := func() {
		cfg, err := loadConfig(configPath)
		if err != nil {
			slog.Error("load config", "err", err)
			return
		}
		if cfg.PrivateKey == "" || cfg.PeerPubKey == "" {
			slog.Info("config incomplete — waiting for keys to be set")
			return
		}

		if current != nil {
			current.close()
			current = nil
		}

		t, err := startTunnel(cfg)
		if err != nil {
			slog.Error("start tunnel", "err", err)
			return
		}
		current = t
		slog.Info("WireGuard tunnel up",
			"ip", cfg.ClientIP,
			"endpoint", cfg.Endpoint,
			"socks5_port", socks5Port,
		)
	}

	reload()

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
				reload()
			default:
				slog.Info("shutting down")
				if current != nil {
					current.close()
				}
				return
			}
		case <-ticker.C:
			info, err := os.Stat(configPath)
			if err == nil && info.ModTime().After(lastMod) {
				lastMod = info.ModTime()
				slog.Info("config file changed — reloading")
				reload()
			}
		}
	}
}
