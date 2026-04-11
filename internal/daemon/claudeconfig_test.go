package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
)

func TestWriteClaudeSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	if err := writeClaudeSettings(baseURL, apiKey, nil); err != nil {
		t.Fatalf("expected settings to be written, got %v", err)
	}

	settingsPath := filepath.Join(tmpHome, ".claude", "settings.json")
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected settings to be readable, got %v", err)
	}

	var got claudeSettings
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("expected settings to parse, got %v", err)
	}

	expected := claudeSettings{
		Permissions: claudePermissions{
			DefaultMode: "bypassPermissions",
			Allow: []string{
				"Bash",
				"Read",
				"Write",
				"Edit",
				"MultiEdit",
				"WebFetch",
				"WebSearch",
				"Grep",
				"Glob",
				"LS",
				"Task",
				"TodoWrite",
				"NotebookEdit",
			},
			Deny: []string{},
		},
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":                       baseURL,
			"ANTHROPIC_API_KEY":                        apiKey,
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"DISABLE_AUTOUPDATER":                      "1",
		},
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected settings %#v, got %#v", expected, got)
	}
}

func TestWriteClaudeSettingsWithMCPServers(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	baseURL := "https://example.com"
	apiKey := "test-api-key"
	mcpServers := []config.MCPServer{
		{Name: "memory", Port: 8100},
		{Name: "cache", Port: 8200},
	}
	if err := writeClaudeSettings(baseURL, apiKey, mcpServers); err != nil {
		t.Fatalf("expected settings to be written, got %v", err)
	}

	settingsPath := filepath.Join(tmpHome, ".claude", "settings.json")
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected settings to be readable, got %v", err)
	}

	var got claudeSettings
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("expected settings to parse, got %v", err)
	}

	expected := claudeSettings{
		Permissions: claudePermissions{
			DefaultMode: "bypassPermissions",
			Allow: []string{
				"Bash",
				"Read",
				"Write",
				"Edit",
				"MultiEdit",
				"WebFetch",
				"WebSearch",
				"Grep",
				"Glob",
				"LS",
				"Task",
				"TodoWrite",
				"NotebookEdit",
			},
			Deny: []string{},
		},
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":                       baseURL,
			"ANTHROPIC_API_KEY":                        apiKey,
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"DISABLE_AUTOUPDATER":                      "1",
		},
		MCPServers: map[string]claudeMCPServer{
			"memory": {Type: "http", URL: "http://localhost:8100/mcp"},
			"cache":  {Type: "http", URL: "http://localhost:8200/mcp"},
		},
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected settings %#v, got %#v", expected, got)
	}
}

func TestClaudeBaseURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strip-v1",
			input: "http://llm-proxy.ziti:443/v1",
			want:  "http://llm-proxy.ziti:443",
		},
		{
			name:  "strip-v1-trailing-slash",
			input: "http://llm-proxy.ziti:443/v1/",
			want:  "http://llm-proxy.ziti:443",
		},
		{
			name:  "no-strip",
			input: "http://llm-proxy.ziti:443/v1beta",
			want:  "http://llm-proxy.ziti:443/v1beta",
		},
		{
			name:  "already-base",
			input: "http://llm-proxy.ziti:443",
			want:  "http://llm-proxy.ziti:443",
		},
		{
			name:  "trim-space",
			input: " http://llm-proxy.ziti:443/v1 ",
			want:  "http://llm-proxy.ziti:443",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := claudeBaseURL(test.input)
			if got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}
