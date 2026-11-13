package main

import (
	"net"
	"strconv"
	"time"
)

// Camera services are dialed on [::1]. The web server's loopback virtual hosts
// differ by AXIS OS version: AXIS OS 13 answers 127.0.0.1 in plain HTTP on every
// port, 443 included, and AXIS OS 12 does the same for other 127.x addresses.
// Both serve [::1] like the network, with TLS on 443.
const (
	cameraHost = "::1"
	// forwardFallback is tried when [::1] fails for a forwarded port. The
	// admin chose those ports, so they may reach services that listen only on
	// 127.0.0.1 (such as port 80 on an HTTPS-only device).
	forwardFallback = "127.0.0.1"
	// socksFallback is tried when [::1] fails for the tunnel-side SOCKS5
	// proxy, which accepts any port. It reaches services listening on IPv4
	// 0.0.0.0 but never those bound to 127.0.0.1 alone, such as other apps'
	// settings back ends, which have no login of their own.
	socksFallback     = "127.0.0.2"
	cameraDialTimeout = 10 * time.Second
)

// dialCamera connects to a camera port on [::1], or on fallback if that fails.
func dialCamera(port int, fallback string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(cameraHost, strconv.Itoa(port)), cameraDialTimeout)
	if err != nil {
		return net.DialTimeout("tcp", net.JoinHostPort(fallback, strconv.Itoa(port)), cameraDialTimeout)
	}
	return c, err
}
