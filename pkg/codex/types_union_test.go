package codex

import (
	"encoding/json"
	"testing"
)

func TestAskForApprovalJSON(t *testing.T) {
	var approval AskForApproval
	if err := json.Unmarshal([]byte(`"never"`), &approval); err != nil {
		t.Fatalf("unmarshal mode: %v", err)
	}
	if approval.Mode != AskForApprovalNever {
		t.Fatalf("expected mode never, got %q", approval.Mode)
	}
	if approval.Granular != nil {
		t.Fatalf("expected nil granular")
	}
	data, err := json.Marshal(approval)
	if err != nil {
		t.Fatalf("marshal mode: %v", err)
	}
	if string(data) != `"never"` {
		t.Fatalf("unexpected marshal output: %s", string(data))
	}

	var granular AskForApproval
	input := []byte(`{"granular":{"mcp_elicitations":true,"rules":true,"sandbox_approval":false}}`)
	if err := json.Unmarshal(input, &granular); err != nil {
		t.Fatalf("unmarshal granular: %v", err)
	}
	if granular.Granular == nil || !granular.Granular.McpElicitations || !granular.Granular.Rules || granular.Granular.SandboxApproval {
		t.Fatalf("unexpected granular payload: %#v", granular.Granular)
	}
}

func TestUserInputUnion(t *testing.T) {
	var input UserInput
	if err := json.Unmarshal([]byte(`{"type":"text","text":"hello"}`), &input); err != nil {
		t.Fatalf("unmarshal user input: %v", err)
	}
	if input.Text == nil || input.Text.Text != "hello" {
		t.Fatalf("unexpected user input: %#v", input.Text)
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal user input: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["type"] != string(UserInputTypeText) {
		t.Fatalf("expected type text, got %v", payload["type"])
	}
}

func TestThreadItemUnion(t *testing.T) {
	var item ThreadItem
	input := []byte(`{"id":"item-1","type":"agentMessage","text":"final","phase":"final_answer"}`)
	if err := json.Unmarshal(input, &item); err != nil {
		t.Fatalf("unmarshal thread item: %v", err)
	}
	if item.AgentMessage == nil {
		t.Fatalf("expected agent message")
	}
	if item.AgentMessage.Phase == nil || *item.AgentMessage.Phase != MessagePhaseFinalAnswer {
		t.Fatalf("unexpected phase: %#v", item.AgentMessage.Phase)
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal thread item: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["type"] != string(ThreadItemTypeAgentMessage) {
		t.Fatalf("expected agentMessage type, got %v", payload["type"])
	}
}
