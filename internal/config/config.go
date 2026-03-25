package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/agynio/agynd-cli/internal/uuidutil"
	"github.com/google/uuid"
)

type Config struct {
	AgentID              uuid.UUID
	ThreadsAddress       string
	NotificationsAddress string
	TeamsAddress         string
	SDK                  string
	AgentBinary          string
	LLMBaseURL           string
	WorkDir              string
}

func Load() (Config, error) {
	agentID, err := uuidutil.ParseUUID(strings.TrimSpace(os.Getenv("AGENT_ID")), "AGENT_ID")
	if err != nil {
		return Config{}, err
	}
	threadsAddress := strings.TrimSpace(os.Getenv("THREADS_ADDRESS"))
	if threadsAddress == "" {
		return Config{}, fmt.Errorf("THREADS_ADDRESS is required")
	}
	notificationsAddress := strings.TrimSpace(os.Getenv("NOTIFICATIONS_ADDRESS"))
	if notificationsAddress == "" {
		return Config{}, fmt.Errorf("NOTIFICATIONS_ADDRESS is required")
	}
	teamsAddress := strings.TrimSpace(os.Getenv("TEAMS_ADDRESS"))
	if teamsAddress == "" {
		return Config{}, fmt.Errorf("TEAMS_ADDRESS is required")
	}

	sdk := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_SDK")))
	if sdk == "" {
		sdk = "codex"
	}
	switch sdk {
	case "codex", "agn":
	default:
		return Config{}, fmt.Errorf("AGENT_SDK must be \"codex\" or \"agn\"")
	}

	agentBinary := strings.TrimSpace(os.Getenv("AGENT_BINARY"))
	if agentBinary == "" {
		agentBinary = strings.TrimSpace(os.Getenv("CODEX_BINARY"))
	}
	if agentBinary == "" {
		agentBinary = "codex"
	}

	llmBaseURL := strings.TrimSpace(os.Getenv("LLM_BASE_URL"))
	if sdk == "agn" && llmBaseURL == "" {
		return Config{}, fmt.Errorf("LLM_BASE_URL is required for agn sdk")
	}
	workDir := strings.TrimSpace(os.Getenv("WORKSPACE_DIR"))
	if workDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("determine working directory: %w", err)
		}
		workDir = cwd
	}

	return Config{
		AgentID:              agentID,
		ThreadsAddress:       threadsAddress,
		NotificationsAddress: notificationsAddress,
		TeamsAddress:         teamsAddress,
		SDK:                  sdk,
		AgentBinary:          agentBinary,
		LLMBaseURL:           llmBaseURL,
		WorkDir:              workDir,
	}, nil
}
