package daemon

import (
	"fmt"
	"os"
	"testing"
)

func TestWriteAgnConfig(t *testing.T) {
	baseURL := "https://example.com"
	agnDir, configPath, err := writeAgnConfig(baseURL)
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

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, agnDefaultModel)
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}
