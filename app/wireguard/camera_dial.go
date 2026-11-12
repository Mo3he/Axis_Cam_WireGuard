package main

import (
	"errors"
	"net"
	"strconv"
	"syscall"
	"time"
)

// Camera services are dialed on 127.0.0.2, not 127.0.0.1. Since AXIS OS 13 the
// web server answers 127.0.0.1 in plain HTTP on every port, 443 included, so
// HTTPS through the tunnel failed. Any other loopback address reaches the same
// listeners LAN clients use.
const (
	cameraHost        = "127.0.0.2"
	loopbackHost      = "127.0.0.1"
	cameraDialTimeout = 10 * time.Second
)

// dialCamera connects to a camera port. With fallback, a port refused on
// 127.0.0.2 is retried on 127.0.0.1, so a forwarded service bound only to
// 127.0.0.1 stays reachable. The tunnel-side SOCKS5 proxy accepts any port, so
// it never falls back: loopback-only services must not become reachable from
// the VPN.
func dialCamera(port int, fallback bool) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(cameraHost, strconv.Itoa(port)), cameraDialTimeout)
	if err != nil && fallback && errors.Is(err, syscall.ECONNREFUSED) {
		return net.DialTimeout("tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(port)), cameraDialTimeout)
	}
	return c, err
}
