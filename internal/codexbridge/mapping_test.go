package codexbridge

import "testing"

func newMappingRecord(platformID, codexID string, createdAt, lastUsedAt int64) ThreadMappingRecord {
	return ThreadMappingRecord{
		InstanceID:       platformID,
		CodexThreadID:    codexID,
		CreatedAtUnixMs:  createdAt,
		LastUsedAtUnixMs: lastUsedAt,
	}
}

func TestThreadMappingSetGet(t *testing.T) {
	mapping := NewThreadMapping()
	mapping.SetRecord(newMappingRecord("instance-1", "codex-1", 1700000000000, 1700000000100))
	got, ok := mapping.CodexForInstance("instance-1")
	if !ok {
		t.Fatal("expected mapping to exist")
	}
	if got != "codex-1" {
		t.Fatalf("expected codex id codex-1, got %q", got)
	}
}

func TestThreadMappingBidirectional(t *testing.T) {
	mapping := NewThreadMapping()
	mapping.SetRecord(newMappingRecord("instance-2", "codex-2", 1700000000000, 1700000000200))
	got, ok := mapping.InstanceForCodex("codex-2")
	if !ok {
		t.Fatal("expected reverse mapping to exist")
	}
	if got != "instance-2" {
		t.Fatalf("expected platform id platform-2, got %q", got)
	}
}

func TestThreadMappingMissing(t *testing.T) {
	mapping := NewThreadMapping()
	if got, ok := mapping.CodexForInstance("missing"); ok || got != "" {
		t.Fatalf("expected no mapping, got %q", got)
	}
	if got, ok := mapping.InstanceForCodex("missing"); ok || got != "" {
		t.Fatalf("expected no reverse mapping, got %q", got)
	}
}

func TestThreadMappingOverwrite(t *testing.T) {
	mapping := NewThreadMapping()
	mapping.SetRecord(newMappingRecord("platform-3", "codex-3", 1700000000000, 1700000000300))
	mapping.SetRecord(newMappingRecord("platform-3", "codex-4", 1700000000000, 1700000000400))
	got, ok := mapping.CodexForInstance("platform-3")
	if !ok {
		t.Fatal("expected mapping to exist")
	}
	if got != "codex-4" {
		t.Fatalf("expected codex id codex-4, got %q", got)
	}
	got, ok = mapping.InstanceForCodex("codex-4")
	if !ok {
		t.Fatal("expected reverse mapping to exist")
	}
	if got != "platform-3" {
		t.Fatalf("expected platform id platform-3, got %q", got)
	}
}
