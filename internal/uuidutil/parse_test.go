package uuidutil

import "testing"

func TestParseUUIDValid(t *testing.T) {
	value := "550e8400-e29b-41d4-a716-446655440000"
	parsed, err := ParseUUID(value, "FIELD")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if parsed.String() != value {
		t.Fatalf("expected %s, got %s", value, parsed.String())
	}
}

func TestParseUUIDEmpty(t *testing.T) {
	_, err := ParseUUID("", "FIELD")
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}

func TestParseUUIDMalformed(t *testing.T) {
	_, err := ParseUUID("not-a-uuid", "FIELD")
	if err == nil {
		t.Fatal("expected error for malformed uuid")
	}
}
