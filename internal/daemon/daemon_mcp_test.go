package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agynio/agynd-cli/internal/config"
)

func TestWaitForMCPServersReady(t *testing.T) {
	_, portA := startMCPReadyServer(t)
	_, portB := startMCPReadyServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	servers := []config.MCPServer{
		{Name: "alpha", Port: portA},
		{Name: "beta", Port: portB},
	}
	if err := waitForMCPServers(ctx, servers, 500*time.Millisecond); err != nil {
		t.Fatalf("expected MCP servers to be ready, got %v", err)
	}
}

func TestWaitForMCPServersWaitsForInitializeResponse(t *testing.T) {
	var requests atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := requests.Add(1)
		if attempt < 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"agynd-mcp-ready","result":{"protocolVersion":"2025-06-18"}}`))
	})}
	listener, port := startHTTPListener(t, server)
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	servers := []config.MCPServer{{Name: "memory", Port: port}}
	if err := waitForMCPServers(ctx, servers, 5*time.Second); err != nil {
		t.Fatalf("expected MCP server to become ready, got %v", err)
	}
	if got := requests.Load(); got < 3 {
		t.Fatalf("expected readiness probe retries, got %d request(s)", got)
	}
}

func TestWaitForMCPServersEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := waitForMCPServers(ctx, nil, 10*time.Millisecond); err != nil {
		t.Fatalf("expected no error for empty server list, got %v", err)
	}
}

func TestWaitForMCPServersTimeout(t *testing.T) {
	port := unusedTCPPort(t)

	err := waitForMCPServers(context.Background(), []config.MCPServer{{Name: "missing", Port: port}}, 50*time.Millisecond)
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

	err := waitForMCPServers(ctx, []config.MCPServer{{Name: "missing", Port: port}}, 5*time.Second)
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

func startMCPReadyServer(t *testing.T) (*http.Server, int) {
	t.Helper()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"agynd-mcp-ready","result":{"protocolVersion":"2025-06-18"}}`))
	})}
	listener, port := startHTTPListener(t, server)
	t.Cleanup(func() { _ = listener.Close() })
	return server, port
}

func startHTTPListener(t *testing.T, server *http.Server) (net.Listener, int) {
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
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !strings.Contains(err.Error(), "use of closed network connection") {
			panic(fmt.Sprintf("serve MCP ready test server: %v", err))
		}
	}()
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
