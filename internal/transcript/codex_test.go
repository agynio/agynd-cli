package transcript

import (
	"strings"
	"testing"
)

// Codex rollout lines: the session, the turn's model, a user message, a tool
// call and its output, the model's reply, and the usage event that follows.
const codexTranscript = `{"timestamp":"2026-01-01T00:00:00.000Z","type":"session_meta","payload":{"id":"session-codex"}}
{"timestamp":"2026-01-01T00:00:00.500Z","type":"turn_context","payload":{"model":"gpt-5.5"}}
{"timestamp":"2026-01-01T00:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Read the file."}]}}
{"timestamp":"2026-01-01T00:00:02.000Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"Look at the file."}]}}
{"timestamp":"2026-01-01T00:00:03.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"call-1","arguments":"{\"command\":\"cat README.md\"}"}}
{"timestamp":"2026-01-01T00:00:04.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"# Example","success":true}}
{"timestamp":"2026-01-01T00:00:05.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"It starts with a heading."}]}}
{"timestamp":"2026-01-01T00:00:06.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":8607,"output_tokens":11,"cached_input_tokens":0,"reasoning_output_tokens":4}}}}`

func TestCodexReadsPromptToolAndReply(t *testing.T) {
	turns := parseOrFail(t, FormatCodex, codexTranscript)
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %d", len(turns))
	}
	turn := turns[0]

	if turn.UserInput != "Read the file." {
		t.Fatalf("unexpected prompt: %q", turn.UserInput)
	}
	if turn.SessionID != "session-codex" {
		t.Fatalf("unexpected session: %q", turn.SessionID)
	}
	if turn.Model != "gpt-5.5" {
		t.Fatalf("expected the model from turn_context, got %q", turn.Model)
	}
	if turn.FinalOutput != "It starts with a heading." {
		t.Fatalf("unexpected reply: %q", turn.FinalOutput)
	}
	if len(turn.Steps) != 1 {
		t.Fatalf("expected one step, got %d", len(turn.Steps))
	}
	step := turn.Steps[0]
	if step.Reasoning != "Look at the file." {
		t.Fatalf("unexpected reasoning: %q", step.Reasoning)
	}
	if len(step.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(step.ToolCalls))
	}
	call := step.ToolCalls[0]
	if call.Name != "shell" {
		t.Fatalf("unexpected tool: %q", call.Name)
	}
	// Codex encodes arguments as a JSON string, so they are decoded rather than
	// carried through as text.
	if args, ok := call.Arguments.(map[string]any); !ok || args["command"] != "cat README.md" {
		t.Fatalf("expected decoded arguments, got %#v", call.Arguments)
	}
	if call.Output != "# Example" {
		t.Fatalf("expected the output attached to its call, got %#v", call.Output)
	}
}

// The counts arrive on an event after the work, reported for the turn so far.
func TestCodexTakesUsageFromTheEvent(t *testing.T) {
	turn := parseOrFail(t, FormatCodex, codexTranscript)[0]
	if turn.Usage == nil {
		t.Fatal("expected the turn to carry usage")
	}
	if turn.Usage.InputTokens == nil || *turn.Usage.InputTokens != 8607 {
		t.Fatalf("unexpected input tokens: %+v", turn.Usage.InputTokens)
	}
	if turn.Usage.OutputTokens == nil || *turn.Usage.OutputTokens != 11 {
		t.Fatalf("unexpected output tokens: %+v", turn.Usage.OutputTokens)
	}
	if turn.Usage.ReasoningTokens == nil || *turn.Usage.ReasoningTokens != 4 {
		t.Fatalf("unexpected reasoning tokens: %+v", turn.Usage.ReasoningTokens)
	}
	if len(turn.Steps) != 1 || turn.Steps[0].Usage == nil {
		t.Fatal("expected the step to carry the usage too")
	}
}

// The rollout has no turn id, so one is derived from the moment it opened --
// which must not change when the same file is read again.
func TestCodexTurnIDIsStableAcrossReads(t *testing.T) {
	first := parseOrFail(t, FormatCodex, codexTranscript)[0]
	second := parseOrFail(t, FormatCodex, codexTranscript)[0]
	if first.ID == "" {
		t.Fatal("expected a turn id")
	}
	if first.ID != second.ID {
		t.Fatalf("expected a stable turn id, got %q then %q", first.ID, second.ID)
	}
}

func TestCodexSplitsTurnsOnEachUserMessage(t *testing.T) {
	second := `{"timestamp":"2026-01-01T00:01:00.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"And again."}]}}
{"timestamp":"2026-01-01T00:01:01.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done."}]}}`
	turns := parseOrFail(t, FormatCodex, codexTranscript+"\n"+second)
	if len(turns) != 2 {
		t.Fatalf("expected a turn per user message, got %d", len(turns))
	}
	if turns[1].UserInput != "And again." {
		t.Fatalf("unexpected second prompt: %q", turns[1].UserInput)
	}
	if turns[0].ID == turns[1].ID {
		t.Fatal("expected the turns to be identified separately")
	}
}

// A rollout read mid-write can begin anywhere, so output with no turn open is
// dropped rather than inventing one.
func TestCodexIgnoresOutputBeforeAnyPrompt(t *testing.T) {
	orphan := `{"timestamp":"2026-01-01T00:00:00.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"stray"}]}}`
	turns := parseOrFail(t, FormatCodex, orphan)
	if len(turns) != 0 {
		t.Fatalf("expected no turns, got %d", len(turns))
	}
}

func TestCodexMarksAFailedToolCall(t *testing.T) {
	failing := `{"timestamp":"2026-01-01T00:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Run it."}]}}
{"timestamp":"2026-01-01T00:00:02.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"call-9","arguments":"{}"}}
{"timestamp":"2026-01-01T00:00:03.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-9","output":"exit 1","success":false}}`
	turn := parseOrFail(t, FormatCodex, failing)[0]
	call := turn.Steps[0].ToolCalls[0]
	if call.Error != "exit 1" {
		t.Fatalf("expected the failure recorded, got %q", call.Error)
	}
}

func TestParseRejectsAFormatItDoesNotKnow(t *testing.T) {
	if _, err := Parse(Format("agn"), []byte("{}")); err == nil {
		t.Fatal("expected an unknown format to be rejected")
	}
}

// A call is worth storing because of what the model was shown. The context is
// the conversation as it stood when the call was made, with what arrived since
// the previous call marked as new.
func TestCodexCarriesTheConversationAsContext(t *testing.T) {
	turns, err := Parse(FormatCodex, []byte(strings.Join([]string{
		`{"timestamp":"2026-08-09T05:00:00Z","type":"session_meta","payload":{"id":"session-1"}}`,
		`{"timestamp":"2026-08-09T05:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"list the files"}]}}`,
		`{"timestamp":"2026-08-09T05:00:02Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"call-1","arguments":"{\"cmd\":\"ls\"}"}}`,
		`{"timestamp":"2026-08-09T05:00:03Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"a\nb"}}`,
		`{"timestamp":"2026-08-09T05:00:10Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"and again"}]}}`,
		`{"timestamp":"2026-08-09T05:00:11Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`,
	}, "\n")))
	if err != nil {
		t.Fatalf("expected the rollout to parse, got %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected two turns, got %d", len(turns))
	}

	first := turns[0].Steps[0].Context
	if len(first) != 1 || first[0].Role != "user" || first[0].Text != "list the files" {
		t.Fatalf("expected the prompt as the first call's context, got %#v", first)
	}
	if !first[0].IsNew {
		t.Fatal("expected the prompt to be new to the first call")
	}

	// The second call sees everything the first one did, plus what happened in
	// between. Only the prompt was on screen when the first call was made; the
	// tool call, its output and the new prompt all arrived after it, so they are
	// what changed.
	second := turns[1].Steps[0].Context
	if len(second) != 4 {
		t.Fatalf("expected the conversation so far, got %d items", len(second))
	}
	if second[0].IsNew {
		t.Fatal("expected the prompt the first call already saw to be carried over")
	}
	for _, item := range second[1:] {
		if !item.IsNew {
			t.Fatalf("expected what arrived after the first call to be new, got %#v", item)
		}
	}
	if second[3].Text != "and again" {
		t.Fatalf("expected the new prompt last, got %#v", second[3])
	}
	if second[1].Role != "assistant" || second[1].Text != "shell" {
		t.Fatalf("expected the tool call in the context, got %#v", second[1])
	}
	if second[2].Role != "tool" {
		t.Fatalf("expected the tool output in the context, got %#v", second[2])
	}
}
