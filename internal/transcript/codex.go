package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"time"
)

// Codex appends one JSON object per line to its rollout file, each tagged with
// what it records. A user message opens a turn; the response items that follow
// are the model's output and the tools it called; a function_call_output
// carries what a tool returned.
type codexLine struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	ID string `json:"id"`
}

type codexTurnContext struct {
	Model string `json:"model"`
}

// A response item is the model's own output: a message it wrote, a reasoning
// summary, a tool it called, or the result of one.
type codexResponseItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   []codexContent  `json:"content"`
	Summary   []codexContent  `json:"summary"`
	Name      string          `json:"name"`
	CallID    string          `json:"call_id"`
	Arguments json.RawMessage `json:"arguments"`
	Output    json.RawMessage `json:"output"`
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Success   *bool           `json:"success"`
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Usage arrives on an event rather than a response item, reported for the turn
// so far rather than for one call.
type codexEventMsg struct {
	Type string          `json:"type"`
	Info *codexTokenInfo `json:"info"`
}

type codexTokenInfo struct {
	LastTokenUsage  *codexUsage `json:"last_token_usage"`
	TotalTokenUsage *codexUsage `json:"total_token_usage"`
}

type codexUsage struct {
	InputTokens         *int64 `json:"input_tokens"`
	CachedInputTokens   *int64 `json:"cached_input_tokens"`
	OutputTokens        *int64 `json:"output_tokens"`
	ReasoningOutputToks *int64 `json:"reasoning_output_tokens"`
}

func parseCodex(data []byte) ([]Turn, error) {
	var turns []Turn
	var current *Turn
	sessionID := ""
	model := ""
	pending := map[string]*ToolCall{}
	seen := &history{}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxTranscriptLine)
	for scanner.Scan() {
		var line codexLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		switch line.Type {
		case "session_meta":
			var meta codexSessionMeta
			if json.Unmarshal(line.Payload, &meta) == nil {
				sessionID = meta.ID
			}
		case "turn_context":
			var context codexTurnContext
			if json.Unmarshal(line.Payload, &context) == nil && context.Model != "" {
				model = context.Model
			}
		case "response_item":
			current = applyCodexItem(&turns, current, line, sessionID, model, pending, seen)
		case "event_msg":
			applyCodexUsage(current, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if current != nil {
		turns = append(turns, *current)
	}
	return turns, nil
}

func applyCodexItem(turns *[]Turn, current *Turn, line codexLine, sessionID, model string, pending map[string]*ToolCall, seen *history) *Turn {
	var item codexResponseItem
	if err := json.Unmarshal(line.Payload, &item); err != nil {
		return current
	}

	// A user message opens a turn. Everything else continues the one in flight,
	// and is dropped when there is none -- a transcript read mid-write can begin
	// anywhere.
	if item.Type == "message" && item.Role == "user" {
		if current != nil {
			*turns = append(*turns, *current)
		}
		seen.add(ContextItem{Role: "user", Text: codexText(item.Content), At: line.Timestamp})
		return &Turn{
			// The rollout has no turn id, so the moment it opened serves: it is
			// stable across re-reads of the same file, which is what the span
			// ids derived from it need.
			ID:        sessionID + "@" + line.Timestamp.UTC().Format(time.RFC3339Nano),
			SessionID: sessionID,
			StartedAt: line.Timestamp,
			Model:     model,
			UserInput: codexText(item.Content),
		}
	}
	if current == nil {
		return nil
	}
	step := codexStep(current, line, model, seen)

	switch item.Type {
	case "message":
		text := codexText(item.Content)
		step.Text = joinText(step.Text, text)
		current.FinalOutput = joinText(current.FinalOutput, text)
		seen.add(ContextItem{Role: firstNonEmptyText(item.Role, "assistant"), Text: text, At: line.Timestamp})
	case "reasoning":
		reasoning := codexText(item.Summary)
		step.Reasoning = joinText(step.Reasoning, reasoning)
		seen.add(ContextItem{Role: "assistant", Text: reasoning, At: line.Timestamp})
	case "function_call", "local_shell_call", "custom_tool_call", "mcp_tool_call":
		name := item.Name
		if name == "" {
			name = item.Tool
		}
		step.ToolCalls = append(step.ToolCalls, ToolCall{
			CallID:    item.CallID,
			Name:      name,
			Server:    item.Server,
			StartedAt: line.Timestamp,
			Arguments: nestedValue(item.Arguments),
		})
		pending[item.CallID] = &step.ToolCalls[len(step.ToolCalls)-1]
		seen.add(ContextItem{Role: "assistant", Text: name, JSON: nestedValue(item.Arguments), At: line.Timestamp})
	case "function_call_output", "custom_tool_call_output", "mcp_tool_call_output":
		call, ok := pending[item.CallID]
		if !ok {
			break
		}
		call.EndedAt = line.Timestamp
		call.Output = nestedValue(item.Output)
		seen.add(ContextItem{Role: "tool", JSON: call.Output, At: line.Timestamp})
		if item.Success != nil && !*item.Success {
			call.Error = textOf(call.Output)
		}
		delete(pending, item.CallID)
	}
	step.EndedAt = line.Timestamp
	current.EndedAt = line.Timestamp
	return current
}

// Codex does not delimit its model calls, so a turn's output is read as one
// step. What it called and what it said belong to the same request either way.
func codexStep(turn *Turn, line codexLine, model string, seen *history) *Step {
	if len(turn.Steps) == 0 {
		turn.Steps = append(turn.Steps, Step{
			ID:        turn.ID + "@step",
			StartedAt: line.Timestamp,
			EndedAt:   line.Timestamp,
			Model:     model,
			// Taken when the step opens: what the model was shown is the
			// conversation as it stood before it answered, not after.
			Context: seen.snapshot(),
		})
	}
	return &turn.Steps[len(turn.Steps)-1]
}

// The counts are reported for the turn so far, so the last report wins rather
// than accumulating across events.
func applyCodexUsage(turn *Turn, line codexLine) {
	if turn == nil {
		return
	}
	var event codexEventMsg
	if err := json.Unmarshal(line.Payload, &event); err != nil || event.Info == nil {
		return
	}
	usage := codexUsageOf(event.Info.TotalTokenUsage)
	if usage == nil {
		usage = codexUsageOf(event.Info.LastTokenUsage)
	}
	if usage == nil {
		return
	}
	turn.Usage = usage
	if len(turn.Steps) > 0 {
		turn.Steps[len(turn.Steps)-1].Usage = usage
	}
}

func codexUsageOf(usage *codexUsage) *Usage {
	if usage == nil {
		return nil
	}
	return &Usage{
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		CachedTokens:    usage.CachedInputTokens,
		ReasoningTokens: usage.ReasoningOutputToks,
	}
}

func codexText(content []codexContent) string {
	joined := ""
	for _, part := range content {
		joined = joinText(joined, part.Text)
	}
	return joined
}
