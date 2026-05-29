package daemon

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/agynio/agynd-cli/internal/config"
	codex "github.com/agynio/codex-sdk-go"
)

const codexConfigTemplate = `model_provider = "platform"
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
`

const codexAPIKeyEnv = `env_key = "OPENAI_API_KEY"
`

const zitiHostnameSuffix = ".ziti"

const (
	codexEnvPath                     = "PATH"
	codexEnvHome                     = "HOME"
	codexEnvCodexHome                = "CODEX_HOME"
	codexEnvOpenAIAPIKey             = "OPENAI_API_KEY"
	codexEnvCodexAPIKey              = "CODEX_API_KEY"
	codexEnvCodexAccessToken         = "CODEX_ACCESS_TOKEN"
	codexEnvOTELExporterOTLPEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

var codexAuthEnvMu sync.Mutex

var codexAuthEnvVars = []string{
	codexEnvOpenAIAPIKey,
	codexEnvCodexAPIKey,
	codexEnvCodexAccessToken,
}

func writeCodexConfig(llmBaseURL string, mcpServers []config.MCPServer, otlpEndpoint string) (string, error) {
	codexHome := filepath.Join(codexHomeEnv(), ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return "", fmt.Errorf("create codex home dir: %w", err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	payload := codexConfig(llmBaseURL, mcpServers, otlpEndpoint)
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		return "", fmt.Errorf("write codex config: %w", err)
	}
	return codexHome, nil
}

func codexConfig(llmBaseURL string, mcpServers []config.MCPServer, otlpEndpoint string) string {
	apiKeyEnv := codexAPIKeyEnv
	if isZitiLLMBaseURL(llmBaseURL) {
		apiKeyEnv = ""
	}
	payload := fmt.Sprintf(codexConfigTemplate, otlpEndpoint, llmBaseURL, apiKeyEnv)
	if len(mcpServers) == 0 {
		return payload
	}
	var builder strings.Builder
	builder.WriteString(payload)
	for _, server := range mcpServers {
		url := fmt.Sprintf("http://localhost:%d/mcp", server.Port)
		fmt.Fprintf(&builder, "\n[mcp_servers.%s]\nurl = %q\nrequired = true\nstartup_timeout_sec = 120\n", server.Name, url)
	}
	return builder.String()
}

func codexEnv(cfg config.Config, codexHome, codexHomeValue, otlpEndpoint string) map[string]string {
	env := map[string]string{
		codexEnvPath:                     agentPathValue(),
		codexEnvCodexHome:                codexHome,
		codexEnvHome:                     codexHomeValue,
		codexEnvOTELExporterOTLPEndpoint: otlpEndpoint,
	}
	if !isZitiLLMBaseURL(cfg.LLMBaseURL) {
		env[codexEnvOpenAIAPIKey] = cfg.LLMAPIToken
	}
	return env
}

func newCodexClient(ctx context.Context, cfg config.Config, options ...codex.Option) (codexClient, error) {
	if !isZitiLLMBaseURL(cfg.LLMBaseURL) {
		return codex.NewClient(ctx, options...)
	}
	return withoutCodexAuthEnv(func() (codexClient, error) {
		return codex.NewClient(ctx, options...)
	})
}

func withoutCodexAuthEnv(start func() (codexClient, error)) (codexClient, error) {
	codexAuthEnvMu.Lock()
	defer codexAuthEnvMu.Unlock()

	originalEnv := captureEnv(codexAuthEnvVars)
	for _, key := range codexAuthEnvVars {
		if err := os.Unsetenv(key); err != nil {
			restoreEnv(originalEnv)
			return nil, fmt.Errorf("unset %s: %w", key, err)
		}
	}

	client, err := start()
	restoreEnv(originalEnv)
	return client, err
}

type envValue struct {
	value string
	set   bool
}

func captureEnv(keys []string) map[string]envValue {
	values := make(map[string]envValue, len(keys))
	for _, key := range keys {
		value, ok := os.LookupEnv(key)
		values[key] = envValue{value: value, set: ok}
	}
	return values
}

func restoreEnv(values map[string]envValue) {
	for key, value := range values {
		if value.set {
			_ = os.Setenv(key, value.value)
			continue
		}
		_ = os.Unsetenv(key)
	}
}

func isZitiLLMBaseURL(llmBaseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(llmBaseURL))
	if err != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	return strings.HasSuffix(hostname, zitiHostnameSuffix)
}
