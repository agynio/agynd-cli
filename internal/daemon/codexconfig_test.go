package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
)

func TestWriteCodexConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	codexHome, err := writeCodexConfig(baseURL, "127.0.0.1", nil)
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

	expected := fmt.Sprintf(`model_provider = "platform"
approval_policy = "never"
sandbox_mode = "danger-full-access"

[model_providers.platform]
name = "Agyn LLM"
base_url = %q
env_key = "OPENAI_API_KEY"
wire_api = "responses"
`, baseURL)
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
	codexHome, err := writeCodexConfig(baseURL, "127.0.0.1", mcpServers)
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

	expected := fmt.Sprintf(`model_provider = "platform"
approval_policy = "never"
sandbox_mode = "danger-full-access"

[model_providers.platform]
name = "Agyn LLM"
base_url = %q
env_key = "OPENAI_API_KEY"
wire_api = "responses"
`, baseURL) +
		"\n[mcp_servers.memory]\n" +
		"url = \"http://127.0.0.1:8100/mcp\"\n" +
		"required = true\n" +
		"startup_timeout_sec = 180\n" +
		"\n[mcp_servers.cache]\n" +
		"url = \"http://127.0.0.1:8200/mcp\"\n" +
		"required = true\n" +
		"startup_timeout_sec = 180\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}
