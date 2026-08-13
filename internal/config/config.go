package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/agynio/agynd-cli/internal/uuidutil"
	"github.com/google/uuid"
)

const (
	agentVolumePath = "/agyn"
	agentConfigPath = agentVolumePath + "/config.json"
)

const (
	ModeAgent            = "agent"
	ModeHolder           = "holder"
	HolderDefaultWorkDir = "/workspace"
)

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
	Mode    string
	AgentID uuid.UUID
	// EnvironmentID names the environment this workload runs, whose MCPs and
	// init scripts apply alongside the agent's. Empty for an agent that names no
	// environment.
	EnvironmentID   string
	AgentInstanceID uuid.UUID
	GatewayAddress  string
	TracingAddress  string
	// ThreadID pins the daemon to a single thread. Instances serve every
	// thread that reaches their inbox, so it is empty for them and set only
	// by the thread-scoped callers that predate instances.
	ThreadID    string
	WorkloadID  string
	LLMBaseURL  string
	LLMAPIToken string
	// LLMNative leaves the agent CLI in its stock configuration: it addresses
	// its vendor directly and interception happens at the network layer, so the
	// container is never told the proxy exists. Delivered as an environment
	// variable rather than fetched, so it is knowable in holder mode where
	// nothing is being prepared.
	LLMNative bool
	// LLMModelName pins a model through the CLI's own model setting. Unset --
	// the common case, and always the case for a sandbox -- leaves the CLI on
	// its own default and its own picker.
	LLMModelName string
	SDK          string
	AgentBinary  string
	WorkDir      string
	MCPServers   []MCPServer
	MCPPort      *int
}

func FromEnv() (Config, error) {
	return fromEnv(agentConfigPath)
}

func fromEnv(configPath string) (Config, error) {
	mode, err := parseMode(os.Getenv("AGYND_MODE"))
	if err != nil {
		return Config{}, err
	}
	if mode == ModeHolder {
		return holderConfig(configPath)
	}

	agentID, err := uuidutil.ParseUUID(strings.TrimSpace(os.Getenv("AGENT_ID")), "AGENT_ID")
	if err != nil {
		return Config{}, err
	}
	environmentID := strings.TrimSpace(os.Getenv("ENVIRONMENT_ID"))
	gatewayAddress := gatewayAddressFromEnv()
	tracingAddress := strings.TrimSpace(os.Getenv("TRACING_ADDRESS"))
	if tracingAddress == "" {
		tracingAddress = "tracing.agyn:443"
	}
	agentInstanceID, err := uuidutil.ParseUUID(strings.TrimSpace(os.Getenv("AGENT_INSTANCE_ID")), "AGENT_INSTANCE_ID")
	if err != nil {
		return Config{}, err
	}
	// Optional: an instance consumes its inbox, which spans threads. Callers
	// that still scope the daemon to one thread set it and keep the old path.
	threadID := strings.TrimSpace(os.Getenv("THREAD_ID"))
	if threadID != "" {
		threadUUID, err := uuidutil.ParseUUID(threadID, "THREAD_ID")
		if err != nil {
			return Config{}, err
		}
		threadID = threadUUID.String()
	}
	workloadID := strings.TrimSpace(os.Getenv("WORKLOAD_ID"))
	workloadUUID, err := uuidutil.ParseUUID(workloadID, "WORKLOAD_ID")
	if err != nil {
		return Config{}, err
	}
	workloadID = workloadUUID.String()
	llmModelName := strings.TrimSpace(os.Getenv("LLM_MODEL_NAME"))

	mcpServers, err := parseMCPServers(os.Getenv("AGENT_MCP_SERVERS"))
	if err != nil {
		return Config{}, err
	}
	mcpPort, err := parseMCPPort(os.Getenv("MCP_PORT"))
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
	// An image states what it carries, not where the platform mounted it.
	if filepath.IsAbs(agentBinary) {
		return Config{}, fmt.Errorf("%s bin %q must be relative to the volume", configPath, agentBinary)
	}
	agentBinary = filepath.Join(agentVolumePath, agentBinary)

	workDir := strings.TrimSpace(os.Getenv("WORKSPACE_DIR"))
	if workDir == "" {
		workDir = "/tmp"
	}

	return Config{
		Mode:            mode,
		AgentID:         agentID,
		EnvironmentID:   environmentID,
		AgentInstanceID: agentInstanceID,
		GatewayAddress:  gatewayAddress,
		TracingAddress:  tracingAddress,
		ThreadID:        threadID,
		WorkloadID:      workloadID,
		LLMBaseURL:      llmBaseURLFromEnv(),
		LLMAPIToken:     llmAPITokenFromEnv(),
		LLMNative:       llmNativeFromEnv(),
		LLMModelName:    llmModelName,
		SDK:             sdk,
		AgentBinary:     agentBinary,
		WorkDir:         workDir,
		MCPServers:      mcpServers,
		MCPPort:         mcpPort,
	}, nil
}

func parseMode(raw string) (string, error) {
	mode := strings.TrimSpace(raw)
	if mode == "" {
		return ModeAgent, nil
	}
	switch mode {
	case ModeAgent, ModeHolder:
		return mode, nil
	default:
		return "", fmt.Errorf("AGYND_MODE must be %q or %q", ModeAgent, ModeHolder)
	}
}

// A sandbox prepares the agent CLI it will not spawn, so holder mode reads the
// same LLM and MCP variables agent mode does. It reads the environment and the
// gateway too: the environment's init scripts are its own to run, and fetching
// them is the one call a holder makes.
func holderConfig(configPath string) (Config, error) {
	workDir := strings.TrimSpace(os.Getenv("WORKSPACE_DIR"))
	if workDir == "" {
		workDir = HolderDefaultWorkDir
	}
	mcpServers, err := parseMCPServers(os.Getenv("AGENT_MCP_SERVERS"))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Mode:           ModeHolder,
		SDK:            ModeHolder,
		EnvironmentID:  strings.TrimSpace(os.Getenv("ENVIRONMENT_ID")),
		GatewayAddress: gatewayAddressFromEnv(),
		WorkDir:        workDir,
		LLMBaseURL:     llmBaseURLFromEnv(),
		LLMAPIToken:    llmAPITokenFromEnv(),
		LLMNative:      llmNativeFromEnv(),
		MCPServers:     mcpServers,
	}
	// A workspace-only environment carries no CLI and so no config.json. There
	// is nothing to prepare, which is not a failure.
	if agentCfg, err := loadAgentConfig(configPath); err == nil {
		if sdk := strings.TrimSpace(agentCfg.SDK); sdk != "" {
			cfg.SDK = sdk
		}
	}
	return cfg, nil
}

func gatewayAddressFromEnv() string {
	if address := strings.TrimSpace(os.Getenv("GATEWAY_ADDRESS")); address != "" {
		return address
	}
	return "gateway.agyn:443"
}

func llmBaseURLFromEnv() string {
	if llmBaseURL := strings.TrimSpace(os.Getenv("LLM_BASE_URL")); llmBaseURL != "" {
		return llmBaseURL
	}
	return "http://llm-proxy.agyn:443/v1"
}

func llmAPITokenFromEnv() string {
	if token := strings.TrimSpace(os.Getenv("LLM_API_TOKEN")); token != "" {
		return token
	}
	return "platform"
}

func llmNativeFromEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("LLM_MODE")), "native")
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

func parseMCPPort(raw string) (*int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("MCP_PORT has invalid port %q", value)
	}
	return &port, nil
}
