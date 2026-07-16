package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	validAgentID    = "550e8400-e29b-41d4-a716-446655440000"
	validThreadID   = "3f7fd7e3-5c6c-4a1e-8f4e-69c0030d3ed7"
	validWorkloadID = "7e5dc613-7822-4f48-9f55-6ce6d72a0a2e"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_ID", validAgentID)
	t.Setenv("THREAD_ID", validThreadID)
	t.Setenv("WORKLOAD_ID", validWorkloadID)
	t.Setenv("MCP_PORT", "")
}

func writeAgentConfig(t *testing.T, sdk, bin string) string {
	t.Helper()
	payload := fmt.Sprintf(`{"sdk":%q,"bin":%q}`, sdk, bin)
	return writeAgentConfigRaw(t, payload)
}

func writeAgentConfigRaw(t *testing.T, payload string) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func TestFromEnvValid(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("WORKSPACE_DIR", "/tmp/workdir")
	t.Setenv("GATEWAY_ADDRESS", "gateway:1234")
	t.Setenv("TRACING_ADDRESS", "tracing:5678")
	t.Setenv("THREAD_ID", "9d06fe19-695b-48ac-83ba-2cd82472f7c8")
	t.Setenv("LLM_BASE_URL", "https://llm.example")
	t.Setenv("LLM_API_TOKEN", "token-123")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.AgentID.String() != validAgentID {
		t.Fatalf("unexpected agent id: %s", cfg.AgentID.String())
	}
	if cfg.GatewayAddress != "gateway:1234" {
		t.Fatalf("unexpected gateway address: %s", cfg.GatewayAddress)
	}
	if cfg.TracingAddress != "tracing:5678" {
		t.Fatalf("unexpected tracing address: %s", cfg.TracingAddress)
	}
	if cfg.ThreadID != "9d06fe19-695b-48ac-83ba-2cd82472f7c8" {
		t.Fatalf("unexpected thread id: %s", cfg.ThreadID)
	}
	if cfg.WorkloadID != validWorkloadID {
		t.Fatalf("unexpected workload id: %s", cfg.WorkloadID)
	}
	if cfg.LLMBaseURL != "https://llm.example" {
		t.Fatalf("unexpected LLM base URL: %s", cfg.LLMBaseURL)
	}
	if cfg.LLMAPIToken != "token-123" {
		t.Fatalf("unexpected LLM API token: %s", cfg.LLMAPIToken)
	}
	if cfg.SDK != "codex" {
		t.Fatalf("unexpected sdk: %s", cfg.SDK)
	}
	if cfg.AgentBinary != "/opt/bin/codex" {
		t.Fatalf("unexpected agent binary: %s", cfg.AgentBinary)
	}
	if cfg.WorkDir != "/tmp/workdir" {
		t.Fatalf("unexpected work dir: %s", cfg.WorkDir)
	}
	if cfg.MCPPort != nil {
		t.Fatalf("expected no MCP port, got %v", *cfg.MCPPort)
	}
}

func TestFromEnvLLMAPITokenDefault(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("LLM_API_TOKEN", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.LLMAPIToken != "platform" {
		t.Fatalf("expected default LLM API token, got %q", cfg.LLMAPIToken)
	}
}

func TestFromEnvMissingRequired(t *testing.T) {
	tests := []struct {
		name     string
		missing  string
		expected string
	}{
		{name: "agent-id", missing: "AGENT_ID", expected: "AGENT_ID"},
		{name: "thread-id", missing: "THREAD_ID", expected: "THREAD_ID"},
		{name: "workload-id", missing: "WORKLOAD_ID", expected: "WORKLOAD_ID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredEnv(t)
			configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
			t.Setenv(test.missing, "")
			_, err := fromEnv(configPath)
			if err == nil {
				t.Fatalf("expected error for missing %s", test.missing)
			}
			if !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFromEnvDefaults(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("WORKSPACE_DIR", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.WorkDir != "/tmp" {
		t.Fatalf("expected default workspace dir /tmp, got %s", cfg.WorkDir)
	}
}

func TestFromEnvGatewayDefault(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("GATEWAY_ADDRESS", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.GatewayAddress != "gateway.ziti:443" {
		t.Fatalf("expected default gateway address, got %s", cfg.GatewayAddress)
	}
}

func TestFromEnvTracingAddressDefault(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("TRACING_ADDRESS", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.TracingAddress != "tracing.ziti:443" {
		t.Fatalf("expected default tracing address, got %s", cfg.TracingAddress)
	}
}

func TestFromEnvTracingAddressCustom(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("TRACING_ADDRESS", "tracing.local:9999")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.TracingAddress != "tracing.local:9999" {
		t.Fatalf("unexpected tracing address: %s", cfg.TracingAddress)
	}
}

func TestFromEnvThreadID(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("THREAD_ID", "  9E54A9CE-F2E1-4B5F-9C91-8F4C77554C30 ")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.ThreadID != "9e54a9ce-f2e1-4b5f-9c91-8f4c77554c30" {
		t.Fatalf("unexpected thread id: %q", cfg.ThreadID)
	}
}

func TestFromEnvWorkloadID(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("WORKLOAD_ID", "  94F0F199-7F3A-4B7C-9B2E-9E8C4200BA87 ")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.WorkloadID != "94f0f199-7f3a-4b7c-9b2e-9e8c4200ba87" {
		t.Fatalf("unexpected workload id: %q", cfg.WorkloadID)
	}
}

func TestFromEnvWorkloadIDEmpty(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("WORKLOAD_ID", "")

	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for missing WORKLOAD_ID")
	}
	if !strings.Contains(err.Error(), "WORKLOAD_ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvWorkloadIDInvalid(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("WORKLOAD_ID", "not-a-uuid")

	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for invalid WORKLOAD_ID")
	}
	if !strings.Contains(err.Error(), "WORKLOAD_ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvThreadIDEmpty(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("THREAD_ID", "")

	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for missing THREAD_ID")
	}
	if !strings.Contains(err.Error(), "THREAD_ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvThreadIDInvalid(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("THREAD_ID", "not-a-uuid")

	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for invalid THREAD_ID")
	}
	if !strings.Contains(err.Error(), "THREAD_ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvLLMBaseURLDefault(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("LLM_BASE_URL", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.LLMBaseURL != "http://llm-proxy.ziti:443/v1" {
		t.Fatalf("expected default LLM base URL, got %s", cfg.LLMBaseURL)
	}
}

func TestFromEnvMCPServersEmpty(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("AGENT_MCP_SERVERS", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.MCPServers != nil {
		t.Fatalf("expected nil MCP servers, got %#v", cfg.MCPServers)
	}
}

func TestFromEnvMCPPort(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("AGENT_MCP_SERVERS", "")
	t.Setenv("MCP_PORT", "8123")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.MCPPort == nil || *cfg.MCPPort != 8123 {
		t.Fatalf("unexpected MCP port: %#v", cfg.MCPPort)
	}
}

func TestFromEnvMCPServersSingle(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("AGENT_MCP_SERVERS", "memory:8100")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(cfg.MCPServers))
	}
	server := cfg.MCPServers[0]
	if server.Name != "memory" || server.Port != 8100 {
		t.Fatalf("unexpected MCP server: %#v", server)
	}
}

func TestFromEnvMCPServersMultiple(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("AGENT_MCP_SERVERS", "memory:8100, cache:8200")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(cfg.MCPServers))
	}
	if cfg.MCPServers[0].Name != "memory" || cfg.MCPServers[0].Port != 8100 {
		t.Fatalf("unexpected first MCP server: %#v", cfg.MCPServers[0])
	}
	if cfg.MCPServers[1].Name != "cache" || cfg.MCPServers[1].Port != 8200 {
		t.Fatalf("unexpected second MCP server: %#v", cfg.MCPServers[1])
	}
}

func TestFromEnvMCPServersMalformed(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing-port", value: "memory"},
		{name: "missing-name", value: ":8100"},
		{name: "invalid-port", value: "memory:abc"},
		{name: "port-too-large", value: "memory:65536"},
		{name: "invalid-name-uppercase", value: "Memory:8100"},
		{name: "invalid-name-hyphen", value: "mem-ory:8100"},
		{name: "empty-entry", value: "memory:8100,"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredEnv(t)
			configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
			t.Setenv("AGENT_MCP_SERVERS", test.value)

			_, err := fromEnv(configPath)
			if err == nil {
				t.Fatalf("expected error for %s", test.value)
			}
			if !strings.Contains(err.Error(), "AGENT_MCP_SERVERS") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFromEnvMCPPortInvalid(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("MCP_PORT", "70000")

	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for invalid MCP_PORT")
	}
	if !strings.Contains(err.Error(), "MCP_PORT") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvMissingConfigFile(t *testing.T) {
	setRequiredEnv(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvMissingConfigFields(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfigRaw(t, `{}`)
	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for missing sdk")
	}
	if !strings.Contains(err.Error(), "missing sdk") {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath = writeAgentConfigRaw(t, `{"sdk":"codex"}`)
	_, err = fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for missing bin")
	}
	if !strings.Contains(err.Error(), "missing bin") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvAgentModeDefault(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("AGYND_MODE", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Mode != ModeAgent {
		t.Fatalf("expected agent mode, got %q", cfg.Mode)
	}
}

func TestFromEnvHolderModeMinimal(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "missing-config.json")
	t.Setenv("AGYND_MODE", ModeHolder)
	t.Setenv("AGENT_ID", "")
	t.Setenv("THREAD_ID", "")
	t.Setenv("WORKLOAD_ID", "")
	t.Setenv("WORKSPACE_DIR", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Mode != ModeHolder {
		t.Fatalf("expected holder mode, got %q", cfg.Mode)
	}
	if cfg.SDK != ModeHolder {
		t.Fatalf("expected holder sdk marker, got %q", cfg.SDK)
	}
	if cfg.WorkDir != HolderDefaultWorkDir {
		t.Fatalf("expected holder default work dir %s, got %s", HolderDefaultWorkDir, cfg.WorkDir)
	}
	if cfg.AgentID.String() != "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("expected zero agent id, got %s", cfg.AgentID.String())
	}
	if cfg.ThreadID != "" || cfg.WorkloadID != "" || cfg.GatewayAddress != "" {
		t.Fatalf("expected holder mode to skip platform config, got thread=%q workload=%q gateway=%q", cfg.ThreadID, cfg.WorkloadID, cfg.GatewayAddress)
	}
}

func TestFromEnvHolderModeWorkDir(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "missing-config.json")
	t.Setenv("AGYND_MODE", ModeHolder)
	t.Setenv("WORKSPACE_DIR", " /sandbox-workspace ")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.WorkDir != "/sandbox-workspace" {
		t.Fatalf("unexpected holder work dir: %q", cfg.WorkDir)
	}
}

func TestFromEnvInvalidMode(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("AGYND_MODE", "sandbox")

	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "AGYND_MODE") {
		t.Fatalf("unexpected error: %v", err)
	}
}
