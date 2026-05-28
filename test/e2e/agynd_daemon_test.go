//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	notificationsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/notifications/v1"
	runnersv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/runners/v1"
	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	agyndE2EAgentID    = "550e8400-e29b-41d4-a716-446655440000"
	agyndE2EThreadID   = "550e8400-e29b-41d4-a716-446655440001"
	agyndE2EWorkloadID = "550e8400-e29b-41d4-a716-446655440002"
)

func TestAgyndBinaryInitializesWithStubGateway(t *testing.T) {
	binary := buildAgynd(t)
	server := startAgyndGatewayStub(t)
	workDir := t.TempDir()
	agentConfigPath := writeAgentRuntimeConfig(t, `{"sdk":"agn","bin":"/bin/false"}`)

	cmd := exec.Command(binary)
	cmd.Dir = workDir
	cmd.Env = append(cleanAgyndEnv(),
		"AGENT_ID="+agyndE2EAgentID,
		"THREAD_ID="+agyndE2EThreadID,
		"WORKLOAD_ID="+agyndE2EWorkloadID,
		"GATEWAY_ADDRESS="+server.address,
		"TRACING_ADDRESS=127.0.0.1:1",
		"LLM_BASE_URL=https://testllm.dev/v1/org/agynio/suite/agn",
		"LLM_API_TOKEN=test-token",
		"AGYND_CONFIG_PATH="+agentConfigPath,
		"AGN_TOKEN_COUNTING_ADDRESS=127.0.0.1:1",
		"HOME="+t.TempDir(),
		"WORKSPACE_DIR="+workDir,
	)

	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agynd: %v", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	if err := server.waitInitialized(10 * time.Second); err != nil {
		terminateProcess(t, cmd.Process)
		<-waitCh
		t.Fatalf("agynd did not initialize against stub gateway: %v\noutput:\n%s", err, output.String())
	}

	select {
	case err := <-waitCh:
		assertAgyndExit(t, err, output.String())
		server.assertInitialized(t)
		return
	default:
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		select {
		case waitErr := <-waitCh:
			assertAgyndExit(t, waitErr, output.String())
			server.assertInitialized(t)
			return
		default:
		}
		terminateProcess(t, cmd.Process)
		<-waitCh
		t.Fatalf("interrupt agynd: %v\noutput:\n%s", err, output.String())
	}
	select {
	case err := <-waitCh:
		assertAgyndExit(t, err, output.String())
	case <-time.After(5 * time.Second):
		terminateProcess(t, cmd.Process)
		<-waitCh
		t.Fatalf("agynd did not stop after interrupt; output:\n%s", output.String())
	}

	server.assertInitialized(t)
}

func assertAgyndExit(t *testing.T, err error, output string) {
	t.Helper()
	if err == nil {
		return
	}
	for _, expected := range []string{"daemon stopped", "daemon exited", "daemon init failed", "signal: interrupt"} {
		if strings.Contains(output, expected) {
			return
		}
	}
	t.Fatalf("agynd exited unexpectedly: %v\noutput:\n%s", err, output)
}

func terminateProcess(t *testing.T, process *os.Process) {
	t.Helper()
	if process == nil {
		return
	}
	_ = process.Signal(os.Interrupt)
	time.Sleep(100 * time.Millisecond)
	_ = process.Signal(syscall.SIGKILL)
}

func cleanAgyndEnv() []string {
	blocked := map[string]struct{}{
		"AGENT_MCP_SERVERS": {},
		"MCP_PORT":          {},
	}
	env := os.Environ()
	cleaned := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, skip := blocked[key]; skip {
			continue
		}
		cleaned = append(cleaned, entry)
	}
	return cleaned
}

func buildAgynd(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agynd")
	cmd := exec.Command("go", "build", "-trimpath", "-o", path, "./cmd/agynd")
	cmd.Dir = repoRoot(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build agynd: %v\n%s", err, output)
	}
	return path
}

func writeAgentRuntimeConfig(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write agynd config: %v", err)
	}
	return path
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

type agyndGatewayStub struct {
	address string
	server  *grpc.Server
	ready   chan struct{}
	once    sync.Once

	mu                   sync.Mutex
	getAgentCalls        int
	listSkillsCalls      int
	listMCPsCalls        int
	listInitScriptsCalls int
}

type agyndAgentsGatewayStub struct {
	gatewayv1.UnimplementedAgentsGatewayServer
	*agyndGatewayStub
}

type agyndThreadsGatewayStub struct {
	gatewayv1.UnimplementedThreadsGatewayServer
	*agyndGatewayStub
}

type agyndNotificationsGatewayStub struct {
	gatewayv1.UnimplementedNotificationsGatewayServer
	*agyndGatewayStub
}

type agyndRunnersGatewayStub struct {
	gatewayv1.UnimplementedRunnersGatewayServer
	*agyndGatewayStub
}

func startAgyndGatewayStub(t *testing.T) *agyndGatewayStub {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen gateway stub: %v", err)
	}
	stub := &agyndGatewayStub{address: listener.Addr().String(), ready: make(chan struct{})}
	stub.server = grpc.NewServer()
	gatewayv1.RegisterAgentsGatewayServer(stub.server, agyndAgentsGatewayStub{agyndGatewayStub: stub})
	gatewayv1.RegisterThreadsGatewayServer(stub.server, agyndThreadsGatewayStub{agyndGatewayStub: stub})
	gatewayv1.RegisterNotificationsGatewayServer(stub.server, agyndNotificationsGatewayStub{agyndGatewayStub: stub})
	gatewayv1.RegisterRunnersGatewayServer(stub.server, agyndRunnersGatewayStub{agyndGatewayStub: stub})

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- stub.server.Serve(listener)
	}()
	t.Cleanup(func() {
		stub.server.Stop()
		_ = listener.Close()
		select {
		case err := <-serveErr:
			if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
				t.Errorf("gateway stub stopped with error: %v", err)
			}
		default:
		}
	})
	return stub
}

func (s agyndAgentsGatewayStub) GetAgent(_ context.Context, req *agentsv1.GetAgentRequest) (*agentsv1.GetAgentResponse, error) {
	if req.GetId() != agyndE2EAgentID {
		return nil, status.Errorf(codes.InvalidArgument, "unexpected agent id %q", req.GetId())
	}
	s.mu.Lock()
	s.getAgentCalls++
	s.mu.Unlock()
	return &agentsv1.GetAgentResponse{Agent: &agentsv1.Agent{
		Meta:  &agentsv1.EntityMeta{Id: agyndE2EAgentID},
		Name:  "agynd-e2e-agent",
		Model: "simple-hello",
	}}, nil
}

func (s agyndAgentsGatewayStub) ListSkills(_ context.Context, req *agentsv1.ListSkillsRequest) (*agentsv1.ListSkillsResponse, error) {
	if req.GetAgentId() != agyndE2EAgentID {
		return nil, status.Errorf(codes.InvalidArgument, "unexpected skills agent id %q", req.GetAgentId())
	}
	s.mu.Lock()
	s.listSkillsCalls++
	s.mu.Unlock()
	return &agentsv1.ListSkillsResponse{}, nil
}

func (s agyndAgentsGatewayStub) ListMcps(_ context.Context, req *agentsv1.ListMcpsRequest) (*agentsv1.ListMcpsResponse, error) {
	if req.GetAgentId() != agyndE2EAgentID {
		return nil, status.Errorf(codes.InvalidArgument, "unexpected mcps agent id %q", req.GetAgentId())
	}
	s.mu.Lock()
	s.listMCPsCalls++
	s.mu.Unlock()
	return &agentsv1.ListMcpsResponse{}, nil
}

func (s agyndAgentsGatewayStub) ListInitScripts(_ context.Context, req *agentsv1.ListInitScriptsRequest) (*agentsv1.ListInitScriptsResponse, error) {
	if req.GetAgentId() != agyndE2EAgentID {
		return nil, status.Errorf(codes.InvalidArgument, "unexpected init scripts agent id %q", req.GetAgentId())
	}
	s.mu.Lock()
	s.listInitScriptsCalls++
	s.mu.Unlock()
	s.markInitialized()
	return &agentsv1.ListInitScriptsResponse{}, nil
}

func (s *agyndGatewayStub) markInitialized() {
	s.once.Do(func() { close(s.ready) })
}

func (s *agyndGatewayStub) waitInitialized(timeout time.Duration) error {
	select {
	case <-s.ready:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for ListInitScripts; calls: %s", s.callsSummary())
	}
}

func (s agyndThreadsGatewayStub) GetUnackedMessages(context.Context, *threadsv1.GetUnackedMessagesRequest) (*threadsv1.GetUnackedMessagesResponse, error) {
	return &threadsv1.GetUnackedMessagesResponse{}, nil
}

func (s agyndNotificationsGatewayStub) Subscribe(_ *notificationsv1.SubscribeRequest, stream grpc.ServerStreamingServer[notificationsv1.SubscribeResponse]) error {
	<-stream.Context().Done()
	return stream.Context().Err()
}

func (s agyndRunnersGatewayStub) TouchWorkload(context.Context, *runnersv1.TouchWorkloadRequest) (*runnersv1.TouchWorkloadResponse, error) {
	return &runnersv1.TouchWorkloadResponse{}, nil
}

func (s *agyndGatewayStub) assertInitialized(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	checks := map[string]int{
		"GetAgent":        s.getAgentCalls,
		"ListSkills":      s.listSkillsCalls,
		"ListMcps":        s.listMCPsCalls,
		"ListInitScripts": s.listInitScriptsCalls,
	}
	for name, calls := range checks {
		if calls == 0 {
			t.Fatalf("expected agynd to call %s during initialization; calls: %s", name, formatCalls(checks))
		}
	}
}

func (s *agyndGatewayStub) callsSummary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return formatCalls(map[string]int{
		"GetAgent":        s.getAgentCalls,
		"ListSkills":      s.listSkillsCalls,
		"ListMcps":        s.listMCPsCalls,
		"ListInitScripts": s.listInitScriptsCalls,
	})
}

func formatCalls(calls map[string]int) string {
	return fmt.Sprintf("GetAgent=%d ListSkills=%d ListMcps=%d ListInitScripts=%d", calls["GetAgent"], calls["ListSkills"], calls["ListMcps"], calls["ListInitScripts"])
}
