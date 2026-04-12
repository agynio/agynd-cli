package daemon

import (
	"os"
	"testing"
)

func TestPrependCLIPathEmpty(t *testing.T) {
	got := prependCLIPath("")
	if got != cliPathPrefix {
		t.Fatalf("expected %q, got %q", cliPathPrefix, got)
	}
}

func TestPrependCLIPathExisting(t *testing.T) {
	basePath := "/usr/local/bin"
	expected := cliPathPrefix + string(os.PathListSeparator) + basePath
	got := prependCLIPath(basePath)
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}
