package transcript

import "testing"

// Lines taken from a Claude Code transcript: a prompt, a streamed reply split
// across two lines sharing one message id, a tool call, and its result.
const claudeToolTranscript = `{"type":"user","timestamp":"2026-01-01T00:01:00.000Z","sessionId":"session-tool","uuid":"user-tool-1","cwd":"/repo","message":{"role":"user","content":"Read the file."}}
{"type":"assistant","timestamp":"2026-01-01T00:01:01.000Z","sessionId":"session-tool","uuid":"assistant-tool-1","requestId":"req-tool-1","message":{"id":"msg-tool-1","role":"assistant","model":"claude-test","content":[{"type":"tool_use","id":"toolu_read_1","name":"Read","input":{"file_path":"README.md"}}]}}
{"type":"user","timestamp":"2026-01-01T00:01:02.000Z","sessionId":"session-tool","uuid":"tool-result-read-1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_read_1","content":"# Example"}]}}
{"type":"assistant","timestamp":"2026-01-01T00:01:03.000Z","sessionId":"session-tool","uuid":"assistant-tool-2","requestId":"req-tool-2","message":{"id":"msg-tool-2","role":"assistant","model":"claude-test","content":[{"type":"text","text":"The file starts with a heading."}]}}`

const claudeSimpleTranscript = `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","sessionId":"session-simple","uuid":"user-simple-1","message":{"role":"user","content":"Say hello."}}
{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","sessionId":"session-simple","uuid":"assistant-simple-1a","requestId":"req-simple-1","message":{"id":"msg-simple-1","role":"assistant","model":"claude-test","content":[{"type":"text","text":"Hello "}],"usage":{"input_tokens":10,"output_tokens":2}}}
{"type":"assistant","timestamp":"2026-01-01T00:00:02.000Z","sessionId":"session-simple","uuid":"assistant-simple-1b","requestId":"req-simple-1","message":{"id":"msg-simple-1","role":"assistant","model":"claude-test","content":[{"type":"text","text":"from Claude Code."}],"usage":{"input_tokens":10,"output_tokens":5}}}`

func parseOrFail(t *testing.T, format Format, data string) []Turn {
	t.Helper()
	turns, err := Parse(format, []byte(data))
	if err != nil {
		t.Fatalf("parse %s: %v", format, err)
	}
	return turns
}

func TestClaudeReadsPromptReplyAndUsage(t *testing.T) {
	turns := parseOrFail(t, FormatClaude, claudeSimpleTranscript)
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %d", len(turns))
	}
	turn := turns[0]
	if turn.UserInput != "Say hello." {
		t.Fatalf("unexpected prompt: %q", turn.UserInput)
	}
	// One response streamed across two lines is one step, not two.
	if len(turn.Steps) != 1 {
		t.Fatalf("expected one step for one message id, got %d", len(turn.Steps))
	}
	if turn.FinalOutput != "Hello from Claude Code." {
		t.Fatalf("expected the streamed text joined, got %q", turn.FinalOutput)
	}
	if turn.Model != "claude-test" {
		t.Fatalf("unexpected model: %q", turn.Model)
	}
	// Usage is cumulative per line, so the turn takes the highest rather than
	// the sum of what each line reported.
	if turn.Usage == nil || turn.Usage.OutputTokens == nil || *turn.Usage.OutputTokens != 5 {
		t.Fatalf("expected cumulative usage to settle at 5, got %+v", turn.Usage)
	}
	if turn.Usage.InputTokens == nil || *turn.Usage.InputTokens != 10 {
		t.Fatal("expected the input tokens reported")
	}
}

func TestClaudeAttachesToolOutputToItsCall(t *testing.T) {
	turns := parseOrFail(t, FormatClaude, claudeToolTranscript)
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %d", len(turns))
	}
	turn := turns[0]

	// A user line carrying tool_result continues the turn; it does not open one.
	if turn.UserInput != "Read the file." {
		t.Fatalf("unexpected prompt: %q", turn.UserInput)
	}
	var calls []ToolCall
	for _, step := range turn.Steps {
		calls = append(calls, step.ToolCalls...)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	call := calls[0]
	if call.Name != "Read" {
		t.Fatalf("unexpected tool: %q", call.Name)
	}
	if args, ok := call.Arguments.(map[string]any); !ok || args["file_path"] != "README.md" {
		t.Fatalf("expected the tool input, got %#v", call.Arguments)
	}
	if call.Output != "# Example" {
		t.Fatalf("expected the tool output attached to its call, got %#v", call.Output)
	}
	if call.EndedAt.IsZero() {
		t.Fatal("expected the result to close the call")
	}
	if turn.FinalOutput != "The file starts with a heading." {
		t.Fatalf("unexpected final output: %q", turn.FinalOutput)
	}
}

func TestClaudeSplitsTurnsOnEachPrompt(t *testing.T) {
	turns := parseOrFail(t, FormatClaude, claudeSimpleTranscript+"\n"+claudeToolTranscript)
	if len(turns) != 2 {
		t.Fatalf("expected a turn per prompt, got %d", len(turns))
	}
	if turns[0].ID == turns[1].ID {
		t.Fatal("expected each turn to be identified separately")
	}
}

// A transcript is read while it is still being written, so a line that does not
// parse must not cost the turns around it.
func TestClaudeSkipsUnreadableLines(t *testing.T) {
	turns := parseOrFail(t, FormatClaude, claudeSimpleTranscript+"\n{not json")
	if len(turns) != 1 {
		t.Fatalf("expected the intact turn, got %d", len(turns))
	}
	if turns[0].FinalOutput != "Hello from Claude Code." {
		t.Fatal("expected the turn before the bad line to survive intact")
	}
}

func TestClaudeIgnoresLinesItDoesNotModel(t *testing.T) {
	noise := `{"type":"mode","sessionId":"session-simple","mode":"default"}
{"type":"file-history-snapshot","messageId":"snapshot","snapshot":{}}`
	turns := parseOrFail(t, FormatClaude, noise+"\n"+claudeSimpleTranscript)
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %d", len(turns))
	}
}
