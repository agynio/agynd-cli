package daemon

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWaitForZitiLLMServiceSkipsPublicURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := waitForZitiLLMService(ctx, "https://llm.example/v1", 10*time.Millisecond); err != nil {
		t.Fatalf("expected public LLM URL to be skipped, got %v", err)
	}
}

func TestZitiLLMServiceAddress(t *testing.T) {
	tests := []struct {
		name string
		url  string
		addr string
		ok   bool
	}{
		{name: "default http", url: "http://llm-proxy.agyn/v1", addr: "llm-proxy.agyn:80", ok: true},
		{name: "default https", url: "https://llm-proxy.agyn/v1", addr: "llm-proxy.agyn:443", ok: true},
		{name: "explicit port", url: "http://llm-proxy.agyn:8080/v1", addr: "llm-proxy.agyn:8080", ok: true},
		{name: "public", url: "https://llm.example/v1", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, ok, err := zitiLLMServiceAddress(tt.url)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if ok != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, ok)
			}
			if addr != tt.addr {
				t.Fatalf("expected addr %q, got %q", tt.addr, addr)
			}
		})
	}
}

func TestZitiLLMServiceAddressRejectsMissingDefaultPort(t *testing.T) {
	_, _, err := zitiLLMServiceAddress("tcp://llm-proxy.agyn/v1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "has no default port") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForTCPServiceReady(t *testing.T) {
	listener, port := startTCPListener(t)
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	addr := tcpAddrForPort(port)
	if err := waitForTCPService(ctx, addr, 500*time.Millisecond, "test service"); err != nil {
		t.Fatalf("expected service to be ready, got %v", err)
	}
}

func TestWaitForTCPServiceTimeout(t *testing.T) {
	addr := tcpAddrForPort(unusedTCPPort(t))

	err := waitForTCPService(context.Background(), addr, 50*time.Millisecond, "test service")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "not ready after") {
		t.Fatalf("unexpected timeout error: %v", err)
	}
}

func TestWaitForTCPServiceContextCanceled(t *testing.T) {
	addr := tcpAddrForPort(unusedTCPPort(t))
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer timer.Stop()

	err := waitForTCPService(ctx, addr, 5*time.Second, "test service")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func tcpAddrForPort(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
