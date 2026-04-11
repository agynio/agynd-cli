package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agynio/agynd-cli/internal/config"
)

type claudeSettings struct {
	Permissions claudePermissions          `json:"permissions"`
	Env         map[string]string          `json:"env"`
	MCPServers  map[string]claudeMCPServer `json:"mcpServers,omitempty"`
}

type claudePermissions struct {
	DefaultMode string   `json:"defaultMode"`
	Allow       []string `json:"allow"`
	Deny        []string `json:"deny"`
}

type claudeMCPServer struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func writeClaudeSettings(llmBaseURL, apiKey string, mcpServers []config.MCPServer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		return fmt.Errorf("create claude config dir: %w", err)
	}
	settings := claudeSettings{
		Permissions: claudePermissions{
			DefaultMode: "bypassPermissions",
			Allow: []string{
				"Bash",
				"Read",
				"Write",
				"Edit",
				"MultiEdit",
				"WebFetch",
				"WebSearch",
				"Grep",
				"Glob",
				"LS",
				"Task",
				"TodoWrite",
				"NotebookEdit",
			},
			Deny: []string{},
		},
		Env: map[string]string{
			"ANTHROPIC_BASE_URL": llmBaseURL,
			"ANTHROPIC_API_KEY":  apiKey,
		},
	}
	if len(mcpServers) > 0 {
		settings.MCPServers = make(map[string]claudeMCPServer, len(mcpServers))
		for _, server := range mcpServers {
			settings.MCPServers[server.Name] = claudeMCPServer{
				Type: "http",
				URL:  fmt.Sprintf("http://localhost:%d/mcp", server.Port),
			}
		}
	}
	payload, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal claude settings: %w", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, payload, 0o600); err != nil {
		return fmt.Errorf("write claude settings: %w", err)
	}
	return nil
}

func claudeBaseURL(llmBaseURL string) string {
	trimmed := strings.TrimSpace(llmBaseURL)
	trimmed = strings.TrimSuffix(trimmed, "/")
	return strings.TrimSuffix(trimmed, "/v1")
}
