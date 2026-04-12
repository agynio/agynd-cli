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
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, nil, "127.0.0.1", nil)
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

func TestAgnConfigNoSummarizationInJSON(t *testing.T) {
	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	configJSON := `{"system_prompt":"hello"}`

	summarization, err := parseAgentSummarization(configJSON)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summarization != nil {
		t.Fatalf("expected nil summarization, got %#v", summarization)
	}

	content := agnConfig(baseURL, apiKey, model, summarization, "127.0.0.1", nil)
	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model)
	if content != expected {
		t.Fatalf("expected config %q, got %q", expected, content)
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
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, nil, "127.0.0.1", mcpServers)
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
		"    memory:\n      url: http://127.0.0.1:8100/mcp\n" +
		"    filesystem:\n      url: http://127.0.0.1:8200/mcp\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteAgnConfigWithSummarizationThresholds(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	keepTokens := 10
	maxTokens := 200
	summarization := &summarizationConfig{
		KeepTokens: &keepTokens,
		MaxTokens:  &maxTokens,
	}
	_, configPath, err := writeAgnConfig(baseURL, apiKey, model, summarization, "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model) +
		"summarization:\n" +
		"  keep_tokens: 10\n" +
		"  max_tokens: 200\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteAgnConfigWithSummarizationLLMAPIKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	keepTokens := 10
	maxTokens := 200
	summarization := &summarizationConfig{
		KeepTokens: &keepTokens,
		MaxTokens:  &maxTokens,
		LLM: &summarizationLLMConfig{
			Endpoint: "https://summarization.example.com",
			Auth: summarizationAuthConfig{
				APIKey: "sum-key",
			},
			Model: "gpt-4.1-mini",
		},
	}
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, summarization, "127.0.0.1", nil)
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
		"      api_key: sum-key\n" +
		"    model: gpt-4.1-mini\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteAgnConfigWithSummarizationLLMAPIKeyEnv(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	keepTokens := 10
	maxTokens := 200
	summarization := &summarizationConfig{
		KeepTokens: &keepTokens,
		MaxTokens:  &maxTokens,
		LLM: &summarizationLLMConfig{
			Endpoint: "https://summarization.example.com",
			Auth: summarizationAuthConfig{
				APIKeyEnv: "SUMMARIZE_KEY",
			},
			Model: "gpt-4.1-mini",
		},
	}
	_, configPath, err := writeAgnConfig(baseURL, apiKey, model, summarization, "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
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
