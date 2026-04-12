package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

func writeCodexConfig(llmBaseURL, mcpHost string, mcpServers []config.MCPServer) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return "", fmt.Errorf("create codex home dir: %w", err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	payload := codexConfig(llmBaseURL, mcpHost, mcpServers)
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		return "", fmt.Errorf("write codex config: %w", err)
	}
	return codexHome, nil
}

func codexConfig(llmBaseURL, mcpHost string, mcpServers []config.MCPServer) string {
	payload := fmt.Sprintf(codexConfigTemplate, llmBaseURL)
	if len(mcpServers) == 0 {
		return payload
	}
	var builder strings.Builder
	builder.WriteString(payload)
	for _, server := range mcpServers {
		url := mcpServerURL(mcpHost, server.Port)
		startupTimeout := int(mcpReadyTimeout / time.Second)
		fmt.Fprintf(&builder, "\n[mcp_servers.%s]\nurl = %q\nrequired = true\nstartup_timeout_sec = %d\n", server.Name, url, startupTimeout)
	}
	return builder.String()
}
