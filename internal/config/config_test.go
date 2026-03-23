package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validAgentID = "550e8400-e29b-41d4-a716-446655440000"

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_ID", validAgentID)
	t.Setenv("GATEWAY_ADDRESS", "gateway:1234")
}

func writeAgentConfig(t *testing.T, sdk, bin string) {
	t.Helper()
	payload := fmt.Sprintf(`{"sdk":%q,"bin":%q}`, sdk, bin)
	writeAgentConfigRaw(t, payload)
}

func writeAgentConfigRaw(t *testing.T, payload string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	oldPath := agentConfigPath
	agentConfigPath = configPath
	t.Cleanup(func() {
		agentConfigPath = oldPath
	})
}

func TestFromEnvValid(t *testing.T) {
	setRequiredEnv(t)
	writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("WORKSPACE_DIR", "/tmp/workdir")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.AgentID.String() != validAgentID {
		t.Fatalf("unexpected agent id: %s", cfg.AgentID.String())
	}
	if cfg.GatewayAddress != "gateway:1234" {
		t.Fatalf("unexpected gateway address: %s", cfg.GatewayAddress)
	}
	if cfg.SDK != "codex" {
		t.Fatalf("unexpected sdk: %s", cfg.SDK)
	}
	if cfg.AgentBinary != "/opt/bin/codex" {
		t.Fatalf("unexpected agent binary: %s", cfg.AgentBinary)
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
		{name: "gateway", missing: "GATEWAY_ADDRESS", expected: "GATEWAY_ADDRESS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredEnv(t)
			writeAgentConfig(t, "codex", "/opt/bin/codex")
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
	writeAgentConfig(t, "codex", "/opt/bin/codex")
	t.Setenv("WORKSPACE_DIR", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.WorkDir != "/workspace" {
		t.Fatalf("expected default workspace dir, got %s", cfg.WorkDir)
	}
}

func TestFromEnvMissingConfigFile(t *testing.T) {
	setRequiredEnv(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	oldPath := agentConfigPath
	agentConfigPath = configPath
	t.Cleanup(func() {
		agentConfigPath = oldPath
	})

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromEnvMissingConfigFields(t *testing.T) {
	setRequiredEnv(t)
	writeAgentConfigRaw(t, `{}`)
	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error for missing sdk")
	}
	if !strings.Contains(err.Error(), "missing sdk") {
		t.Fatalf("unexpected error: %v", err)
	}

	writeAgentConfigRaw(t, `{"sdk":"codex"}`)
	_, err = FromEnv()
	if err == nil {
		t.Fatal("expected error for missing bin")
	}
	if !strings.Contains(err.Error(), "missing bin") {
		t.Fatalf("unexpected error: %v", err)
	}
}
