package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
)

const (
	testAgnAPIAddress         = "api.platform.svc.cluster.local:443"
	testAgnAPIAddressURL      = "https://api.platform.svc.cluster.local:443/v1"
	testTokenCountingAddress  = "token-counting.platform.svc.cluster.local:50051"
	testTokenCountingOverride = "token-counting.custom.svc.cluster.local:50052"
)

func setAgnAPIAddress(t *testing.T) {
	t.Helper()
	t.Setenv(agynAPIAddressEnvVar, testAgnAPIAddress)
}

func TestWriteAgnConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	setAgnAPIAddress(t)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, "", nil, nil)
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

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model, testTokenCountingAddress)
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

	content := agnConfig(baseURL, apiKey, model, testTokenCountingAddress, "", summarization, nil)
	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model, testTokenCountingAddress)
	if content != expected {
		t.Fatalf("expected config %q, got %q", expected, content)
	}
}

func TestWriteAgnConfigWithMCPServers(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	setAgnAPIAddress(t)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	mcpServers := []config.MCPServer{
		{Name: "memory", Port: 8100},
		{Name: "filesystem", Port: 8200},
	}
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, "", nil, mcpServers)
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

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model, testTokenCountingAddress) +
		"mcp:\n  servers:\n" +
		"    memory:\n      url: http://localhost:8100/mcp\n" +
		"    filesystem:\n      url: http://localhost:8200/mcp\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestWriteAgnConfigWithSummarizationThresholds(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	setAgnAPIAddress(t)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	keepTokens := 10
	maxTokens := 200
	summarization := &summarizationConfig{
		KeepTokens: &keepTokens,
		MaxTokens:  &maxTokens,
	}
	_, configPath, err := writeAgnConfig(baseURL, apiKey, model, "", summarization, nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model, testTokenCountingAddress) +
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
	setAgnAPIAddress(t)

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
	agnDir, configPath, err := writeAgnConfig(baseURL, apiKey, model, "", summarization, nil)
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

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model, testTokenCountingAddress) +
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
	setAgnAPIAddress(t)

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
	_, configPath, err := writeAgnConfig(baseURL, apiKey, model, "", summarization, nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model, testTokenCountingAddress) +
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

func TestWriteAgnConfigWithSystemPrompt(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	setAgnAPIAddress(t)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	model := "test-model-id"
	systemPrompt := "You are a helpful agent.\nFollow the guidelines."
	_, configPath, err := writeAgnConfig(baseURL, apiKey, model, systemPrompt, nil, nil)
	if err != nil {
		t.Fatalf("expected config to be written, got %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to be readable, got %v", err)
	}

	expected := fmt.Sprintf(agnConfigTemplate, baseURL, apiKey, model, testTokenCountingAddress) +
		"system_prompt: |-\n" +
		"  You are a helpful agent.\n" +
		"  Follow the guidelines.\n"
	if string(content) != expected {
		t.Fatalf("expected config %q, got %q", expected, string(content))
	}
}

func TestResolveTokenCountingAddressFromAPIAddress(t *testing.T) {
	setAgnAPIAddress(t)

	address, err := resolveTokenCountingAddress()
	if err != nil {
		t.Fatalf("expected address, got %v", err)
	}
	if address != testTokenCountingAddress {
		t.Fatalf("expected address %q, got %q", testTokenCountingAddress, address)
	}
}

func TestResolveTokenCountingAddressFromAPIAddressURL(t *testing.T) {
	t.Setenv(agynAPIAddressEnvVar, testAgnAPIAddressURL)

	address, err := resolveTokenCountingAddress()
	if err != nil {
		t.Fatalf("expected address, got %v", err)
	}
	if address != testTokenCountingAddress {
		t.Fatalf("expected address %q, got %q", testTokenCountingAddress, address)
	}
}

func TestResolveTokenCountingAddressDefault(t *testing.T) {
	t.Setenv(agynAPIAddressEnvVar, "")
	t.Setenv(agnTokenCountingAddressEnvVar, "")

	address, err := resolveTokenCountingAddress()
	if err != nil {
		t.Fatalf("expected address, got %v", err)
	}
	expected := fmt.Sprintf("%s:%d", tokenCountingServiceName, tokenCountingServicePort)
	if address != expected {
		t.Fatalf("expected address %q, got %q", expected, address)
	}
}

func TestResolveTokenCountingAddressOverride(t *testing.T) {
	setAgnAPIAddress(t)
	t.Setenv(agnTokenCountingAddressEnvVar, testTokenCountingOverride)

	address, err := resolveTokenCountingAddress()
	if err != nil {
		t.Fatalf("expected address, got %v", err)
	}
	if address != testTokenCountingOverride {
		t.Fatalf("expected address %q, got %q", testTokenCountingOverride, address)
	}
}
