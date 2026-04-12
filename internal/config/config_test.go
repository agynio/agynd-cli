package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validAgentID = "550e8400-e29b-41d4-a716-446655440000"

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_ID", validAgentID)
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
	t.Setenv("THREAD_ID", "thread-123")
	t.Setenv("LLM_BASE_URL", "https://llm.example")
	t.Setenv("LLM_API_TOKEN", "token-123")
	t.Setenv("AGENT_MCP_HOST", "mcp.internal")

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
	if cfg.ThreadID != "thread-123" {
		t.Fatalf("unexpected thread id: %s", cfg.ThreadID)
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
	if cfg.MCPHost != "mcp.internal" {
		t.Fatalf("unexpected MCP host: %s", cfg.MCPHost)
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
	if cfg.WorkDir != "/workspace" {
		t.Fatalf("expected default workspace dir, got %s", cfg.WorkDir)
	}
}

func TestFromEnvMCPHostDefault(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("AGENT_MCP_HOST", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.MCPHost != "127.0.0.1" {
		t.Fatalf("expected default MCP host, got %s", cfg.MCPHost)
	}
}

func TestFromEnvMCPHostInvalid(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")

	t.Setenv("AGENT_MCP_HOST", "http://localhost")
	_, err := fromEnv(configPath)
	if err == nil {
		t.Fatalf("expected error for AGENT_MCP_HOST with scheme")
	}

	t.Setenv("AGENT_MCP_HOST", "localhost:8100")
	_, err = fromEnv(configPath)
	if err == nil {
		t.Fatalf("expected error for AGENT_MCP_HOST with port")
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
	t.Setenv("THREAD_ID", "  thread-abc ")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.ThreadID != "thread-abc" {
		t.Fatalf("unexpected thread id: %q", cfg.ThreadID)
	}
}

func TestFromEnvThreadIDEmpty(t *testing.T) {
	setRequiredEnv(t)
	configPath := writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("THREAD_ID", "")

	cfg, err := fromEnv(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.ThreadID != "" {
		t.Fatalf("expected empty thread id, got %q", cfg.ThreadID)
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
