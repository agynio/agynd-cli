package daemon

import (
	"context"
	"testing"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type scopedMcpsClient struct {
	byTarget map[string][]*agentsv1.Mcp
	requests []string
}

func (c *scopedMcpsClient) ListMcps(_ context.Context, in *agentsv1.ListMcpsRequest, _ ...grpc.CallOption) (*agentsv1.ListMcpsResponse, error) {
	target := "agent:" + in.GetAgentId()
	if in.GetEnvironmentId() != "" {
		target = "environment:" + in.GetEnvironmentId()
	}
	c.requests = append(c.requests, target)
	return &agentsv1.ListMcpsResponse{Mcps: c.byTarget[target]}, nil
}

// Environment-level and agent-level servers compose as a union, and on a name
// collision the agent's wins -- the rule ENV already follows.
func TestListMCPsMergesEnvironmentAndAgentByName(t *testing.T) {
	client := &scopedMcpsClient{byTarget: map[string][]*agentsv1.Mcp{
		"environment:env-1": {
			{Meta: &agentsv1.EntityMeta{Id: "env-shared", CreatedAt: timestamppb.New(time.Now())}, Name: "files"},
			{Meta: &agentsv1.EntityMeta{Id: "env-only", CreatedAt: timestamppb.New(time.Now())}, Name: "search"},
		},
		"agent:agent-1": {
			{Meta: &agentsv1.EntityMeta{Id: "agent-shared", CreatedAt: timestamppb.New(time.Now())}, Name: "files"},
		},
	}}

	mcps, err := listMCPs(context.Background(), client, "agent-1", "env-1")
	if err != nil {
		t.Fatalf("list mcps: %v", err)
	}
	byName := map[string]string{}
	for _, mcp := range mcps {
		byName[mcp.Name] = mcp.ID
	}
	if len(byName) != 2 {
		t.Fatalf("expected 2 servers, got %d: %v", len(byName), byName)
	}
	if byName["files"] != "agent-shared" {
		t.Fatalf("expected the agent-level server to win on a name collision, got %q", byName["files"])
	}
	if byName["search"] != "env-only" {
		t.Fatalf("expected the environment-only server to survive, got %q", byName["search"])
	}
}

// A sandbox has no agent, so it runs the environment's servers alone.
func TestListMCPsWithoutAnAgentReadsTheEnvironmentOnly(t *testing.T) {
	client := &scopedMcpsClient{byTarget: map[string][]*agentsv1.Mcp{
		"environment:env-1": {{Meta: &agentsv1.EntityMeta{Id: "env-only", CreatedAt: timestamppb.New(time.Now())}, Name: "files"}},
	}}

	mcps, err := listMCPs(context.Background(), client, "", "env-1")
	if err != nil {
		t.Fatalf("list mcps: %v", err)
	}
	if len(mcps) != 1 || mcps[0].ID != "env-only" {
		t.Fatalf("unexpected servers: %+v", mcps)
	}
	for _, req := range client.requests {
		if req == "agent:" {
			t.Fatal("expected no agent-scoped request without an agent")
		}
	}
}

func TestListMCPsRequiresATarget(t *testing.T) {
	if _, err := listMCPs(context.Background(), &scopedMcpsClient{}, "", ""); err == nil {
		t.Fatal("expected an error with neither an agent nor an environment")
	}
}
