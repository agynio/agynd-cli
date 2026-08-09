package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"time"
)

// Claude Code appends one JSON object per line. A user line carrying text opens
// a turn; the assistant lines that follow are the steps it took; a user line
// carrying tool_result blocks is not a new turn but the output of a tool an
// earlier step called.
type claudeLine struct {
	Type      string        `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	SessionID string        `json:"sessionId"`
	UUID      string        `json:"uuid"`
	RequestID string        `json:"requestId"`
	Message   claudeMessage `json:"message"`
}

type claudeMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *claudeUsage    `json:"usage"`
}

type claudeUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}

// Content is a bare string on a typed user prompt and a block list everywhere
// else, so it is decoded twice rather than into an interface.
type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func parseClaude(data []byte) ([]Turn, error) {
	var turns []Turn
	var current *Turn
	// A tool's output arrives on a later line than the call, so calls are held
	// by id until it does.
	pending := map[string]*ToolCall{}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxTranscriptLine)
	for scanner.Scan() {
		var line claudeLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			// A transcript is written by something else and read while it is
			// still being written, so a line that does not parse is skipped
			// rather than failing the turns around it.
			continue
		}
		switch line.Type {
		case "user":
			if text, ok := claudeText(line.Message.Content); ok {
				current = closeAndOpen(&turns, current, line, text)
				continue
			}
			applyToolResults(line, pending)
		case "assistant":
			if current == nil {
				continue
			}
			appendAssistant(current, line, pending)
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

// closeAndOpen ends the turn in flight and starts the one this prompt opens.
func closeAndOpen(turns *[]Turn, current *Turn, line claudeLine, text string) *Turn {
	if current != nil {
		*turns = append(*turns, *current)
	}
	return &Turn{
		ID:        line.UUID,
		SessionID: line.SessionID,
		StartedAt: line.Timestamp,
		UserInput: text,
	}
}

// Claude streams one response as several lines sharing a message id. They are
// one model step, so the text is joined and the usage taken at its highest
// rather than counted once per line.
func appendAssistant(turn *Turn, line claudeLine, pending map[string]*ToolCall) {
	stepID := line.Message.ID
	if stepID == "" {
		stepID = line.RequestID
	}
	step := stepByID(turn, stepID, line)

	var blocks []claudeBlock
	if err := json.Unmarshal(line.Message.Content, &blocks); err != nil {
		return
	}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			step.Text = joinText(step.Text, block.Text)
			turn.FinalOutput = joinText(turn.FinalOutput, block.Text)
		case "thinking":
			step.Reasoning = joinText(step.Reasoning, block.Text)
		case "tool_use":
			call := ToolCall{
				CallID:    block.ID,
				Name:      block.Name,
				StartedAt: line.Timestamp,
				Arguments: rawValue(block.Input),
			}
			step.ToolCalls = append(step.ToolCalls, call)
			pending[block.ID] = &step.ToolCalls[len(step.ToolCalls)-1]
		}
	}
	step.EndedAt = line.Timestamp
	step.Usage = maxUsage(step.Usage, claudeUsageOf(line.Message.Usage))
	turn.EndedAt = line.Timestamp
	turn.Usage = maxUsage(turn.Usage, step.Usage)
	if turn.Model == "" {
		turn.Model = line.Message.Model
	}
}

func stepByID(turn *Turn, id string, line claudeLine) *Step {
	for i := range turn.Steps {
		if turn.Steps[i].ID == id {
			return &turn.Steps[i]
		}
	}
	turn.Steps = append(turn.Steps, Step{
		ID:        id,
		StartedAt: line.Timestamp,
		EndedAt:   line.Timestamp,
		Model:     line.Message.Model,
	})
	return &turn.Steps[len(turn.Steps)-1]
}

func applyToolResults(line claudeLine, pending map[string]*ToolCall) {
	var blocks []claudeBlock
	if err := json.Unmarshal(line.Message.Content, &blocks); err != nil {
		return
	}
	for _, block := range blocks {
		if block.Type != "tool_result" {
			continue
		}
		call, ok := pending[block.ToolUseID]
		if !ok {
			continue
		}
		call.EndedAt = line.Timestamp
		call.Output = rawValue(block.Content)
		if block.IsError {
			call.Error = textOf(rawValue(block.Content))
		}
		delete(pending, block.ToolUseID)
	}
}

// A user line opens a turn only when its content is a prompt. The same field
// carries tool results, which continue the turn already in flight.
func claudeText(content json.RawMessage) (string, bool) {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text, true
	}
	var blocks []claudeBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return "", false
	}
	joined := ""
	for _, block := range blocks {
		if block.Type == "tool_result" {
			return "", false
		}
		if block.Type == "text" {
			joined = joinText(joined, block.Text)
		}
	}
	return joined, joined != ""
}

func claudeUsageOf(usage *claudeUsage) *Usage {
	if usage == nil {
		return nil
	}
	return &Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CachedTokens: usage.CacheReadInputTokens,
	}
}
