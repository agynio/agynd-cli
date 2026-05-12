package daemon

import (
	"os"
	"testing"
)

func TestPrependCLIPathEmpty(t *testing.T) {
	got := prependCLIPath("")
	expected := agentPathPrefix()
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestPrependCLIPathExisting(t *testing.T) {
	basePath := "/usr/local/bin"
	expected := agentPathPrefix() + string(os.PathListSeparator) + basePath
	got := prependCLIPath(basePath)
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestCodexHomeEnvDefault(t *testing.T) {
	t.Setenv("HOME", "")
	if got := codexHomeEnv(); got != codexDefaultHome {
		t.Fatalf("expected %q, got %q", codexDefaultHome, got)
	}
}

func TestCodexHomeEnvUsesHome(t *testing.T) {
	t.Setenv("HOME", "/custom/home")
	if got := codexHomeEnv(); got != "/custom/home" {
		t.Fatalf("expected /custom/home, got %q", got)
	}
}
