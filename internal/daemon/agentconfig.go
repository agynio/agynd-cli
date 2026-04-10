package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
)

type summarizationConfig struct {
	KeepTokens *int
	MaxTokens  *int
	LLM        *summarizationLLMConfig
}

type summarizationLLMConfig struct {
	Endpoint string
	Auth     summarizationAuthConfig
	Model    string
}

type summarizationAuthConfig struct {
	APIKey    string
	APIKeyEnv string
}

type agentConfigurationPayload struct {
	Summarization *summarizationPayload `json:"summarization"`
}

type summarizationPayload struct {
	KeepTokens *int                     `json:"keep_tokens"`
	MaxTokens  *int                     `json:"max_tokens"`
	LLM        *summarizationLLMPayload `json:"llm"`
}

type summarizationLLMPayload struct {
	Endpoint string                   `json:"endpoint"`
	Auth     summarizationAuthPayload `json:"auth"`
	Model    string                   `json:"model"`
}

type summarizationAuthPayload struct {
	APIKey    string `json:"api_key"`
	APIKeyEnv string `json:"api_key_env"`
}

func parseAgentSummarization(raw string) (*summarizationConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var payload agentConfigurationPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("parse agent configuration: %w", err)
	}

	return normalizeSummarizationPayload(payload.Summarization)
}

func normalizeSummarizationPayload(payload *summarizationPayload) (*summarizationConfig, error) {
	if payload == nil {
		return nil, nil
	}

	cfg := summarizationConfig{
		KeepTokens: payload.KeepTokens,
		MaxTokens:  payload.MaxTokens,
	}

	if payload.LLM != nil {
		llm, err := normalizeSummarizationLLM(payload.LLM)
		if err != nil {
			return nil, err
		}
		cfg.LLM = llm
	}

	if cfg.KeepTokens == nil && cfg.MaxTokens == nil && cfg.LLM == nil {
		return nil, nil
	}

	return &cfg, nil
}

func normalizeSummarizationLLM(payload *summarizationLLMPayload) (*summarizationLLMConfig, error) {
	endpoint := strings.TrimSpace(payload.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("summarization.llm.endpoint is required")
	}

	model := strings.TrimSpace(payload.Model)
	if model == "" {
		return nil, fmt.Errorf("summarization.llm.model is required")
	}

	auth, err := normalizeSummarizationAuth(payload.Auth)
	if err != nil {
		return nil, err
	}

	return &summarizationLLMConfig{
		Endpoint: endpoint,
		Auth:     auth,
		Model:    model,
	}, nil
}

func normalizeSummarizationAuth(payload summarizationAuthPayload) (summarizationAuthConfig, error) {
	apiKey := strings.TrimSpace(payload.APIKey)
	apiKeyEnv := strings.TrimSpace(payload.APIKeyEnv)
	if apiKey == "" && apiKeyEnv == "" {
		return summarizationAuthConfig{}, fmt.Errorf("summarization.llm.auth requires api_key or api_key_env")
	}
	if apiKey != "" && apiKeyEnv != "" {
		return summarizationAuthConfig{}, fmt.Errorf("summarization.llm.auth supports only one of api_key or api_key_env")
	}

	return summarizationAuthConfig{APIKey: apiKey, APIKeyEnv: apiKeyEnv}, nil
}
