package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCodexConfig(t *testing.T) {
	baseURL := "https://example.com"
	codexHome, err := writeCodexConfig(baseURL)
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
