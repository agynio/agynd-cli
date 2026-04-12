package daemon

import (
	"net"
	"net/url"
	"strconv"
)

func mcpServerURL(host string, port int) string {
	endpoint := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/mcp",
	}
	return endpoint.String()
}

func mcpServerAddress(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
