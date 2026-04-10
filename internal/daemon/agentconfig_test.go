package daemon

import (
	"strings"
	"testing"
)

func TestParseAgentSummarizationValid(t *testing.T) {
	payload := `{"summarization":{"keepTokens":42,"max_tokens":100,"llm":{"endpoint":"https://sum.example.com","auth":{"apiKeyEnv":"SUM_KEY"},"model":"gpt-4.1-mini"}}}`

	cfg, err := parseAgentSummarization(payload)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected summarization config")
	}
	if cfg.KeepTokens == nil || *cfg.KeepTokens != 42 {
		t.Fatalf("unexpected keep tokens: %#v", cfg.KeepTokens)
	}
	if cfg.MaxTokens == nil || *cfg.MaxTokens != 100 {
		t.Fatalf("unexpected max tokens: %#v", cfg.MaxTokens)
	}
	if cfg.LLM == nil {
		t.Fatal("expected summarization LLM")
	}
	if cfg.LLM.Endpoint != "https://sum.example.com" {
		t.Fatalf("unexpected LLM endpoint: %s", cfg.LLM.Endpoint)
	}
	if cfg.LLM.Model != "gpt-4.1-mini" {
		t.Fatalf("unexpected LLM model: %s", cfg.LLM.Model)
	}
	if cfg.LLM.Auth.APIKeyEnv != "SUM_KEY" || cfg.LLM.Auth.APIKey != "" {
		t.Fatalf("unexpected LLM auth: %#v", cfg.LLM.Auth)
	}
}

func TestParseAgentSummarizationInvalidJSON(t *testing.T) {
	_, err := parseAgentSummarization("{")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse agent configuration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseAgentSummarizationInvalidAuth(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "missing-auth",
			payload: `{"summarization":{"llm":{"endpoint":"https://sum.example.com","auth":{},"model":"gpt-4.1-mini"}}}`,
		},
		{
			name:    "conflicting-auth",
			payload: `{"summarization":{"llm":{"endpoint":"https://sum.example.com","auth":{"api_key":"a","api_key_env":"ENV"},"model":"gpt-4.1-mini"}}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseAgentSummarization(test.payload)
			if err == nil {
				t.Fatalf("expected error for %s", test.name)
			}
			if !strings.Contains(err.Error(), "summarization.llm.auth") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
