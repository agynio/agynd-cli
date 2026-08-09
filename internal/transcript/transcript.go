// Package transcript reads what an agent CLI recorded of a session.
//
// An agent CLI's telemetry reports that a model call happened and never what
// was said. The transcript it keeps in order to resume a session holds the
// prompt, the reply, every tool call with its input and output, and the usage
// counts, so that is what is read.
//
// Each CLI writes its own format. They are parsed into one shape, because what
// a turn is does not differ between them.
package transcript

import (
	"encoding/json"
	"time"
)

// Turn is one exchange: what the agent was asked, the model steps it took to
// answer, and what it said in the end.
type Turn struct {
	// Identifies the turn within its session. Spans derive their ids from it,
	// so a resumed session that replays the transcript writes what it wrote
	// before rather than a second copy.
	ID          string
	SessionID   string
	StartedAt   time.Time
	EndedAt     time.Time
	Model       string
	UserInput   string
	FinalOutput string
	Steps       []Step
	Usage       *Usage
	// Set on a subagent's turns, naming the turn that delegated the work.
	ParentTurnID string
}

// Step is one model call and the tools it decided to run.
type Step struct {
	ID        string
	StartedAt time.Time
	EndedAt   time.Time
	Model     string
	Reasoning string
	Text      string
	// What the model was shown. The reason a turn is worth storing: without it
	// a call records only that it happened.
	Context   any
	ToolCalls []ToolCall
	Usage     *Usage
}

type ToolCall struct {
	CallID    string
	Name      string
	Server    string
	StartedAt time.Time
	EndedAt   time.Time
	Arguments any
	Output    any
	Error     string
}

type Usage struct {
	InputTokens     *int64
	OutputTokens    *int64
	CachedTokens    *int64
	ReasoningTokens *int64
}

// Format names how a transcript is written. The hook is told which agent CLI
// invoked it, so the file is never sniffed.
type Format string

const (
	FormatClaude Format = "claude"
	FormatCodex  Format = "codex"
)

// Parse reads every turn a transcript holds. Callers decide which of them have
// already been exported; a parser reports what is there.
func Parse(format Format, data []byte) ([]Turn, error) {
	switch format {
	case FormatClaude:
		return parseClaude(data)
	case FormatCodex:
		return parseCodex(data)
	}
	return nil, &UnknownFormatError{Format: format}
}

type UnknownFormatError struct {
	Format Format
}

func (e *UnknownFormatError) Error() string {
	return "unknown transcript format: " + string(e.Format)
}

func intPtr(v int64) *int64 { return &v }

// A step's usage is reported cumulatively by some CLIs, so the largest value
// seen for a turn is the turn's, not the sum.
func maxUsage(a, b *Usage) *Usage {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &Usage{
		InputTokens:     maxPtr(a.InputTokens, b.InputTokens),
		OutputTokens:    maxPtr(a.OutputTokens, b.OutputTokens),
		CachedTokens:    maxPtr(a.CachedTokens, b.CachedTokens),
		ReasoningTokens: maxPtr(a.ReasoningTokens, b.ReasoningTokens),
	}
}

func maxPtr(a, b *int64) *int64 {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case *b > *a:
		return b
	default:
		return a
	}
}

// A transcript line carries a whole prompt or a whole tool output, so the
// scanner is given room a default one does not have.
const maxTranscriptLine = 8 << 20

func joinText(existing, addition string) string {
	switch {
	case addition == "":
		return existing
	case existing == "":
		return addition
	default:
		return existing + addition
	}
}

// Arguments and outputs are whatever the agent CLI recorded. They are carried
// as decoded values so the exporter can render them as JSON, and left as a
// string when they are not JSON at all.
func rawValue(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}

// Codex encodes a tool's arguments as a JSON string rather than an object, so
// a decode leaves text where the structure is. Unwrapped only when the inner
// value is itself structured: a tool whose output is the literal "42" is a
// string, not a number.
func nestedValue(raw []byte) any {
	value := rawValue(raw)
	text, ok := value.(string)
	if !ok {
		return value
	}
	var inner any
	if err := json.Unmarshal([]byte(text), &inner); err != nil {
		return value
	}
	switch inner.(type) {
	case map[string]any, []any:
		return inner
	}
	return value
}

func textOf(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}
