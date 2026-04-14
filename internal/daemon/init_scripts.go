package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"google.golang.org/grpc"
)

const initScriptsPageSize int32 = 100

type initScriptsClient interface {
	ListInitScripts(ctx context.Context, in *agentsv1.ListInitScriptsRequest, opts ...grpc.CallOption) (*agentsv1.ListInitScriptsResponse, error)
}

type initScript struct {
	ID        string
	CreatedAt time.Time
	Script    string
}

func runInitScripts(ctx context.Context, client initScriptsClient, agentID string, workDir string) error {
	scripts, err := listInitScripts(ctx, client, agentID)
	if err != nil {
		return err
	}
	for _, script := range scripts {
		if err := executeInitScript(ctx, script, workDir); err != nil {
			return err
		}
	}
	return nil
}

func listInitScripts(ctx context.Context, client initScriptsClient, agentID string) ([]initScript, error) {
	if client == nil {
		return nil, fmt.Errorf("init scripts client is required")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	var scripts []initScript
	pageToken := ""
	for {
		resp, err := client.ListInitScripts(ctx, &agentsv1.ListInitScriptsRequest{
			AgentId:   agentID,
			PageSize:  initScriptsPageSize,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, script := range resp.GetInitScripts() {
			parsed, err := initScriptFromProto(script)
			if err != nil {
				return nil, err
			}
			scripts = append(scripts, parsed)
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			break
		}
	}
	sort.Slice(scripts, func(i, j int) bool {
		if scripts[i].CreatedAt.Equal(scripts[j].CreatedAt) {
			return scripts[i].ID < scripts[j].ID
		}
		return scripts[i].CreatedAt.Before(scripts[j].CreatedAt)
	})
	return scripts, nil
}

func initScriptFromProto(script *agentsv1.InitScript) (initScript, error) {
	if script == nil {
		return initScript{}, fmt.Errorf("init script is required")
	}
	meta := script.GetMeta()
	if meta == nil {
		return initScript{}, fmt.Errorf("init script meta is required")
	}
	id := strings.TrimSpace(meta.GetId())
	if id == "" {
		return initScript{}, fmt.Errorf("init script id is required")
	}
	createdAt := meta.GetCreatedAt()
	if createdAt == nil {
		return initScript{}, fmt.Errorf("init script created at is required")
	}
	rawScript := script.GetScript()
	if strings.TrimSpace(rawScript) == "" {
		return initScript{}, fmt.Errorf("init script script is required")
	}
	return initScript{
		ID:        id,
		CreatedAt: createdAt.AsTime(),
		Script:    rawScript,
	}, nil
}

func executeInitScript(ctx context.Context, script initScript, workDir string) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", script.Script)
	if strings.TrimSpace(workDir) != "" {
		cmd.Dir = workDir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("init script %s failed: %s", script.ID, strings.TrimSpace(stderr.String()))
			return nil
		}
		return fmt.Errorf("run init script %s: %w", script.ID, err)
	}
	return nil
}
