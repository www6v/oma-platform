package outbound

import (
	"net"
	"strings"
)

// HostForHarness maps a listen address to host:port for sandbox clients.
func HostForHarness(listenAddr string) string {
	if listenAddr == "" {
		return ""
	}
	if strings.HasPrefix(listenAddr, ":") {
		return "127.0.0.1" + listenAddr
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return listenAddr
	}
	if host == "" || host == "0.0.0.0" || host == "[::]" {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return net.JoinHostPort(host, port)
}
