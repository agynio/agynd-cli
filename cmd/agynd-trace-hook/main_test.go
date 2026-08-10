package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agynio/agynd-cli/internal/transcript"
)

// Each CLI names the transcript its own way, and codex hands the path alone
// rather than a document. All of them have to arrive at the same file.
func TestHookPayloadReadsThePathWhereverItIs(t *testing.T) {
	for name, payload := range map[string]hookPayload{
		"claude":        {TranscriptPath: "/t/session.jsonl"},
		"camel case":    {TranscriptPath2: "/t/session.jsonl"},
		"rollout":       {RolloutPath: "/t/session.jsonl"},
		"rollout camel": {RolloutPath2: "/t/session.jsonl"},
		"bare path":     {bare: "/t/session.jsonl"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := payload.transcriptPath(); got != "/t/session.jsonl" {
				t.Fatalf("expected the transcript path, got %q", got)
			}
		})
	}
}

func TestHookPayloadNamesNothingWhenEmpty(t *testing.T) {
	if got := (hookPayload{}).transcriptPath(); got != "" {
		t.Fatalf("expected no path, got %q", got)
	}
}

// The sidecar is discarded with the session it describes, so it sits beside the
// transcript rather than somewhere shared.
func TestSidecarSitsBesideItsTranscript(t *testing.T) {
	got := sidecarPath("/home/agent/.codex/sessions/rollout-1.jsonl")
	want := "/home/agent/.codex/sessions/.rollout-1.jsonl.agyn-traced"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSentIsEmptyBeforeAnythingIsRecorded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	sent, err := loadSent(path)
	if err != nil {
		t.Fatalf("expected a missing sidecar to read as empty, got %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("expected nothing sent, got %d", len(sent))
	}
}

func finishedTurn(id string) transcript.Turn {
	return transcript.Turn{
		ID:        id,
		StartedAt: time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2026, 8, 9, 5, 0, 3, 0, time.UTC),
	}
}

// A resumed session replays the transcript from its start, so what was already
// exported has to survive the process that exported it.
func TestRecordedTurnsAreReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	if err := recordSent(path, map[string]bool{}, []transcript.Turn{finishedTurn("turn-1"), finishedTurn("turn-2")}); err != nil {
		t.Fatalf("expected the turns to record, got %v", err)
	}

	sent, err := loadSent(path)
	if err != nil {
		t.Fatalf("expected the sidecar to read, got %v", err)
	}
	if !sent["turn-1"] || !sent["turn-2"] {
		t.Fatalf("expected both turns recorded, got %v", sent)
	}
}

// A second invocation must not drop what the first recorded, or every turn is
// exported again on the one after it.
func TestRecordingKeepsWhatWasAlreadySent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	if err := recordSent(path, map[string]bool{"turn-1": true}, []transcript.Turn{finishedTurn("turn-2")}); err != nil {
		t.Fatalf("expected the turn to record, got %v", err)
	}

	sent, _ := loadSent(path)
	if !sent["turn-1"] {
		t.Fatal("expected the earlier turn to survive")
	}
	if !sent["turn-2"] {
		t.Fatal("expected the new turn to be recorded")
	}
}

// A hook fires while the CLI is still writing, so the last turn in the file may
// not have finished. Recording it as sent would lose everything it went on to
// do.
func TestAnUnfinishedTurnIsNotRecordedAsSent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	running := finishedTurn("turn-2")
	running.EndedAt = time.Time{}

	if err := recordSent(path, map[string]bool{}, []transcript.Turn{finishedTurn("turn-1"), running}); err != nil {
		t.Fatalf("expected the finished turn to record, got %v", err)
	}

	sent, _ := loadSent(path)
	if !sent["turn-1"] {
		t.Fatal("expected the finished turn to be recorded")
	}
	if sent["turn-2"] {
		t.Fatal("expected the turn still running to be left for the next export")
	}
}

func TestRecordingTwiceDoesNotDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	if err := recordSent(path, map[string]bool{}, []transcript.Turn{finishedTurn("turn-1")}); err != nil {
		t.Fatalf("expected the turn to record, got %v", err)
	}
	sent, _ := loadSent(path)
	if err := recordSent(path, sent, []transcript.Turn{finishedTurn("turn-1")}); err != nil {
		t.Fatalf("expected the turn to record again, got %v", err)
	}

	data, err := os.ReadFile(sidecarPath(path))
	if err != nil {
		t.Fatalf("expected the sidecar to read, got %v", err)
	}
	if want := "turn-1\n"; string(data) != want {
		t.Fatalf("expected %q, got %q", want, string(data))
	}
}

func TestUnsentSkipsWhatWasAlreadyExported(t *testing.T) {
	turns := []transcript.Turn{finishedTurn("turn-1"), finishedTurn("turn-2"), finishedTurn("turn-3")}

	fresh := unsent(turns, map[string]bool{"turn-1": true, "turn-3": true})

	if len(fresh) != 1 || fresh[0].ID != "turn-2" {
		t.Fatalf("expected only the unsent turn, got %v", fresh)
	}
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

const claudePrompt = `{"type":"user","uuid":"turn-1","sessionId":"s","timestamp":"2026-08-09T05:00:00Z","message":{"role":"user","content":"hello"}}`
const claudeReply = `{"type":"assistant","uuid":"a","sessionId":"s","timestamp":"2026-08-09T05:00:03Z","message":{"id":"m1","role":"assistant","model":"claude","content":[{"type":"text","text":"hi"}]}}`

// Claude Code runs the hook with the assistant's reply still unflushed, so a
// transcript read the instant the hook starts holds a turn with nothing in it.
// The hook is not run again, so exporting that is exporting nothing.
func TestTurnsAreReadOnceTheTranscriptCatchesUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeLines(t, path, claudePrompt)

	go func() {
		time.Sleep(200 * time.Millisecond)
		writeLines(t, path, claudePrompt, claudeReply)
	}()

	turns, err := settledTurns(transcript.FormatClaude, path, map[string]bool{})
	if err != nil {
		t.Fatalf("expected the turns to read, got %v", err)
	}
	if !finished(turns) {
		t.Fatal("expected the hook to wait for the turn the CLI was still writing")
	}
}

// A transcript that never completes must not hold the turn open: the hook gives
// up and lets the CLI carry on.
func TestWaitingForTheTurnIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeLines(t, path, claudePrompt)

	start := time.Now()
	turns, err := settledTurns(transcript.FormatClaude, path, map[string]bool{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected the read to succeed, got %v", err)
	}
	if finished(turns) {
		t.Fatal("expected no finished turn")
	}
	if elapsed < settleTimeout {
		t.Fatalf("expected to wait for the turn, gave up after %s", elapsed)
	}
	if elapsed > settleTimeout*2 {
		t.Fatalf("expected the wait to be bounded, took %s", elapsed)
	}
}

// A turn already in the transcript is exported without waiting: codex flushes
// its rollout before running the hook.
func TestAFinishedTurnIsReadImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeLines(t, path, claudePrompt, claudeReply)

	start := time.Now()
	turns, err := settledTurns(transcript.FormatClaude, path, map[string]bool{})

	if err != nil {
		t.Fatalf("expected the turns to read, got %v", err)
	}
	if !finished(turns) {
		t.Fatal("expected the finished turn")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("expected no wait, took %s", elapsed)
	}
}
