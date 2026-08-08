package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A vendor whose CLI reads its subscription credential from a file rather than
// an environment variable needs that file present before the CLI starts. The
// orchestrator cannot write it: the path is CLI-specific and resolves against
// HOME, which the platform does not manage. It arrives on the container spec
// instead, verbatim from the LLM service, so nothing here knows which vendor
// or which CLI it belongs to.
const (
	placeholderFilePathEnv     = "LLM_PLACEHOLDER_FILE_PATH"
	placeholderFileContentsEnv = "LLM_PLACEHOLDER_FILE_CONTENTS"
)

// writePlaceholderFile writes the file placeholder when one is declared. It
// runs in holder mode too: holder spawns nothing, but a sandbox shell opened
// hours later still finds the file, which is the whole reason this kind exists.
//
// An existing file is left alone. A real credential may already be there --
// written by the CLI's own login, or by a previous session -- and replacing it
// with a placeholder would log the engineer out of their own subscription.
func writePlaceholderFile() error {
	path := strings.TrimSpace(os.Getenv(placeholderFilePathEnv))
	if path == "" {
		return nil
	}
	if filepath.IsAbs(path) || strings.Contains(path, "..") {
		return fmt.Errorf("placeholder path %q must be relative to HOME", path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	target := filepath.Join(home, path)
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", target, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(os.Getenv(placeholderFileContentsEnv)), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}
