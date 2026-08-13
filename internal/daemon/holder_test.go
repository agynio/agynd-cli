package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRunHolderRunsEnvironmentInitScripts(t *testing.T) {
	workDir := t.TempDir()
	stubHolderChdir(t)
	fake := &fakeInitScriptsClient{responses: []*agentsv1.ListInitScriptsResponse{{
		InitScripts: []*agentsv1.InitScript{{
			Meta:   &agentsv1.EntityMeta{Id: "install", CreatedAt: timestamppb.New(time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC))},
			Script: "printf 'ran\n' > installed.txt",
		}},
	}}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- runHolder(ctx, fake, "env-1", workDir) }()

	waitForFile(t, errCh, filepath.Join(workDir, "installed.txt"))

	// The holder holds: the scripts finishing is not the workload finishing.
	select {
	case err := <-errCh:
		t.Fatalf("holder returned before cancellation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("holder did not stop after cancellation")
	}

	if len(fake.requests) != 1 {
		t.Fatalf("expected 1 list request, got %d", len(fake.requests))
	}
	// Environment-scoped only: a sandbox has no agent to scope scripts to.
	if got := fake.requests[0].GetEnvironmentId(); got != "env-1" {
		t.Fatalf("expected environment id env-1, got %q", got)
	}
	if got := fake.requests[0].GetAgentId(); got != "" {
		t.Fatalf("expected no agent id, got %q", got)
	}
}

func TestRunHolderWithoutEnvironmentSkipsInitScripts(t *testing.T) {
	stubHolderChdir(t)
	fake := &fakeInitScriptsClient{}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- runHolder(ctx, fake, "", t.TempDir()) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("holder did not stop after cancellation")
	}

	if len(fake.requests) != 0 {
		t.Fatalf("expected no list requests, got %d", len(fake.requests))
	}
}

func TestRunHolderFailsWhenInitScriptsCannotBeFetched(t *testing.T) {
	stubHolderChdir(t)
	listErr := errors.New("gateway unavailable")
	fake := &fakeInitScriptsClient{err: listErr}

	err := runHolder(context.Background(), fake, "env-1", t.TempDir())
	if !errors.Is(err, listErr) {
		t.Fatalf("expected the list error, got %v", err)
	}
}

func stubHolderChdir(t *testing.T) {
	t.Helper()
	original := holderChdir
	holderChdir = func(string) error { return nil }
	t.Cleanup(func() { holderChdir = original })
}

func waitForFile(t *testing.T, errCh <-chan error, path string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case err := <-errCh:
			t.Fatalf("holder returned before running init scripts: %v", err)
		case <-deadline:
			t.Fatalf("init script did not produce %s", path)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
