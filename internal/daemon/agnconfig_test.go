package daemon

import (
	"fmt"
	"os"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
)

func TestWriteAgnConfig(t *testing.T) {
	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(agnDir)
	})

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model)
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteAgnConfigWithMCPServers(t *testing.T) {
	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	mcpServers := []config.MCPServer{
		{Name: "memory", Port: 8100},
		{Name: "filesystem", Port: 8200},
	}
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, mcpServers)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(agnDir)
	})

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := agnConfig(baseURL, apiKey, model, mcpServers)
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}
