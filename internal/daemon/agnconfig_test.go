package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
)

func TestWriteAgnConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, nil, nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	expectedDir := filepath.Join(tmpHome, ".agyn", "agn")
	if agnDir != expectedDir {
		t.Fatalf("expected agn dir %q, got %q", expectedDir, agnDir)
	}

	expectedPath := filepath.Join(expectedDir, "config.yaml")
	if configPath != expectedPath {
		t.Fatalf("expected config path %q, got %q", expectedPath, configPath)
	}

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
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	mcpServers := []config.MCPServer{
		{Name: "memory", Port: 8100},
		{Name: "filesystem", Port: 8200},
	}
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, nil, mcpServers)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	expectedDir := filepath.Join(tmpHome, ".agyn", "agn")
	if agnDir != expectedDir {
		t.Fatalf("expected agn dir %q, got %q", expectedDir, agnDir)
	}

	expectedPath := filepath.Join(expectedDir, "config.yaml")
	if configPath != expectedPath {
		t.Fatalf("expected config path %q, got %q", expectedPath, configPath)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model) +
		"mcp:\n  servers:\n" +
		"    memory:\n      url: http://localhost:8100/mcp\n" +
		"    filesystem:\n      url: http://localhost:8200/mcp\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteAgnConfigWithSummarization(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	keepTokens := 10
	maxTokens := 200
	summarization := &SummarizationConfig{
		KeepTokens: &keepTokens,
		MaxTokens:  &maxTokens,
		LLM: &SummarizationLLMConfig{
			Endpoint: "https://summarization.example.com",
			Auth: SummarizationAuthConfig{
				APIKeyEnv: "SUMMARIZE_KEY",
			},
			Model: "gpt-4.1-mini",
		},
	}
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, summarization, nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	expectedDir := filepath.Join(tmpHome, ".agyn", "agn")
	if agnDir != expectedDir {
		t.Fatalf("expected agn dir %q, got %q", expectedDir, agnDir)
	}

	expectedPath := filepath.Join(expectedDir, "config.yaml")
	if configPath != expectedPath {
		t.Fatalf("expected config path %q, got %q", expectedPath, configPath)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model) +
		"summarization:\n" +
		"  keep_tokens: 10\n" +
		"  max_tokens: 200\n" +
		"  llm:\n" +
		"    endpoint: https://summarization.example.com\n" +
		"    auth:\n" +
		"      api_key_env: SUMMARIZE_KEY\n" +
		"    model: gpt-4.1-mini\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}
