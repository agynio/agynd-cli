// Command agynd-trace-hook exports what an agent CLI recorded of a turn.
//
// The agent CLI runs it on turn completion and hands it the session transcript.
// It reads the turns it has not sent, exports them to the Tracing service, and
// records what it sent so a resumed session -- which replays the transcript
// from its start -- does not send them twice.
//
// It fails open in every case. Tracing is an optional dependency, and a hook
// that ends a turn because it could not record it has done more harm than the
// missing record.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agynio/agynd-cli/internal/tracing"
	"github.com/agynio/agynd-cli/internal/transcript"
)

const exportTimeout = 20 * time.Second

// A trace id is 16 bytes, so an id of any other length is a misconfiguration
// rather than a trace nobody has written to yet.
const traceIDLength = 16

func main() {
	log.SetFlags(0)
	log.SetPrefix("agynd-trace-hook: ")
	if err := run(); err != nil {
		// Reported, never returned: the exit code is the agent CLI's signal to
		// continue, and a turn must not be lost to a tracing failure.
		log.Printf("%v", err)
	}
}

func run() error {
	format := transcript.Format(os.Getenv("AGYN_TRACE_FORMAT"))
	if format == "" {
		return fmt.Errorf("AGYN_TRACE_FORMAT is not set")
	}
	address := os.Getenv("TRACING_ADDRESS")
	if address == "" {
		// Tracing is optional, so an unconfigured endpoint is silence rather
		// than an error.
		return nil
	}

	payload, err := readHookPayload()
	if err != nil {
		return err
	}
	path := payload.transcriptPath()
	if path == "" {
		return fmt.Errorf("hook payload names no transcript")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read transcript: %w", err)
	}
	turns, err := transcript.Parse(format, data)
	if err != nil {
		return fmt.Errorf("parse transcript: %w", err)
	}

	sent, err := loadSent(path)
	if err != nil {
		// A sidecar that cannot be read is treated as empty. Re-exporting a
		// turn lands on the spans it already wrote, so the cost is a repeated
		// write rather than a duplicate.
		log.Printf("read sidecar: %v", err)
	}
	fresh := unsent(turns, sent)
	if len(fresh) == 0 {
		return nil
	}

	// agynd opened the trace and named it in the environment it set up. A hook
	// that was given no trace has nothing to attach to, so it exports nothing
	// rather than opening a second one.
	traceID, err := hex.DecodeString(os.Getenv("AGYN_TRACE_ID"))
	if err != nil || len(traceID) != traceIDLength {
		return fmt.Errorf("AGYN_TRACE_ID is not a trace id")
	}
	exporter, err := tracing.NewExporter(tracing.Config{
		Address:    address,
		TraceID:    traceID,
		WorkloadID: os.Getenv("WORKLOAD_ID"),
	})
	if err != nil {
		return err
	}
	defer func() { _ = exporter.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
	defer cancel()
	if err := exporter.Turns(ctx, fresh); err != nil {
		// Left unrecorded so the next turn sends it again, rather than marked
		// delivered on a failed export.
		return fmt.Errorf("export turns: %w", err)
	}
	return recordSent(path, sent, fresh)
}

// hookPayload is what the agent CLI writes to the hook's stdin. The CLIs differ
// in spelling and in which fields they set, so each is read where it is found.
type hookPayload struct {
	TranscriptPath  string `json:"transcript_path"`
	TranscriptPath2 string `json:"transcriptPath"`
	RolloutPath     string `json:"rollout_path"`
	RolloutPath2    string `json:"rolloutPath"`
	SessionID       string `json:"session_id"`
	SessionID2      string `json:"sessionId"`
	// Codex hands the path alone on stdin rather than a document.
	bare string
}

func (p hookPayload) transcriptPath() string {
	for _, candidate := range []string{p.TranscriptPath, p.TranscriptPath2, p.RolloutPath, p.RolloutPath2, p.bare} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func readHookPayload() (hookPayload, error) {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return hookPayload{}, fmt.Errorf("read hook payload: %w", err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return hookPayload{}, fmt.Errorf("hook payload is empty")
	}
	var payload hookPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		// Not a document: the whole of stdin is the path.
		return hookPayload{bare: trimmed}, nil
	}
	return payload, nil
}

// The sidecar sits beside the transcript so it is discarded with the session it
// describes, rather than accumulating somewhere shared.
func sidecarPath(transcriptPath string) string {
	return filepath.Join(filepath.Dir(transcriptPath), "."+filepath.Base(transcriptPath)+".agyn-traced")
}

func loadSent(transcriptPath string) (map[string]bool, error) {
	sent := map[string]bool{}
	data, err := os.ReadFile(sidecarPath(transcriptPath))
	if err != nil {
		if os.IsNotExist(err) {
			return sent, nil
		}
		return sent, err
	}
	for _, id := range strings.Split(string(data), "\n") {
		if id = strings.TrimSpace(id); id != "" {
			sent[id] = true
		}
	}
	return sent, nil
}

// A turn is recorded as sent only once its export succeeded, and one still
// running is never recorded -- it is exported again when it completes, landing
// on the spans it already wrote.
func recordSent(transcriptPath string, sent map[string]bool, exported []transcript.Turn) error {
	var lines []string
	for id := range sent {
		lines = append(lines, id)
	}
	for _, turn := range exported {
		if turn.EndedAt.IsZero() || sent[turn.ID] {
			continue
		}
		lines = append(lines, turn.ID)
	}
	body := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(sidecarPath(transcriptPath), []byte(body), 0o600)
}

func unsent(turns []transcript.Turn, sent map[string]bool) []transcript.Turn {
	fresh := make([]transcript.Turn, 0, len(turns))
	for _, turn := range turns {
		if sent[turn.ID] {
			continue
		}
		fresh = append(fresh, turn)
	}
	return fresh
}
