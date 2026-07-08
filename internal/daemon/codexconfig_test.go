package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
	codex "github.com/agynio/codex-sdk-go"
)

type noopCodexClient struct{}

func (noopCodexClient) StartThread(context.Context, *codex.ThreadStartParams) (*codex.ThreadStartResponse, error) {
	return nil, nil
}

func (noopCodexClient) ResumeThread(context.Context, *codex.ThreadResumeParams) (*codex.ThreadResumeResponse, error) {
	return nil, nil
}

func (noopCodexClient) ReadThread(context.Context, *codex.ThreadReadParams) (*codex.ThreadReadResponse, error) {
	return nil, nil
}

func (noopCodexClient) StartTurn(context.Context, *codex.TurnStartParams) (*codex.TurnStartResponse, error) {
	return nil, nil
}

func (noopCodexClient) Close() error {
	return nil
}

const testCodexOTLPEndpoint = "http://127.0.0.1:54321"

func TestWriteCodexConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	codexHome, err := writeCodexConfig(baseURL, nil, testCodexOTLPEndpoint)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	expectedHome := filepath.Join(tmpHome, ".codex")
	if codexHome != expectedHome {
		t.Fatalf("expected codex home %q, got %q", expectedHome, codexHome)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := codexConfigWithAPIKey(baseURL)
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteCodexConfigHomeFallback(t *testing.T) {
	t.Setenv("HOME", "")

	baseURL := "https://example.com"
	codexHome, err := writeCodexConfig(baseURL, nil, testCodexOTLPEndpoint)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	expectedHome := filepath.Join(codexDefaultHome, ".codex")
	if codexHome != expectedHome {
		t.Fatalf("expected codex home %q, got %q", expectedHome, codexHome)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := codexConfigWithAPIKey(baseURL)
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteCodexConfigForZitiOmitsAPIKeyEnv(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "http://llm-proxy.ziti:443/v1"
	codexHome, err := writeCodexConfig(baseURL, nil, testCodexOTLPEndpoint)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := codexConfigWithoutAPIKey(baseURL)
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteCodexConfigWithMCPServers(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	mcpServers := []config.MCPServer{
		{Name: "memory", Port: 8100},
		{Name: "cache", Port: 8200},
	}
	codexHome, err := writeCodexConfig(baseURL, mcpServers, testCodexOTLPEndpoint)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	expectedHome := filepath.Join(tmpHome, ".codex")
	if codexHome != expectedHome {
		t.Fatalf("expected codex home %q, got %q", expectedHome, codexHome)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := codexConfigWithAPIKey(baseURL) +
		"\n[mcp_servers.memory]\n" +
		"url = \"http://127.0.0.1:8100/mcp\"\n" +
		"required = true\n" +
		"startup_timeout_sec = 120\n" +
		"\n[mcp_servers.cache]\n" +
		"url = \"http://127.0.0.1:8200/mcp\"\n" +
		"required = true\n" +
		"startup_timeout_sec = 120\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestCodexEnvIncludesAPIKeyForPublicLLM(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	cfg := config.Config{
		LLMBaseURL:  "https://llm.example/v1",
		LLMAPIToken: "token-123",
	}

	env := codexEnv(cfg, "/tmp/.codex", "/tmp", testCodexOTLPEndpoint)

	if env[codexEnvOpenAIAPIKey] != "token-123" {
		t.Fatalf("expected API key in codex env, got %q", env[codexEnvOpenAIAPIKey])
	}
	assertCodexBaseEnv(t, env)
}

func TestCodexEnvOmitsAPIKeyForZitiLLM(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv(codexEnvNoProxy, "localhost, llm-proxy.ziti")
	t.Setenv(codexEnvNoProxyLower, "127.0.0.1")
	cfg := config.Config{
		LLMBaseURL:  "http://llm-proxy.ziti:443/v1",
		LLMAPIToken: "user-token",
	}

	env := codexEnv(cfg, "/tmp/.codex", "/tmp", testCodexOTLPEndpoint)

	if _, ok := env[codexEnvOpenAIAPIKey]; ok {
		t.Fatalf("expected ziti codex env to omit %s", codexEnvOpenAIAPIKey)
	}
	if env[codexEnvNoProxy] != "localhost,llm-proxy.ziti,127.0.0.1,.ziti,gateway.ziti,tracing.ziti" {
		t.Fatalf("unexpected %s: %q", codexEnvNoProxy, env[codexEnvNoProxy])
	}
	if env[codexEnvNoProxyLower] != env[codexEnvNoProxy] {
		t.Fatalf("unexpected %s: %q", codexEnvNoProxyLower, env[codexEnvNoProxyLower])
	}
	assertCodexBaseEnv(t, env)
}

func TestCodexEnvMergesNoProxySpellingsForZitiLLM(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv(codexEnvNoProxy, "localhost, .ZITI")
	t.Setenv(codexEnvNoProxyLower, "127.0.0.1, localhost, gateway.ziti")
	cfg := config.Config{LLMBaseURL: "http://llm-proxy.ziti:443/v1"}

	env := codexEnv(cfg, "/tmp/.codex", "/tmp", testCodexOTLPEndpoint)

	want := "localhost,.ZITI,127.0.0.1,gateway.ziti,llm-proxy.ziti,tracing.ziti"
	if env[codexEnvNoProxyLower] != env[codexEnvNoProxy] {
		t.Fatalf("expected proxy bypass env spellings to match: %s=%q %s=%q", codexEnvNoProxy, env[codexEnvNoProxy], codexEnvNoProxyLower, env[codexEnvNoProxyLower])
	}
	if env[codexEnvNoProxy] != want {
		t.Fatalf("expected %s %q, got %q", codexEnvNoProxy, want, env[codexEnvNoProxy])
	}
	if env[codexEnvNoProxyLower] != want {
		t.Fatalf("expected %s %q, got %q", codexEnvNoProxyLower, want, env[codexEnvNoProxyLower])
	}
}

func TestCodexEnvClearsProxyVarsForZitiLLM(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	for _, key := range codexProxyEnvVars {
		t.Setenv(key, "http://proxy.invalid:8080")
	}
	cfg := config.Config{LLMBaseURL: "http://llm-proxy.ziti:443/v1"}

	env := codexEnv(cfg, "/tmp/.codex", "/tmp", testCodexOTLPEndpoint)

	for _, key := range codexProxyEnvVars {
		value, ok := env[key]
		if !ok {
			t.Fatalf("expected %s override", key)
		}
		if value != "" {
			t.Fatalf("expected %s to be cleared, got %q", key, value)
		}
	}
}

func TestCodexEnvDoesNotSetNoProxyForPublicLLM(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	cfg := config.Config{
		LLMBaseURL:  "https://llm.example/v1",
		LLMAPIToken: "token-123",
	}

	env := codexEnv(cfg, "/tmp/.codex", "/tmp", testCodexOTLPEndpoint)

	if _, ok := env[codexEnvNoProxy]; ok {
		t.Fatalf("expected public LLM env to omit %s", codexEnvNoProxy)
	}
	if _, ok := env[codexEnvNoProxyLower]; ok {
		t.Fatalf("expected public LLM env to omit %s", codexEnvNoProxyLower)
	}
	for _, key := range codexProxyEnvVars {
		if _, ok := env[key]; ok {
			t.Fatalf("expected public LLM env to omit %s", key)
		}
	}
}

func TestZitiNoProxyValue(t *testing.T) {
	got := zitiNoProxyValue(" localhost, .ZITI,,gateway.ziti ", "127.0.0.1,localhost")
	want := "localhost,.ZITI,gateway.ziti,127.0.0.1,llm-proxy.ziti,tracing.ziti"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestWithoutCodexAuthEnvRemovesParentAuthVarsDuringStart(t *testing.T) {
	t.Setenv(codexEnvOpenAIAPIKey, "parent-openai-token")
	t.Setenv(codexEnvCodexAPIKey, "parent-codex-token")
	t.Setenv(codexEnvCodexAccessToken, "parent-access-token")

	seenEnv := map[string]bool{}
	_, err := withoutCodexAuthEnv(func() (codexClient, error) {
		for _, key := range codexAuthEnvVars {
			_, seenEnv[key] = os.LookupEnv(key)
		}
		return noopCodexClient{}, nil
	})
	if err != nil {
		t.Fatalf("expected auth env scrub to succeed, got %v", err)
	}

	for _, key := range codexAuthEnvVars {
		if seenEnv[key] {
			t.Fatalf("expected %s to be unset while starting ziti Codex", key)
		}
	}
	if got := os.Getenv(codexEnvOpenAIAPIKey); got != "parent-openai-token" {
		t.Fatalf("expected OPENAI_API_KEY restore, got %q", got)
	}
	if got := os.Getenv(codexEnvCodexAPIKey); got != "parent-codex-token" {
		t.Fatalf("expected CODEX_API_KEY restore, got %q", got)
	}
	if got := os.Getenv(codexEnvCodexAccessToken); got != "parent-access-token" {
		t.Fatalf("expected CODEX_ACCESS_TOKEN restore, got %q", got)
	}
}

func TestZitiCodexProcessReceivesNoAuthEnvConfig(t *testing.T) {
	t.Setenv(codexEnvOpenAIAPIKey, "parent-openai-token")
	t.Setenv(codexEnvCodexAPIKey, "parent-codex-token")
	t.Setenv(codexEnvCodexAccessToken, "parent-access-token")
	t.Setenv("PATH", "/usr/bin")
	cfg := config.Config{
		LLMBaseURL:  "http://llm-proxy.ziti:443/v1",
		LLMAPIToken: "agent-env-token",
	}

	env := codexEnv(cfg, "/tmp/.codex", "/tmp", testCodexOTLPEndpoint)
	configPayload := codexConfig(cfg.LLMBaseURL, nil, testCodexOTLPEndpoint)
	seenEnv := map[string]bool{}
	_, err := withoutCodexAuthEnv(func() (codexClient, error) {
		for _, key := range codexAuthEnvVars {
			_, seenEnv[key] = os.LookupEnv(key)
			if _, ok := env[key]; ok {
				t.Fatalf("expected ziti codex env overrides to omit %s", key)
			}
		}
		return noopCodexClient{}, nil
	})
	if err != nil {
		t.Fatalf("expected auth env scrub to succeed, got %v", err)
	}

	for _, key := range codexAuthEnvVars {
		if seenEnv[key] {
			t.Fatalf("expected parent %s to be absent during ziti Codex start", key)
		}
	}
	if strings.Contains(configPayload, "env_key") {
		t.Fatalf("expected ziti codex config to omit env_key, got %q", configPayload)
	}
}

func TestIsZitiLLMBaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "platform proxy", url: "http://llm-proxy.ziti:443/v1", want: true},
		{name: "nested ziti host", url: "https://models.mesh.ziti/v1", want: true},
		{name: "uppercase host", url: " HTTP://LLM-PROXY.ZITI:443/v1 ", want: true},
		{name: "public endpoint", url: "https://llm.example/v1", want: false},
		{name: "ziti as registrable suffix", url: "https://exampleziti/v1", want: false},
		{name: "invalid url", url: "://bad", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isZitiLLMBaseURL(tt.url); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func assertCodexBaseEnv(t *testing.T, env map[string]string) {
	t.Helper()
	if env[codexEnvPath] != agentPathValue() {
		t.Fatalf("expected PATH %q, got %q", agentPathValue(), env[codexEnvPath])
	}
	if env[codexEnvCodexHome] != "/tmp/.codex" {
		t.Fatalf("expected CODEX_HOME, got %q", env[codexEnvCodexHome])
	}
	if env[codexEnvHome] != "/tmp" {
		t.Fatalf("expected HOME, got %q", env[codexEnvHome])
	}
	if env[codexEnvOTELExporterOTLPEndpoint] != testCodexOTLPEndpoint {
		t.Fatalf("expected OTLP endpoint, got %q", env[codexEnvOTELExporterOTLPEndpoint])
	}
}

func codexConfigWithAPIKey(baseURL string) string {
	return codexConfigPayload(baseURL, testCodexOTLPEndpoint, `env_key = "OPENAI_API_KEY"
`)
}

func codexConfigWithoutAPIKey(baseURL string) string {
	return codexConfigPayload(baseURL, testCodexOTLPEndpoint, "")
}

func codexConfigPayload(baseURL, otlpEndpoint, apiKeyEnv string) string {
	return fmt.Sprintf(`model_provider = "platform"
approval_policy = "never"
sandbox_mode = "danger-full-access"

[otel]
trace_exporter = { otlp-grpc = { endpoint = %q } }
metrics_exporter = "none"
exporter = "none"

[model_providers.platform]
name = "Agyn LLM"
base_url = %q
%swire_api = "responses"
request_max_retries = 0
stream_max_retries = 0
supports_websockets = false
`, otlpEndpoint, baseURL, apiKeyEnv)
}
