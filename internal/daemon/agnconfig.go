package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeAgnConfig(llmBaseURL string) (string, string, error) {
	trimmed := strings.TrimSpace(llmBaseURL)
	if trimmed == "" {
		return "", "", fmt.Errorf("llm base url is required")
	}
	dir, err := os.MkdirTemp("", "agnd-agn-config-")
	if err != nil {
		return "", "", fmt.Errorf("create agn config dir: %w", err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf("llm:\n  endpoint: %s\n  auth:\n    api_key: platform\n  model: default\n", trimmed)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", fmt.Errorf("write agn config: %w", err)
	}
	return dir, configPath, nil
}
