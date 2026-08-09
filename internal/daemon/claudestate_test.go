package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readState(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, claudeStateFileName))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	state := map[string]any{}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	return state
}

func TestWriteClaudeStateCreatesTheFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := writeClaudeState(); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := readState(t, home)
	for key, want := range claudeStateKeys {
		if state[key] != want {
			t.Fatalf("%s = %v, want %v", key, state[key], want)
		}
	}
}

// The file is the CLI's own state and accumulates across runs, so the keys are
// merged into whatever is already there.
func TestWriteClaudeStateKeepsExistingKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	existing := `{"numStartups":7,"projects":{"/workspace":{"allowedTools":["Bash"]}}}`
	if err := os.WriteFile(filepath.Join(home, claudeStateFileName), []byte(existing), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if err := writeClaudeState(); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := readState(t, home)
	if state["numStartups"] != float64(7) {
		t.Fatalf("numStartups = %v, want 7", state["numStartups"])
	}
	if _, ok := state["projects"].(map[string]any)["/workspace"]; !ok {
		t.Fatalf("projects lost: %v", state["projects"])
	}
	if state["hasCompletedOnboarding"] != true {
		t.Fatalf("hasCompletedOnboarding = %v", state["hasCompletedOnboarding"])
	}
}

// A key the CLI already set is the CLI's answer, not ours to overrule.
func TestWriteClaudeStateDoesNotOverwriteAKeyItSets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, claudeStateFileName), []byte(`{"installMethod":"brew"}`), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if err := writeClaudeState(); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if state := readState(t, home); state["installMethod"] != "brew" {
		t.Fatalf("installMethod = %v, want brew", state["installMethod"])
	}
}

// Refusing to start over an unreadable state file would strand every workload
// carrying one.
func TestWriteClaudeStateStartsOverOnUnparseableJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, claudeStateFileName), []byte("not json at all"), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if err := writeClaudeState(); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if state := readState(t, home); state["hasCompletedOnboarding"] != true {
		t.Fatalf("hasCompletedOnboarding = %v", state["hasCompletedOnboarding"])
	}
}

func TestWriteClaudeStateIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := writeClaudeState(); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, err := os.Stat(filepath.Join(home, claudeStateFileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := writeClaudeState(); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, err := os.Stat(filepath.Join(home, claudeStateFileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("rewrote a file that needed no change")
	}
}
