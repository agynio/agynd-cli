package config

import (
	"strings"
	"testing"
)

const validAgentID = "550e8400-e29b-41d4-a716-446655440000"

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_ID", validAgentID)
	t.Setenv("THREADS_ADDRESS", "threads:1234")
	t.Setenv("NOTIFICATIONS_ADDRESS", "notifs:2345")
	t.Setenv("TEAMS_ADDRESS", "teams:3456")
	t.Setenv("OPENAI_API_KEY", "key")
}

func TestFromEnvValid(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CODEX_BINARY", "codex-custom")
	t.Setenv("WORKSPACE_DIR", "/tmp/workdir")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.AgentID.String() != validAgentID {
		t.Fatalf("unexpected agent id: %s", cfg.AgentID.String())
	}
	if cfg.ThreadsAddress != "threads:1234" {
		t.Fatalf("unexpected threads address: %s", cfg.ThreadsAddress)
	}
	if cfg.NotificationsAddress != "notifs:2345" {
		t.Fatalf("unexpected notifications address: %s", cfg.NotificationsAddress)
	}
	if cfg.TeamsAddress != "teams:3456" {
		t.Fatalf("unexpected teams address: %s", cfg.TeamsAddress)
	}
	if cfg.OpenAIAPIKey != "key" {
		t.Fatalf("unexpected openai api key: %s", cfg.OpenAIAPIKey)
	}
	if cfg.CodexBinary != "codex-custom" {
		t.Fatalf("unexpected codex binary: %s", cfg.CodexBinary)
	}
	if cfg.WorkDir != "/tmp/workdir" {
		t.Fatalf("unexpected work dir: %s", cfg.WorkDir)
	}
}

func TestFromEnvMissingRequired(t *testing.T) {
	tests := []struct {
		name     string
		missing  string
		expected string
	}{
		{name: "agent-id", missing: "AGENT_ID", expected: "AGENT_ID"},
		{name: "threads", missing: "THREADS_ADDRESS", expected: "THREADS_ADDRESS"},
		{name: "notifications", missing: "NOTIFICATIONS_ADDRESS", expected: "NOTIFICATIONS_ADDRESS"},
		{name: "teams", missing: "TEAMS_ADDRESS", expected: "TEAMS_ADDRESS"},
		{name: "openai", missing: "OPENAI_API_KEY", expected: "OPENAI_API_KEY"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(test.missing, "")
			_, err := FromEnv()
			if err == nil {
				t.Fatalf("expected error for missing %s", test.missing)
			}
			if !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFromEnvDefaults(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CODEX_BINARY", "")
	t.Setenv("WORKSPACE_DIR", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.CodexBinary != "codex" {
		t.Fatalf("expected default codex binary, got %s", cfg.CodexBinary)
	}
	if cfg.WorkDir != "/workspace" {
		t.Fatalf("expected default workspace dir, got %s", cfg.WorkDir)
	}
}
