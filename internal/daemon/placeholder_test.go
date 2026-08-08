package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePlaceholderFileCreatesTheFileAndItsDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(placeholderFilePathEnv, ".codex/auth.json")
	t.Setenv(placeholderFileContentsEnv, `{"tokens":{"access_token":"placeholder"}}`)

	if err := writePlaceholderFile(); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		t.Fatalf("read placeholder: %v", err)
	}
	if string(contents) != `{"tokens":{"access_token":"placeholder"}}` {
		t.Fatalf("contents = %s", contents)
	}
}

// A real credential may already be there -- the CLI's own login, or a previous
// session -- and replacing it with a placeholder would log the engineer out of
// their own subscription.
func TestWritePlaceholderFileLeavesAnExistingFileAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(placeholderFilePathEnv, ".codex/auth.json")
	t.Setenv(placeholderFileContentsEnv, "placeholder")

	target := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := os.WriteFile(target, []byte("the engineer's own token"), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if err := writePlaceholderFile(); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "the engineer's own token" {
		t.Fatalf("an existing credential was overwritten: %s", contents)
	}
}

// Nothing declared is the platform-mode and Anthropic cases, which must not
// leave a stray file behind.
func TestWritePlaceholderFileIsANoOpWithoutAPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(placeholderFilePathEnv, "")

	if err := writePlaceholderFile(); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read home: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("home is not empty: %v", entries)
	}
}

// The path comes from the container environment, which a workload's own ENVs
// can also reach. Escaping HOME has to be refused rather than written.
func TestWritePlaceholderFileRefusesAnEscapingPath(t *testing.T) {
	for _, path := range []string{"/etc/passwd", "../../etc/passwd"} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv(placeholderFilePathEnv, path)
		t.Setenv(placeholderFileContentsEnv, "x")

		if err := writePlaceholderFile(); err == nil {
			t.Fatalf("path %q was accepted", path)
		}
	}
}
