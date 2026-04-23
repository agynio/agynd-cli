package daemon

import (
	"path/filepath"
	"testing"
)

func TestValidateSkillNameTraversal(t *testing.T) {
	for _, name := range []string{".", ".."} {
		if err := validateSkillName(name); err == nil {
			t.Fatalf("expected error for skill name %q", name)
		}
	}
}

func TestSkillsDirForSDKCodexHomeFallback(t *testing.T) {
	t.Setenv("HOME", "")

	dir, err := skillsDirForSDK(SDKCodex)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expected := filepath.Join(codexDefaultHome, ".codex", "skills")
	if dir != expected {
		t.Fatalf("expected skills dir %q, got %q", expected, dir)
	}
}
