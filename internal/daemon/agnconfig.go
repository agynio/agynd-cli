package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	agnDefaultAPIKey = "platform"
	agnDefaultModel  = "default"
)

const agnConfigTemplate = `llm:
  endpoint: %s
  auth:
    api_key: %s
  model: %s
`

func writeAgnConfig(llmBaseURL string) (string, string, error) {
	agnDir, err := os.MkdirTemp("", "agynd-agn-")
	if err != nil {
		return "", "", fmt.Errorf("create agn config dir: %w", err)
	}
	configPath := filepath.Join(agnDir, "config.yaml")
	payload := fmt.Sprintf(agnConfigTemplate, llmBaseURL, agnDefaultAPIKey, agnDefaultModel)
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		_ = os.RemoveAll(agnDir)
		return "", "", fmt.Errorf("write agn config: %w", err)
	}
	return agnDir, configPath, nil
}
