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
	if err := writeClaudeSettings(baseURL, apiKey, nil, false); err != nil {
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
		Hooks: traceHooks(),
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
		SkipDangerousModePermissionPrompt: true,
		Theme:                             "dark",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":                       baseURL,
			"ANTHROPIC_AUTH_TOKEN":                     apiKey,
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
	if err := writeClaudeSettings(baseURL, apiKey, mcpServers, false); err != nil {
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
		Hooks: traceHooks(),
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
		SkipDangerousModePermissionPrompt: true,
		Theme:                             "dark",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":                       baseURL,
			"ANTHROPIC_AUTH_TOKEN":                     apiKey,
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"DISABLE_AUTOUPDATER":                      "1",
		},
		MCPServers: map[string]claudeMCPServer{
			"memory": {Type: "http", URL: "http://127.0.0.1:8100/mcp"},
			"cache":  {Type: "http", URL: "http://127.0.0.1:8200/mcp"},
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

// The hook is how a turn is recorded at all: Claude Code's own telemetry
// reports that a call happened, and the transcript it hands the hook is what
// says what was in it.
func TestWriteClaudeSettingsRegistersTheTraceHook(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := writeClaudeSettings("https://example.com", "test-api-key", nil, false); err != nil {
		t.Fatalf("write claude settings: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpHome, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings claudeSettings
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}

	// SessionEnd as well as Stop: a session can end with a turn whose
	// completion never fired.
	for _, event := range []string{"Stop", "SessionEnd"} {
		matchers, ok := settings.Hooks[event]
		if !ok || len(matchers) != 1 || len(matchers[0].Hooks) != 1 {
			t.Fatalf("expected one %s hook, got %#v", event, settings.Hooks[event])
		}
		hook := matchers[0].Hooks[0]
		if hook.Type != "command" || hook.Command != traceHookCommand {
			t.Fatalf("unexpected %s hook: %#v", event, hook)
		}
	}
}

// Without the disclaimer accepted the CLI downgrades bypassPermissions to the
// default mode, so the permissions block alone does not survive contact.
func TestWriteClaudeSettingsAcceptsTheBypassDisclaimer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := writeClaudeSettings("http://llm-proxy.ziti:443", "platform", nil, false); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings claudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if !settings.SkipDangerousModePermissionPrompt {
		t.Fatal("bypass disclaimer not accepted")
	}
	if settings.Theme == "" {
		t.Fatal("theme not set")
	}
}
