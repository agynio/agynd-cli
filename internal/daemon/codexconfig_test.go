package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
)

func TestWriteCodexConfig(t *testing.T) {
	baseURL := "https://example.com"
	codexHome, err := writeCodexConfig(baseURL, nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(codexHome)
	})

	configPath := filepath.Join(codexHome, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(codexConfigTemplate, baseURL)
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteCodexConfigWithMCPServers(t *testing.T) {
	baseURL := "https://example.com"
	mcpServers := []config.MCPServer{
		{Name: "memory", Port: 8100},
		{Name: "cache", Port: 8200},
	}
	codexHome, err := writeCodexConfig(baseURL, mcpServers)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(codexHome)
	})

	configPath := filepath.Join(codexHome, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(codexConfigTemplate, baseURL) +
		"\n[mcp_servers.memory]\n" +
		"url = \"http://localhost:8100/mcp\"\n" +
		"required = true\n" +
		"startup_timeout_sec = 120\n" +
		"\n[mcp_servers.cache]\n" +
		"url = \"http://localhost:8200/mcp\"\n" +
		"required = true\n" +
		"startup_timeout_sec = 120\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}
