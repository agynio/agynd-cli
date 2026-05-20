package daemon

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/tracingproxy"
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
	codexEnvOTELExporterOTLPEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

func writeCodexConfig(llmBaseURL string, mcpServers []config.MCPServer) (string, error) {
	codexHome := filepath.Join(codexHomeEnv(), ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return "", fmt.Errorf("create codex home dir: %w", err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	payload := codexConfig(llmBaseURL, mcpServers)
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		return "", fmt.Errorf("write codex config: %w", err)
	}
	return codexHome, nil
}

func codexConfig(llmBaseURL string, mcpServers []config.MCPServer) string {
	otlpEndpoint := "http://" + tracingproxy.ListenAddress
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

func isZitiLLMBaseURL(llmBaseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(llmBaseURL))
	if err != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	return strings.HasSuffix(hostname, zitiHostnameSuffix)
}
