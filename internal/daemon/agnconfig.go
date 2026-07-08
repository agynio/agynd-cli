package daemon

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/agynio/agynd-cli/internal/config"
)

const (
	agynAPIAddressEnvVar          = "AGYN_API_ADDRESS"
	agnTokenCountingAddressEnvVar = "AGN_TOKEN_COUNTING_ADDRESS"
	tokenCountingServiceName      = "token-counting"
	tokenCountingServicePort      = 50051
)

const agnConfigTemplate = `llm:
  endpoint: %s
  auth:
    api_key: %s
  model: %s
token_counting:
  address: %s
`

func writeAgnConfig(llmBaseURL, apiKey, model, systemPrompt string, summarization *summarizationConfig, mcpServers []config.MCPServer) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	tokenCountingAddress, err := resolveTokenCountingAddress()
	if err != nil {
		return "", "", fmt.Errorf("resolve token counting address: %w", err)
	}
	agnDir := filepath.Join(home, ".agyn", "agn")
	if err := os.MkdirAll(agnDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create agn config dir: %w", err)
	}
	configPath := filepath.Join(agnDir, "config.yaml")
	payload := agnConfig(llmBaseURL, apiKey, model, tokenCountingAddress, systemPrompt, summarization, mcpServers)
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		return "", "", fmt.Errorf("write agn config: %w", err)
	}
	return agnDir, configPath, nil
}

func agnConfig(llmBaseURL, apiKey, model, tokenCountingAddress, systemPrompt string, summarization *summarizationConfig, mcpServers []config.MCPServer) string {
	payload := fmt.Sprintf(agnConfigTemplate, llmBaseURL, apiKey, model, tokenCountingAddress)
	cleanPrompt := strings.TrimSpace(systemPrompt)
	if summarization == nil && len(mcpServers) == 0 && cleanPrompt == "" {
		return payload
	}
	var builder strings.Builder
	builder.WriteString(payload)
	if summarization != nil {
		appendSummarizationConfig(&builder, summarization)
	}
	if len(mcpServers) > 0 {
		builder.WriteString("mcp:\n  servers:\n")
		for _, server := range mcpServers {
			url := mcpEndpoint(server.Port)
			fmt.Fprintf(&builder, "    %s:\n      url: %s\n", server.Name, url)
		}
	}
	if cleanPrompt != "" {
		appendSystemPrompt(&builder, cleanPrompt)
	}
	return builder.String()
}

func appendSummarizationConfig(builder *strings.Builder, summarization *summarizationConfig) {
	builder.WriteString("summarization:\n")
	if summarization.KeepTokens != nil {
		fmt.Fprintf(builder, "  keep_tokens: %d\n", *summarization.KeepTokens)
	}
	if summarization.MaxTokens != nil {
		fmt.Fprintf(builder, "  max_tokens: %d\n", *summarization.MaxTokens)
	}
	if summarization.LLM == nil {
		return
	}
	builder.WriteString("  llm:\n")
	fmt.Fprintf(builder, "    endpoint: %s\n", summarization.LLM.Endpoint)
	builder.WriteString("    auth:\n")
	if summarization.LLM.Auth.APIKey != "" {
		fmt.Fprintf(builder, "      api_key: %s\n", summarization.LLM.Auth.APIKey)
	}
	if summarization.LLM.Auth.APIKeyEnv != "" {
		fmt.Fprintf(builder, "      api_key_env: %s\n", summarization.LLM.Auth.APIKeyEnv)
	}
	fmt.Fprintf(builder, "    model: %s\n", summarization.LLM.Model)
}

func appendSystemPrompt(builder *strings.Builder, prompt string) {
	builder.WriteString("system_prompt: |-\n")
	for _, line := range strings.Split(prompt, "\n") {
		fmt.Fprintf(builder, "  %s\n", line)
	}
}

func resolveTokenCountingAddress() (string, error) {
	override := strings.TrimSpace(os.Getenv(agnTokenCountingAddressEnvVar))
	if override != "" {
		return override, nil
	}
	apiAddress := strings.TrimSpace(os.Getenv(agynAPIAddressEnvVar))
	if apiAddress == "" {
		return fmt.Sprintf("%s:%d", tokenCountingServiceName, tokenCountingServicePort), nil
	}
	host, err := apiHost(apiAddress)
	if err != nil {
		return "", err
	}
	suffix, err := serviceSuffix(host)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s:%d", tokenCountingServiceName, suffix, tokenCountingServicePort), nil
}

func apiHost(address string) (string, error) {
	if strings.Contains(address, "://") {
		parsed, err := url.Parse(address)
		if err != nil {
			return "", fmt.Errorf("parse %s %q: %w", agynAPIAddressEnvVar, address, err)
		}
		address = parsed.Host
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("%s is empty", agynAPIAddressEnvVar)
	}
	if strings.Contains(address, "/") {
		address = strings.Split(address, "/")[0]
	}
	if strings.Contains(address, ":") {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return "", fmt.Errorf("%s %q must be host:port", agynAPIAddressEnvVar, address)
		}
		address = host
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("%s is empty", agynAPIAddressEnvVar)
	}
	return address, nil
}

func serviceSuffix(host string) (string, error) {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("%s %q missing namespace", agynAPIAddressEnvVar, host)
	}
	suffix := strings.Join(parts[1:], ".")
	if strings.TrimSpace(suffix) == "" {
		return "", fmt.Errorf("%s %q missing namespace", agynAPIAddressEnvVar, host)
	}
	return suffix, nil
}
