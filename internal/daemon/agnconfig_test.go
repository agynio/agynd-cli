package daemon

import (
	"os"
	"testing"
)

func TestWriteAgnConfig(t *testing.T) {
	dir, path, err := writeAgnConfig("https://llm.example.test")
	if err != nil {
		t.Fatalf("writeAgnConfig returned error: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	expected := "llm:\n  endpoint: https://llm.example.test\n  auth:\n    api_key: platform\n  model: default\n"
	if string(content) != expected {
		t.Fatalf("unexpected config content:\n%s", string(content))
	}
}

func TestWriteAgnConfigRequiresBaseURL(t *testing.T) {
	if _, _, err := writeAgnConfig(" "); err == nil {
		t.Fatalf("expected error for empty base URL")
	}
}
