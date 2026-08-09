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
# OTLP/HTTP, not gRPC: codex's otlp-grpc exporter fails to build at all in the
# version shipped in the init image -- it reports "error loading otel config:
# transport error" against any address, a live listener or a closed port alike --
# and codex then exits before answering the initialize handshake. protocol is
# required and binary is the OTLP default encoding.
trace_exporter = { otlp-http = { endpoint = %q, protocol = "binary" } }
# Prompts, tool calls and SSE events ship over logs, not traces; log_user_prompt
# is the opt-in that stops codex reducing the prompt to prompt_length.
exporter = { otlp-http = { endpoint = %q, protocol = "binary" } }
log_user_prompt = true
metrics_exporter = "none"

[model_providers.platform]
name = "Agyn LLM"
base_url = %q
%swire_api = "responses"
request_max_retries = 0
stream_max_retries = 0
supports_websockets = false
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
	codexEnvNoProxy                  = "NO_PROXY"
	codexEnvNoProxyLower             = "no_proxy"
	codexEnvHTTPProxy                = "HTTP_PROXY"
	codexEnvHTTPProxyLower           = "http_proxy"
	codexEnvHTTPSProxy               = "HTTPS_PROXY"
	codexEnvHTTPSProxyLower          = "https_proxy"
	codexEnvAllProxy                 = "ALL_PROXY"
	codexEnvAllProxyLower            = "all_proxy"
)

var codexZitiNoProxyHosts = []string{
	".ziti",
	"llm-proxy.ziti",
	"gateway.ziti",
	"tracing.ziti",
}

var codexAuthEnvMu sync.Mutex

var codexAuthEnvVars = []string{
	codexEnvOpenAIAPIKey,
	codexEnvCodexAPIKey,
	codexEnvCodexAccessToken,
}

var codexProxyEnvVars = []string{
	codexEnvHTTPProxy,
	codexEnvHTTPProxyLower,
	codexEnvHTTPSProxy,
	codexEnvHTTPSProxyLower,
	codexEnvAllProxy,
	codexEnvAllProxyLower,
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

// Codex wants a per-signal URL, not a base, so the caller passes the collector
// root and the signal path is appended here.
func otlpSignalEndpoint(otlpEndpoint, signal string) string {
	return strings.TrimSuffix(otlpEndpoint, "/") + "/v1/" + signal
}

func codexConfig(llmBaseURL string, mcpServers []config.MCPServer, otlpEndpoint string) string {
	apiKeyEnv := codexAPIKeyEnv
	if isZitiLLMBaseURL(llmBaseURL) {
		apiKeyEnv = ""
	}
	payload := fmt.Sprintf(codexConfigTemplate, otlpSignalEndpoint(otlpEndpoint, "traces"),
		otlpSignalEndpoint(otlpEndpoint, "logs"), llmBaseURL, apiKeyEnv)
	if len(mcpServers) == 0 {
		return payload
	}
	var builder strings.Builder
	builder.WriteString(payload)
	for _, server := range mcpServers {
		url := mcpEndpoint(server.Port)
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
	if isZitiLLMBaseURL(cfg.LLMBaseURL) {
		noProxyValue := zitiNoProxyValue(
			os.Getenv(codexEnvNoProxy),
			os.Getenv(codexEnvNoProxyLower),
		)
		env[codexEnvNoProxy] = noProxyValue
		env[codexEnvNoProxyLower] = noProxyValue
		for _, key := range codexProxyEnvVars {
			env[key] = ""
		}
	} else {
		env[codexEnvOpenAIAPIKey] = cfg.LLMAPIToken
	}
	return env
}

func zitiNoProxyValue(values ...string) string {
	seen := make(map[string]struct{}, len(codexZitiNoProxyHosts))
	merged := make([]string, 0, len(codexZitiNoProxyHosts))
	for _, value := range values {
		for _, entry := range splitNoProxy(value) {
			key := strings.ToLower(entry)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, entry)
		}
	}
	for _, host := range codexZitiNoProxyHosts {
		key := strings.ToLower(host)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, host)
	}
	return strings.Join(merged, ",")
}

func splitNoProxy(value string) []string {
	parts := strings.Split(value, ",")
	entries := make([]string, 0, len(parts))
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
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
