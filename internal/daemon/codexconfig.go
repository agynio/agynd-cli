package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agynio/agynd-cli/internal/config"
)

const codexConfigTemplate = `model_provider = "platform"
approval_policy = "never"
sandbox_mode = "danger-full-access"

[model_providers.platform]
name = "Agyn LLM"
base_url = %q
env_key = "OPENAI_API_KEY"
wire_api = "responses"
`

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
	payload := fmt.Sprintf(codexConfigTemplate, llmBaseURL)
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
