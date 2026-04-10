package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
)

type SummarizationConfig struct {
	KeepTokens *int
	MaxTokens  *int
	LLM        *SummarizationLLMConfig
}

type SummarizationLLMConfig struct {
	Endpoint string
	Auth     SummarizationAuthConfig
	Model    string
}

type SummarizationAuthConfig struct {
	APIKey    string
	APIKeyEnv string
}

type agentConfigurationPayload struct {
	Summarization *summarizationPayload `json:"summarization"`
}

type summarizationPayload struct {
	KeepTokens    *int                     `json:"keep_tokens"`
	KeepTokensAlt *int                     `json:"keepTokens"`
	MaxTokens     *int                     `json:"max_tokens"`
	MaxTokensAlt  *int                     `json:"maxTokens"`
	LLM           *summarizationLLMPayload `json:"llm"`
}

type summarizationLLMPayload struct {
	Endpoint string                   `json:"endpoint"`
	Auth     summarizationAuthPayload `json:"auth"`
	Model    string                   `json:"model"`
}

type summarizationAuthPayload struct {
	APIKey       string `json:"api_key"`
	APIKeyAlt    string `json:"apiKey"`
	APIKeyEnv    string `json:"api_key_env"`
	APIKeyEnvAlt string `json:"apiKeyEnv"`
}

func parseAgentSummarization(raw string) (*SummarizationConfig, error) {
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

func normalizeSummarizationPayload(payload *summarizationPayload) (*SummarizationConfig, error) {
	if payload == nil {
		return nil, nil
	}

	keepTokens, err := pickOptionalInt(
		payload.KeepTokens,
		payload.KeepTokensAlt,
		"summarization.keep_tokens",
		"summarization.keepTokens",
	)
	if err != nil {
		return nil, err
	}

	maxTokens, err := pickOptionalInt(
		payload.MaxTokens,
		payload.MaxTokensAlt,
		"summarization.max_tokens",
		"summarization.maxTokens",
	)
	if err != nil {
		return nil, err
	}

	cfg := SummarizationConfig{
		KeepTokens: keepTokens,
		MaxTokens:  maxTokens,
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

func normalizeSummarizationLLM(payload *summarizationLLMPayload) (*SummarizationLLMConfig, error) {
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

	return &SummarizationLLMConfig{
		Endpoint: endpoint,
		Auth:     auth,
		Model:    model,
	}, nil
}

func normalizeSummarizationAuth(payload summarizationAuthPayload) (SummarizationAuthConfig, error) {
	apiKey, err := pickOptionalString(
		payload.APIKey,
		payload.APIKeyAlt,
		"summarization.llm.auth.api_key",
		"summarization.llm.auth.apiKey",
	)
	if err != nil {
		return SummarizationAuthConfig{}, err
	}

	apiKeyEnv, err := pickOptionalString(
		payload.APIKeyEnv,
		payload.APIKeyEnvAlt,
		"summarization.llm.auth.api_key_env",
		"summarization.llm.auth.apiKeyEnv",
	)
	if err != nil {
		return SummarizationAuthConfig{}, err
	}

	if apiKey == "" && apiKeyEnv == "" {
		return SummarizationAuthConfig{}, fmt.Errorf("summarization.llm.auth requires api_key or api_key_env")
	}
	if apiKey != "" && apiKeyEnv != "" {
		return SummarizationAuthConfig{}, fmt.Errorf("summarization.llm.auth supports only one of api_key or api_key_env")
	}

	return SummarizationAuthConfig{APIKey: apiKey, APIKeyEnv: apiKeyEnv}, nil
}

func pickOptionalInt(primary, fallback *int, field, altField string) (*int, error) {
	if primary != nil && fallback != nil && *primary != *fallback {
		return nil, fmt.Errorf("%s conflicts with %s", field, altField)
	}
	if primary != nil {
		return primary, nil
	}
	return fallback, nil
}

func pickOptionalString(primary, fallback, field, altField string) (string, error) {
	primary = strings.TrimSpace(primary)
	fallback = strings.TrimSpace(fallback)
	if primary != "" && fallback != "" && primary != fallback {
		return "", fmt.Errorf("%s conflicts with %s", field, altField)
	}
	if primary != "" {
		return primary, nil
	}
	return fallback, nil
}
