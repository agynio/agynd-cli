package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/agynio/agynd-cli/internal/uuidutil"
	"github.com/google/uuid"
)

const agentConfigPath = "/agyn-bin/config.json"

type agentConfig struct {
	SDK string `json:"sdk"`
	Bin string `json:"bin"`
}

type Config struct {
	AgentID        uuid.UUID
	GatewayAddress string
	LLMBaseURL     string
	ModelOverride  string
	SDK            string
	AgentBinary    string
	WorkDir        string
}

func FromEnv() (Config, error) {
	return fromEnv(agentConfigPath)
}

func fromEnv(configPath string) (Config, error) {
	agentID, err := uuidutil.ParseUUID(strings.TrimSpace(os.Getenv("AGENT_ID")), "AGENT_ID")
	if err != nil {
		return Config{}, err
	}
	gatewayAddress := strings.TrimSpace(os.Getenv("GATEWAY_ADDRESS"))
	if gatewayAddress == "" {
		gatewayAddress = "gateway.ziti:443"
	}
	llmBaseURL := strings.TrimSpace(os.Getenv("LLM_BASE_URL"))
	if llmBaseURL == "" {
		llmBaseURL = "http://llm-proxy.ziti:443/v1"
	}
	modelOverride := strings.TrimSpace(os.Getenv("MODEL_OVERRIDE"))

	agentCfg, err := loadAgentConfig(configPath)
	if err != nil {
		return Config{}, err
	}
	sdk := strings.TrimSpace(agentCfg.SDK)
	if sdk == "" {
		return Config{}, fmt.Errorf("%s missing sdk", configPath)
	}
	agentBinary := strings.TrimSpace(agentCfg.Bin)
	if agentBinary == "" {
		return Config{}, fmt.Errorf("%s missing bin", configPath)
	}

	workDir := strings.TrimSpace(os.Getenv("WORKSPACE_DIR"))
	if workDir == "" {
		workDir = "/workspace"
	}

	return Config{
		AgentID:        agentID,
		GatewayAddress: gatewayAddress,
		LLMBaseURL:     llmBaseURL,
		ModelOverride:  modelOverride,
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
