package daemon

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The tmux server behind persistent shells.
//
// agynd starts it rather than letting the first client spawn it, for two
// reasons. A tmux server captures its environment from whoever starts it, and
// shells created hours apart must not differ by which client happened to
// arrive first. And a server already running is one less thing for an attach
// to do at the moment a person is waiting on it.
//
// Nothing above the container knows tmux is what implements a shell. The
// Terminal Proxy binds an attach-or-create command and the clients name a
// shell by an opaque id, which is what keeps the engine replaceable.

const (
	tmuxBinary = agentBinPath + "/tmux"
	tmuxConfig = "/agyn/tmux.conf"

	// Off the default socket. An engineer running plain tmux inside a shell
	// gets their own server and their own ~/.tmux.conf: tmux configuration is
	// server-wide, so sharing one would silently discard theirs.
	tmuxSocketName = "agyn"

	// The socket directory. Written by the init container, because the main
	// container may run as a uid that cannot create it; agynd creates it only
	// as a fallback. The image's own /tmp is not used — it may be read-only,
	// and the platform should not depend on it either way.
	tmuxTmpDir = "/agyn/run"

	// The curated terminfo tree, for the programs running inside a pane. tmux
	// carries the entries it needs compiled in; vim and htop do their own
	// lookup and may find no database at all in a slim image.
	//
	// The trailing empty element is read by ncurses as "and the compiled-in
	// defaults too", so an image with a good database of its own keeps it.
	// TERMINFO_DIRS rather than TERMINFO for the same reason: it supplements
	// the primary location rather than replacing it.
	tmuxTerminfoDirs = "/agyn/terminfo:"

	// start-server daemonizes and returns; this only bounds a binary that
	// hangs rather than starting.
	tmuxStartTimeout = 15 * time.Second
)

var (
	tmuxCommandContext = exec.CommandContext

	// Swapped in tests. The values above are what runs in a container.
	tmuxBinaryPath   = tmuxBinary
	tmuxConfigPath   = tmuxConfig
	tmuxSocketDir    = tmuxTmpDir
	tmuxTerminfoPath = "/agyn/terminfo"
)

// startShellServer brings up the tmux server holding this container's
// persistent shells.
//
// Failure is logged and not fatal. A workload whose server did not start still
// serves ephemeral sessions, which is what every consumer that does not ask
// for a shell already uses — so a broken multiplexer costs persistence, not
// the terminal.
func startShellServer(ctx context.Context) {
	if _, err := os.Stat(tmuxBinaryPath); err != nil {
		log.Printf("shell server not started: %s unavailable: %v", tmuxBinaryPath, err)
		return
	}
	if err := os.MkdirAll(tmuxSocketDir, 0o777); err != nil {
		log.Printf("shell server not started: socket dir %s: %v", tmuxSocketDir, err)
		return
	}

	startCtx, cancel := context.WithTimeout(ctx, tmuxStartTimeout)
	defer cancel()

	cmd := tmuxCommandContext(startCtx, tmuxBinaryPath, "-L", tmuxSocketName, "-f", tmuxConfigPath, "start-server")
	cmd.Env = shellServerEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("shell server not started: %v: %s", err, strings.TrimSpace(string(out)))
		return
	}
	log.Printf("shell server started on socket %q", tmuxSocketName)
}

// shellServerEnv is what every shell in this container inherits, because the
// server is agynd's child and its children are the shells.
//
// This is the difference from an ephemeral session, which comes from
// Runner.Exec and inherits the container spec. Variables on the spec reach
// both — agynd is started from the same spec — so nothing the Orchestrator
// injects is lost either way.
func shellServerEnv() []string {
	env := os.Environ()
	env = append(env, "TMUX_TMPDIR="+tmuxSocketDir)
	if _, err := os.Stat(tmuxTerminfoPath); err == nil {
		env = append(env, "TERMINFO_DIRS="+tmuxTerminfoPath+":")
	}
	return env
}
