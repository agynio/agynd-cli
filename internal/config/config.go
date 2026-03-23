package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/agynio/agynd-cli/internal/uuidutil"
	"github.com/google/uuid"
)

var agentConfigPath = "/agyn-bin/config.json"

type agentConfig struct {
	SDK string `json:"sdk"`
	Bin string `json:"bin"`
}

type Config struct {
	AgentID        uuid.UUID
	GatewayAddress string
	SDK            string
	AgentBinary    string
	WorkDir        string
}

func FromEnv() (Config, error) {
	agentID, err := uuidutil.ParseUUID(strings.TrimSpace(os.Getenv("AGENT_ID")), "AGENT_ID")
	if err != nil {
		return Config{}, err
	}
	gatewayAddress := strings.TrimSpace(os.Getenv("GATEWAY_ADDRESS"))
	if gatewayAddress == "" {
		return Config{}, fmt.Errorf("GATEWAY_ADDRESS is required")
	}

	agentCfg, err := loadAgentConfig(agentConfigPath)
	if err != nil {
		return Config{}, err
	}
	sdk := strings.TrimSpace(agentCfg.SDK)
	if sdk == "" {
		return Config{}, fmt.Errorf("%s missing sdk", agentConfigPath)
	}
	agentBinary := strings.TrimSpace(agentCfg.Bin)
	if agentBinary == "" {
		return Config{}, fmt.Errorf("%s missing bin", agentConfigPath)
	}

	workDir := strings.TrimSpace(os.Getenv("WORKSPACE_DIR"))
	if workDir == "" {
		workDir = "/workspace"
	}

	return Config{
		AgentID:        agentID,
		GatewayAddress: gatewayAddress,
		SDK:            sdk,
		AgentBinary:    agentBinary,
		WorkDir:        workDir,
	}, nil
}

func loadAgentConfig(path string) (agentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return agentConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg agentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return agentConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}
