package main

import (
	"net"
	"runtime"
	"testing"
)

func listenPort(t *testing.T, network, addr string) int {
	t.Helper()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func dialedHost(port int, fallback string) string {
	c, err := dialCamera(port, fallback)
	if err != nil {
		return "unreachable"
	}
	defer c.Close() //nolint:errcheck
	return c.RemoteAddr().(*net.TCPAddr).IP.String()
}

func TestDialCamera(t *testing.T) {
	// The camera runs Linux; 127.0.0.2 needs all of 127.0.0.0/8 on loopback.
	if runtime.GOOS != "linux" {
		t.Skip("needs Linux loopback addressing")
	}
	if ln, err := net.Listen("tcp6", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback:", err)
	} else {
		_ = ln.Close()
	}

	cases := []struct {
		name           string
		network, addr  string
		forward, socks string
	}{
		// Like the camera's web server: [::1] for both.
		{"all addresses", "tcp", ":0", cameraHost, cameraHost},
		// Reachable from the LAN, so also through SOCKS5.
		{"IPv4 0.0.0.0 only", "tcp4", "0.0.0.0:0", forwardFallback, socksFallback},
		// Internal: forwarded ports may reach it, SOCKS5 never.
		{"127.0.0.1 only", "tcp", "127.0.0.1:0", forwardFallback, "unreachable"},
	}
	for _, c := range cases {
		port := listenPort(t, c.network, c.addr)
		if got := dialedHost(port, forwardFallback); got != c.forward {
			t.Errorf("%s, forwarded port: dialed %s, want %s", c.name, got, c.forward)
		}
		if got := dialedHost(port, socksFallback); got != c.socks {
			t.Errorf("%s, SOCKS5: dialed %s, want %s", c.name, got, c.socks)
		}
	}
}
