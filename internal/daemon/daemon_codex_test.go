package daemon

import (
	"context"
	"testing"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"github.com/agynio/agynd-cli/internal/codexbridge"
	"github.com/agynio/agynd-cli/internal/config"
	codex "github.com/agynio/codex-sdk-go"
)

type fakeCodexClient struct {
	startThreadCalls  int
	resumeThreadCalls int
	startParams       *codex.ThreadStartParams
	resumeParams      *codex.ThreadResumeParams
}

func (f *fakeCodexClient) StartThread(_ context.Context, params *codex.ThreadStartParams) (*codex.ThreadStartResponse, error) {
	f.startThreadCalls++
	f.startParams = params
	return &codex.ThreadStartResponse{Thread: codex.Thread{ID: "codex-started"}}, nil
}

func (f *fakeCodexClient) ResumeThread(_ context.Context, params *codex.ThreadResumeParams) (*codex.ThreadResumeResponse, error) {
	f.resumeThreadCalls++
	f.resumeParams = params
	return &codex.ThreadResumeResponse{Thread: codex.Thread{ID: params.ThreadID}}, nil
}

func (f *fakeCodexClient) StartTurn(_ context.Context, _ *codex.TurnStartParams) (*codex.TurnStartResponse, error) {
	return &codex.TurnStartResponse{Turn: codex.Turn{ID: "turn-1"}}, nil
}

func (f *fakeCodexClient) Close() error {
	return nil
}

func TestEnsureCodexThreadResumesFromStore(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	record := codexbridge.ThreadMappingRecord{
		PlatformThreadID: "platform-1",
		CodexThreadID:    "codex-1",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000000100,
	}
	if err := store.Save(record); err != nil {
		t.Fatalf("expected mapping save to succeed, got %v", err)
	}
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		agent:        &agentsv1.Agent{},
	}
	threadID, err := daemon.ensureCodexThread(context.Background(), record.PlatformThreadID)
	if err != nil {
		t.Fatalf("expected ensureCodexThread to succeed, got %v", err)
	}
	if threadID != record.CodexThreadID {
		t.Fatalf("expected thread id %q, got %q", record.CodexThreadID, threadID)
	}
	if client.resumeThreadCalls != 1 {
		t.Fatalf("expected ResumeThread to be called once, got %d", client.resumeThreadCalls)
	}
	if client.startThreadCalls != 0 {
		t.Fatalf("expected StartThread not to be called, got %d", client.startThreadCalls)
	}
	if client.resumeParams == nil || client.resumeParams.ThreadID != record.CodexThreadID {
		t.Fatalf("expected resume params for %q", record.CodexThreadID)
	}
}

func TestEnsureCodexThreadStartsNonEphemeral(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		agent:        &agentsv1.Agent{},
	}
	threadID, err := daemon.ensureCodexThread(context.Background(), "platform-new")
	if err != nil {
		t.Fatalf("expected ensureCodexThread to succeed, got %v", err)
	}
	if threadID != "codex-started" {
		t.Fatalf("expected thread id %q, got %q", "codex-started", threadID)
	}
	if client.startThreadCalls != 1 {
		t.Fatalf("expected StartThread to be called once, got %d", client.startThreadCalls)
	}
	if client.resumeThreadCalls != 0 {
		t.Fatalf("expected ResumeThread not to be called, got %d", client.resumeThreadCalls)
	}
	if client.startParams == nil || client.startParams.Ephemeral == nil {
		t.Fatal("expected Ephemeral to be set")
	}
	if *client.startParams.Ephemeral {
		t.Fatal("expected Ephemeral to be false")
	}
	stored, ok, err := store.Load("platform-new")
	if err != nil {
		t.Fatalf("expected mapping to be saved, got %v", err)
	}
	if !ok {
		t.Fatal("expected mapping to be saved")
	}
	if stored.CodexThreadID != "codex-started" {
		t.Fatalf("expected stored codex id %q, got %q", "codex-started", stored.CodexThreadID)
	}
}
