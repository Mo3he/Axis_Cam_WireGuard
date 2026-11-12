package main

import (
	"net"
	"runtime"
	"testing"
)

func listenPort(t *testing.T, addr string) int {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func TestDialCamera(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs all of 127.0.0.0/8 on the loopback interface, as on Linux")
	}

	// A service on all addresses, like the camera's web server, is reached on
	// 127.0.0.2.
	port := listenPort(t, ":0")
	c, err := dialCamera(port, false)
	if err != nil {
		t.Fatalf("wildcard service: %v", err)
	}
	if got := c.RemoteAddr().(*net.TCPAddr).IP.String(); got != cameraHost {
		t.Errorf("wildcard service dialed on %s, want %s", got, cameraHost)
	}
	_ = c.Close()

	// A service bound only to 127.0.0.1 is reached with the fallback and
	// never without it.
	port = listenPort(t, "127.0.0.1:0")
	if c, err := dialCamera(port, false); err == nil {
		_ = c.Close()
		t.Error("loopback-only service reached without fallback")
	}
	c, err = dialCamera(port, true)
	if err != nil {
		t.Fatalf("loopback-only service with fallback: %v", err)
	}
	_ = c.Close()
}
