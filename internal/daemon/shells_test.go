package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func withTmuxPaths(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	t.Cleanup(func(bin, cfg, sock, ti string) func() {
		return func() {
			tmuxBinaryPath, tmuxConfigPath, tmuxSocketDir, tmuxTerminfoPath = bin, cfg, sock, ti
		}
	}(tmuxBinaryPath, tmuxConfigPath, tmuxSocketDir, tmuxTerminfoPath))

	tmuxBinaryPath = filepath.Join(root, "tmux")
	tmuxConfigPath = filepath.Join(root, "tmux.conf")
	tmuxSocketDir = filepath.Join(root, "run")
	tmuxTerminfoPath = filepath.Join(root, "terminfo")
	return root
}

func stubTmuxCommand(t *testing.T, capture *[]string, captureEnv *[]string) {
	t.Helper()
	prev := tmuxCommandContext
	t.Cleanup(func() { tmuxCommandContext = prev })

	tmuxCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		*capture = append([]string{name}, args...)
		cmd := exec.CommandContext(ctx, "true")
		// The caller sets cmd.Env after we return, so record it lazily by
		// handing back a command whose Env the caller will overwrite.
		t.Cleanup(func() { *captureEnv = cmd.Env })
		return cmd
	}
}

// A missing binary must be survivable: an image whose multiplexer did not
// arrive still serves ephemeral sessions, and losing those to a panic would
// cost the terminal rather than just persistence.
func TestStartShellServerMissingBinaryIsNotFatal(t *testing.T) {
	withTmuxPaths(t)

	called := false
	prev := tmuxCommandContext
	t.Cleanup(func() { tmuxCommandContext = prev })
	tmuxCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		called = true
		return exec.CommandContext(ctx, "true")
	}

	startShellServer(context.Background())

	if called {
		t.Fatal("started a server with no binary present")
	}
}

func TestStartShellServerInvocation(t *testing.T) {
	withTmuxPaths(t)
	if err := os.WriteFile(tmuxBinaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var argv, env []string
	stubTmuxCommand(t, &argv, &env)

	startShellServer(context.Background())

	want := []string{tmuxBinaryPath, "-L", "agyn", "-f", tmuxConfigPath, "start-server"}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}

	// The socket directory is normally the init container's to create; agynd
	// creating it as a fallback is what keeps a hand-run container working.
	if info, err := os.Stat(tmuxSocketDir); err != nil || !info.IsDir() {
		t.Fatalf("socket dir not created: %v", err)
	}
}

// The socket must not land on tmux's default path, or an engineer running
// their own tmux inside a shell joins the platform's server and silently
// loses their own configuration to it.
func TestShellServerEnvNamesPrivateSocketDir(t *testing.T) {
	withTmuxPaths(t)

	if !slices.Contains(shellServerEnv(), "TMUX_TMPDIR="+tmuxSocketDir) {
		t.Fatalf("TMUX_TMPDIR not set to %s", tmuxSocketDir)
	}
}

// TERMINFO_DIRS supplements rather than replaces: the trailing empty element
// is what ncurses reads as "and the compiled-in defaults too", so an image
// carrying a good database of its own keeps it.
func TestShellServerEnvTerminfoOnlyWhenPresent(t *testing.T) {
	withTmuxPaths(t)

	hasTerminfo := func() bool {
		for _, kv := range shellServerEnv() {
			if strings.HasPrefix(kv, "TERMINFO_DIRS=") {
				if !strings.HasSuffix(kv, ":") {
					t.Fatalf("TERMINFO_DIRS must end in an empty element, got %q", kv)
				}
				return true
			}
		}
		return false
	}

	if hasTerminfo() {
		t.Fatal("TERMINFO_DIRS set with no tree delivered")
	}

	if err := os.MkdirAll(tmuxTerminfoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasTerminfo() {
		t.Fatal("TERMINFO_DIRS not set with a tree present")
	}
}
