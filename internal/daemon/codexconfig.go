package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

const codexConfigTemplate = `model_provider = "platform"

[model_providers.platform]
name = "Agyn LLM"
base_url = %q
env_key = "OPENAI_API_KEY"
wire_api = "responses"
`

func writeCodexConfig(llmBaseURL string) (string, error) {
	codexHome, err := os.MkdirTemp("", "agynd-codex-")
	if err != nil {
		return "", fmt.Errorf("create codex config dir: %w", err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	payload := fmt.Sprintf(codexConfigTemplate, llmBaseURL)
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		_ = os.RemoveAll(codexHome)
		return "", fmt.Errorf("write codex config: %w", err)
	}
	return codexHome, nil
}
