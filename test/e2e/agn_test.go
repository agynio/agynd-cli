//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agnsdk "github.com/agynio/agn-sdk-go"
)

const agnTestLLMEndpoint = "https://testllm.dev/v1/org/agynio/suite/agn"

var (
	buildAgnOnce  sync.Once
	buildAgnErr   error
	agnBinaryPath string
)

func buildAgn(t *testing.T) string {
	t.Helper()
	buildAgnOnce.Do(func() {
		repoPath := os.Getenv("AGN_REPO_PATH")
		if repoPath == "" {
			buildAgnErr = fmt.Errorf("AGN_REPO_PATH must be set to the agn-cli repository root")
			return
		}
		dir, err := os.MkdirTemp("", "agn-e2e-")
		if err != nil {
			buildAgnErr = err
			return
		}
		agnBinaryPath = filepath.Join(dir, "agn")
		cmd := exec.Command("go", "build", "-o", agnBinaryPath, "./cmd/agn")
		cmd.Dir = repoPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			buildAgnErr = fmt.Errorf("build agn: %w: %s", err, strings.TrimSpace(string(output)))
		}
	})
	if buildAgnErr != nil {
		t.Fatalf("build agn binary: %v", buildAgnErr)
	}
	return agnBinaryPath
}

func writeAgnTestConfig(t *testing.T, model string, tokenCountingAddress string) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	config := fmt.Sprintf(`llm:
  endpoint: %s
  auth:
    api_key: dummy
  model: %s
token_counting:
  address: %q
`, agnTestLLMEndpoint, model, tokenCountingAddress)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write agn config: %v", err)
	}
	return configPath
}

func TestAgnClientHelloResponse(t *testing.T) {
	binary := buildAgn(t)
	tokenCountingAddr := startTokenCountingServer(t)
	configPath := writeAgnTestConfig(t, "simple-hello", tokenCountingAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := agnsdk.Start(ctx, agnsdk.Options{
		BinaryPath: binary,
		Env: []string{
			"AGN_CONFIG_PATH=" + configPath,
			"AGN_MCP_COMMAND=",
		},
	})
	if err != nil {
		t.Fatalf("start agn client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})

	result, err := client.Turn(ctx, agnsdk.TurnParams{
		Prompt: "hi",
	}, nil)
	if err != nil {
		t.Fatalf("turn failed: %v", err)
	}

	if result.Response != "Hi! How are you?" {
		t.Fatalf("unexpected response: %q", result.Response)
	}
	if strings.TrimSpace(result.ThreadID) == "" {
		t.Fatalf("thread ID is empty")
	}
}
