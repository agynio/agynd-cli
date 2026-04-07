package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agynio/agynd-cli/internal/config"
)

const agnConfigTemplate = `llm:
  endpoint: %s
  auth:
    api_key: %s
  model: %s
`

func writeAgnConfig(llmBaseURL, apiKey, model string, mcpServers []config.MCPServer) (string, string, error) {
	agnDir, err := os.MkdirTemp("", "agynd-agn-")
	if err != nil {
		return "", "", fmt.Errorf("create agn config dir: %w", err)
	}
	configPath := filepath.Join(agnDir, "config.yaml")
	payload := agnConfig(llmBaseURL, apiKey, model, mcpServers)
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		_ = os.RemoveAll(agnDir)
		return "", "", fmt.Errorf("write agn config: %w", err)
	}
	return agnDir, configPath, nil
}

func agnConfig(llmBaseURL, apiKey, model string, mcpServers []config.MCPServer) string {
	payload := fmt.Sprintf(agnConfigTemplate, llmBaseURL, apiKey, model)
	if len(mcpServers) == 0 {
		return payload
	}
	var builder strings.Builder
	builder.WriteString(payload)
	builder.WriteString("mcp:\n  servers:\n")
	for _, server := range mcpServers {
		url := fmt.Sprintf("http://localhost:%d/mcp", server.Port)
		fmt.Fprintf(&builder, "    %s:\n      url: %s\n", server.Name, url)
	}
	return builder.String()
}
