package daemon

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/tracing"
	codex "github.com/agynio/codex-sdk-go"
)

// codexNativeConfigTemplate is the same file minus the endpoint: no
// model_provider and no [model_providers] block, so codex talks to its own
// vendor. Tracing and the mcp_servers appended below are unaffected.
const codexNativeConfigTemplate = `approval_policy = "never"
sandbox_mode = "danger-full-access"
` + codexHookTemplate

// traceHookCommand is the platform's trace hook, delivered to /agyn/bin beside
// agynd and reached by name because that directory is on the agent's PATH.
const (
	traceHookCommand     = "agyn-trace-hook"
	traceHookFormatEnv   = "AGYN_TRACE_FORMAT"
	traceHookAddressEnv  = "TRACING_ADDRESS"
	traceHookTraceEnv    = "AGYN_TRACE_ID"
	traceHookWorkloadEnv = "WORKLOAD_ID"

	traceFormatCodex  = "codex"
	traceFormatClaude = "claude"
)

// codexHookTemplate runs the platform's trace hook when a turn completes.
// Codex hands it the rollout path, which is the record of what the turn
// actually did -- its telemetry reports only that a call happened.
const codexHookTemplate = `
[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "` + traceHookCommand + `"
`

const codexConfigTemplate = `model_provider = "platform"
approval_policy = "never"
sandbox_mode = "danger-full-access"

[model_providers.platform]
name = "Agyn LLM"
base_url = %q
%swire_api = "responses"
request_max_retries = 0
stream_max_retries = 0
supports_websockets = false
` + codexHookTemplate

const codexAPIKeyEnv = `env_key = "OPENAI_API_KEY"
`

const zitiHostnameSuffix = ".ziti"

const (
	codexEnvPath             = "PATH"
	codexEnvHome             = "HOME"
	codexEnvCodexHome        = "CODEX_HOME"
	codexEnvOpenAIAPIKey     = "OPENAI_API_KEY"
	codexEnvCodexAPIKey      = "CODEX_API_KEY"
	codexEnvCodexAccessToken = "CODEX_ACCESS_TOKEN"
	codexEnvNoProxy          = "NO_PROXY"
	codexEnvNoProxyLower     = "no_proxy"
	codexEnvHTTPProxy        = "HTTP_PROXY"
	codexEnvHTTPProxyLower   = "http_proxy"
	codexEnvHTTPSProxy       = "HTTPS_PROXY"
	codexEnvHTTPSProxyLower  = "https_proxy"
	codexEnvAllProxy         = "ALL_PROXY"
	codexEnvAllProxyLower    = "all_proxy"
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

func writeCodexConfig(cfg config.Config) (string, error) {
	codexHome := filepath.Join(codexHomeEnv(), ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return "", fmt.Errorf("create codex home dir: %w", err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	payload := codexConfig(cfg)
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		return "", fmt.Errorf("write codex config: %w", err)
	}
	return codexHome, nil
}

func codexPlatformConfig(llmBaseURL string) string {
	apiKeyEnv := codexAPIKeyEnv
	if isZitiLLMBaseURL(llmBaseURL) {
		apiKeyEnv = ""
	}
	return fmt.Sprintf(codexConfigTemplate, llmBaseURL, apiKeyEnv)
}

func codexConfig(cfg config.Config) string {
	payload := codexPlatformConfig(cfg.LLMBaseURL)
	if cfg.LLMNative {
		payload = codexNativeConfigTemplate
	}
	mcpServers := cfg.MCPServers
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

func codexEnv(cfg config.Config, codexHome, codexHomeValue string) map[string]string {
	env := map[string]string{
		codexEnvPath:      agentPathValue(),
		codexEnvCodexHome: codexHome,
		codexEnvHome:      codexHomeValue,
		// The hook is told which transcript it is being handed rather than
		// sniffing the file, where to export what it reads, and which trace to
		// write into -- the one agynd opened for this wake cycle.
		traceHookFormatEnv:   traceFormatCodex,
		traceHookAddressEnv:  cfg.TracingAddress,
		traceHookTraceEnv:    traceHookTraceID(cfg.WorkloadID),
		traceHookWorkloadEnv: cfg.WorkloadID,
	}
	// Native mode carries no platform credential at all: the placeholder codex
	// reads is on the container, and the proxy replaces it upstream.
	if cfg.LLMNative || isZitiLLMBaseURL(cfg.LLMBaseURL) {
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

// The hook is handed the trace as hex rather than the workload it came from,
// so agynd stays the one that decides what a trace is.
func traceHookTraceID(workloadID string) string {
	return hex.EncodeToString(tracing.TraceID(workloadID))
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
	// Native mode is the one case where the ambient auth variables are the
	// point: the placeholder lives in them, and codex refuses to start without
	// one. Only the platform path strips them.
	if cfg.LLMNative || !isZitiLLMBaseURL(cfg.LLMBaseURL) {
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
