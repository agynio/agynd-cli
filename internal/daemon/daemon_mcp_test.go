package daemon

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/agynio/agynd-cli/internal/config"
)

func TestWaitForMCPServersReady(t *testing.T) {
	listenerA, portA := startTCPListener(t)
	defer listenerA.Close()
	listenerB, portB := startTCPListener(t)
	defer listenerB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	servers := []config.MCPServer{
		{Name: "alpha", Port: portA},
		{Name: "beta", Port: portB},
	}
	if err := waitForMCPServers(ctx, servers, "127.0.0.1", 500*time.Millisecond); err != nil {
		t.Fatalf("expected MCP servers to be ready, got %v", err)
	}
}

func TestWaitForMCPServersEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := waitForMCPServers(ctx, nil, "127.0.0.1", 10*time.Millisecond); err != nil {
		t.Fatalf("expected no error for empty server list, got %v", err)
	}
}

func TestWaitForMCPServersTimeout(t *testing.T) {
	port := unusedTCPPort(t)

	err := waitForMCPServers(context.Background(), []config.MCPServer{{Name: "missing", Port: port}}, "127.0.0.1", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "not ready after") {
		t.Fatalf("unexpected timeout error: %v", err)
	}
}

func TestWaitForMCPServersContextCanceled(t *testing.T) {
	port := unusedTCPPort(t)

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer timer.Stop()

	err := waitForMCPServers(ctx, []config.MCPServer{{Name: "missing", Port: port}}, "127.0.0.1", 5*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func startTCPListener(t *testing.T) (net.Listener, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		t.Fatalf("unexpected listener address type %T", listener.Addr())
	}
	return listener, addr.Port
}

func unusedTCPPort(t *testing.T) int {
	t.Helper()
	listener, port := startTCPListener(t)
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return port
}
