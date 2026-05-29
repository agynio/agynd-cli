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
	agentBinary := buildFakeAgnAgent(t)
	server := startAgyndGatewayStub(t)
	workDir := t.TempDir()
	runtimeDir := t.TempDir()
	writeAgentRuntimeConfig(t, runtimeDir)

	container := startAgyndContainer(t, binary, agentBinary, runtimeDir, workDir, server.address)
	defer container.cleanup()

	if err := server.waitRunStarted(10 * time.Second); err != nil {
		container.dumpLogs(t)
		t.Fatalf("agynd did not reach daemon.Run/subscriber startup: %v", err)
	}

	if err := container.interrupt(); err != nil {
		container.dumpLogs(t)
		t.Fatalf("interrupt agynd container: %v", err)
	}
	if err := container.wait(5 * time.Second); err != nil {
		container.kill()
		container.dumpLogs(t)
		t.Fatalf("agynd container did not stop after interrupt: %v", err)
	}

	server.assertInitialized(t)
	server.assertRunStarted(t)
}

type agyndContainer struct {
	id string
}

func startAgyndContainer(t *testing.T, binary string, agentBinary string, runtimeDir string, workDir string, gatewayAddress string) *agyndContainer {
	t.Helper()
	args := []string{
		"create",
		"--add-host=host.docker.internal:host-gateway",
		"--workdir", "/workspace",
		"--env", "AGENT_ID=" + agyndE2EAgentID,
		"--env", "THREAD_ID=" + agyndE2EThreadID,
		"--env", "WORKLOAD_ID=" + agyndE2EWorkloadID,
		"--env", "GATEWAY_ADDRESS=" + hostGatewayAddress(gatewayAddress),
		"--env", "TRACING_ADDRESS=host.docker.internal:1",
		"--env", "LLM_BASE_URL=https://testllm.dev/v1/org/agynio/suite/agn",
		"--env", "LLM_API_TOKEN=test-token",
		"--env", "AGN_TOKEN_COUNTING_ADDRESS=host.docker.internal:1",
		"--env", "HOME=/tmp/agynd-home",
		"--env", "WORKSPACE_DIR=/workspace",
		"golang:1.26",
		"/agyn-test-bin/agynd",
	}
	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("create agynd container: %v\n%s", err, output)
	}
	id := strings.TrimSpace(string(output))
	if id == "" {
		t.Fatalf("docker returned empty container id")
	}
	container := &agyndContainer{id: id}
	t.Cleanup(container.cleanup)

	copyToContainer(t, container, filepath.Dir(binary), "/agyn-test-bin")
	copyToContainer(t, container, agentBinary, "/agyn-test-bin/fake-agn")
	copyToContainer(t, container, runtimeDir, "/agyn-bin")
	copyToContainer(t, container, workDir, "/workspace")

	if output, err := exec.Command("docker", "start", container.id).CombinedOutput(); err != nil {
		t.Fatalf("start agynd container: %v\n%s", err, output)
	}
	return container
}

func copyToContainer(t *testing.T, container *agyndContainer, source string, destination string) {
	t.Helper()
	if output, err := exec.Command("docker", "cp", source, container.id+":"+destination).CombinedOutput(); err != nil {
		t.Fatalf("copy %s to container %s: %v\n%s", source, destination, err, output)
	}
}

func hostGatewayAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		panic(fmt.Sprintf("invalid gateway address %q", address))
	}
	return net.JoinHostPort("host.docker.internal", port)
}

func (c *agyndContainer) interrupt() error {
	cmd := exec.Command("docker", "kill", "--signal=SIGINT", c.id)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker interrupt: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (c *agyndContainer) kill() {
	_ = exec.Command("docker", "kill", c.id).Run()
}

func (c *agyndContainer) cleanup() {
	_ = exec.Command("docker", "rm", "-f", c.id).Run()
}

func (c *agyndContainer) wait(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "wait", c.id)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("docker wait: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (c *agyndContainer) dumpLogs(t *testing.T) {
	t.Helper()
	output, err := exec.Command("docker", "logs", c.id).CombinedOutput()
	if err != nil {
		t.Logf("docker logs failed: %v", err)
	}
	t.Logf("agynd container logs:\n%s", output)
}

func buildAgynd(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), ".tmp-e2e", "agynd-"+strings.ReplaceAll(t.Name(), "/", "-"))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create agynd build dir: %v", err)
	}
	path := filepath.Join(dir, "agynd")
	cmd := exec.Command("go", "build", "-trimpath", "-o", path, "./cmd/agynd")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build agynd: %v\n%s", err, output)
	}
	return path
}

func writeAgentRuntimeConfig(t *testing.T, runtimeDir string) {
	t.Helper()
	configPath := filepath.Join(runtimeDir, "config.json")
	payload := `{"sdk":"agn","bin":"/agyn-test-bin/fake-agn"}`
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write agynd config: %v", err)
	}
}

func buildFakeAgnAgent(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-agn")
	source := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(source, []byte(fakeAgnAgentSource), 0o600); err != nil {
		t.Fatalf("write fake agn source: %v", err)
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", path, source)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake agn agent: %v\n%s", err, output)
	}
	return path
}

const fakeAgnAgentSource = "package main\n\nimport (\n\t\"bufio\"\n\t\"encoding/json\"\n\t\"fmt\"\n\t\"os\"\n)\n\ntype request struct {\n\tJSONRPC string          `json:\"jsonrpc\"`\n\tID      json.RawMessage `json:\"id\"`\n\tMethod  string          `json:\"method\"`\n\tParams  json.RawMessage `json:\"params\"`\n}\n\ntype response struct {\n\tJSONRPC string          `json:\"jsonrpc\"`\n\tID      json.RawMessage `json:\"id\"`\n\tResult  json.RawMessage `json:\"result\"`\n}\n\nfunc main() {\n\tif len(os.Args) < 2 || os.Args[1] != \"serve\" {\n\t\tos.Exit(2)\n\t}\n\tscanner := bufio.NewScanner(os.Stdin)\n\tfor scanner.Scan() {\n\t\tvar req request\n\t\tif err := json.Unmarshal(scanner.Bytes(), &req); err != nil {\n\t\t\tcontinue\n\t\t}\n\t\tresult := json.RawMessage(`{\"thread_id\":\"thread-from-fake-agent\",\"response\":\"ok\"}`)\n\t\tif req.Method == \"thread/list\" {\n\t\t\tresult = json.RawMessage(`{\"data\":[],\"next_cursor\":null}`)\n\t\t}\n\t\tpayload, _ := json.Marshal(response{JSONRPC: \"2.0\", ID: req.ID, Result: result})\n\t\tfmt.Println(string(payload))\n\t}\n}\n"

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

type agyndGatewayStub struct {
	address    string
	server     *grpc.Server
	runStarted chan struct{}
	runOnce    sync.Once

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
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen gateway stub: %v", err)
	}
	stub := &agyndGatewayStub{address: listener.Addr().String(), runStarted: make(chan struct{})}
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
	return &agentsv1.ListInitScriptsResponse{}, nil
}

func (s *agyndGatewayStub) markRunStarted() {
	s.runOnce.Do(func() { close(s.runStarted) })
}

func (s *agyndGatewayStub) waitRunStarted(timeout time.Duration) error {
	select {
	case <-s.runStarted:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for Subscribe; calls: %s", s.callsSummary())
	}
}

func (s agyndThreadsGatewayStub) GetUnackedMessages(context.Context, *threadsv1.GetUnackedMessagesRequest) (*threadsv1.GetUnackedMessagesResponse, error) {
	return &threadsv1.GetUnackedMessagesResponse{}, nil
}

func (s agyndNotificationsGatewayStub) Subscribe(_ *notificationsv1.SubscribeRequest, stream grpc.ServerStreamingServer[notificationsv1.SubscribeResponse]) error {
	s.markRunStarted()
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

func (s *agyndGatewayStub) assertRunStarted(t *testing.T) {
	t.Helper()
	select {
	case <-s.runStarted:
	default:
		t.Fatal("expected agynd to enter daemon.Run and subscribe to notifications")
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
