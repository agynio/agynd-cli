package codexbridge

import "testing"

func TestThreadMappingSetGet(t *testing.T) {
	mapping := NewThreadMapping()
	mapping.Set("platform-1", "codex-1")
	got, ok := mapping.CodexForPlatform("platform-1")
	if !ok {
		t.Fatal("expected mapping to exist")
	}
	if got != "codex-1" {
		t.Fatalf("expected codex id codex-1, got %q", got)
	}
}

func TestThreadMappingBidirectional(t *testing.T) {
	mapping := NewThreadMapping()
	mapping.Set("platform-2", "codex-2")
	got, ok := mapping.PlatformForCodex("codex-2")
	if !ok {
		t.Fatal("expected reverse mapping to exist")
	}
	if got != "platform-2" {
		t.Fatalf("expected platform id platform-2, got %q", got)
	}
}

func TestThreadMappingMissing(t *testing.T) {
	mapping := NewThreadMapping()
	if got, ok := mapping.CodexForPlatform("missing"); ok || got != "" {
		t.Fatalf("expected no mapping, got %q", got)
	}
	if got, ok := mapping.PlatformForCodex("missing"); ok || got != "" {
		t.Fatalf("expected no reverse mapping, got %q", got)
	}
}

func TestThreadMappingOverwrite(t *testing.T) {
	mapping := NewThreadMapping()
	mapping.Set("platform-3", "codex-3")
	mapping.Set("platform-3", "codex-4")
	got, ok := mapping.CodexForPlatform("platform-3")
	if !ok {
		t.Fatal("expected mapping to exist")
	}
	if got != "codex-4" {
		t.Fatalf("expected codex id codex-4, got %q", got)
	}
	got, ok = mapping.PlatformForCodex("codex-4")
	if !ok {
		t.Fatal("expected reverse mapping to exist")
	}
	if got != "platform-3" {
		t.Fatalf("expected platform id platform-3, got %q", got)
	}
}
