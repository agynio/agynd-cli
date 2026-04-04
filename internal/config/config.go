package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/agynio/agynd-cli/internal/uuidutil"
	"github.com/google/uuid"
)

const agentConfigPath = "/agyn-bin/config.json"

var mcpServerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type agentConfig struct {
	SDK string `json:"sdk"`
	Bin string `json:"bin"`
}

type MCPServer struct {
	Name string
	Port int
}

type Config struct {
	AgentID        uuid.UUID
	GatewayAddress string
	LLMBaseURL     string
	LLMAPIToken    string
	SDK            string
	AgentBinary    string
	WorkDir        string
	MCPServers     []MCPServer
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
	llmAPIToken := strings.TrimSpace(os.Getenv("LLM_API_TOKEN"))
	if llmAPIToken == "" {
		llmAPIToken = "platform"
	}

	mcpServers, err := parseMCPServers(os.Getenv("AGENT_MCP_SERVERS"))
	if err != nil {
		return Config{}, err
	}

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
		LLMAPIToken:    llmAPIToken,
		SDK:            sdk,
		AgentBinary:    agentBinary,
		WorkDir:        workDir,
		MCPServers:     mcpServers,
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

func parseMCPServers(raw string) ([]MCPServer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	entries := strings.Split(raw, ",")
	servers := make([]MCPServer, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("AGENT_MCP_SERVERS contains empty entry")
		}
		parts := strings.Split(entry, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("AGENT_MCP_SERVERS entry %q must be name:port", entry)
		}
		name := strings.TrimSpace(parts[0])
		if name == "" {
			return nil, fmt.Errorf("AGENT_MCP_SERVERS entry %q missing name", entry)
		}
		if !mcpServerNamePattern.MatchString(name) {
			return nil, fmt.Errorf("AGENT_MCP_SERVERS entry %q has invalid name", entry)
		}
		portRaw := strings.TrimSpace(parts[1])
		if portRaw == "" {
			return nil, fmt.Errorf("AGENT_MCP_SERVERS entry %q missing port", entry)
		}
		port, err := strconv.Atoi(portRaw)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("AGENT_MCP_SERVERS entry %q has invalid port", entry)
		}
		servers = append(servers, MCPServer{Name: name, Port: port})
	}
	return servers, nil
}
