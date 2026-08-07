package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/agynio/agynd-cli/internal/subscriber"
	"github.com/agynio/agynd-cli/internal/tracingproxy"
	claude "github.com/agynio/claude-sdk-go"
)

func newClaudeDaemon(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	// version is unused: the Claude SDK has no client-info metadata.
	_ = version

	setup, updatedCfg, err := connectPlatform(ctx, cfg)
	if err != nil {
		return nil, err
	}
	cfg = updatedCfg

	if _, err := writeSkills(cfg.SDK, setup.skills); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	if err := writeClaudeSettings(claudeBaseURL(cfg.LLMBaseURL), cfg.LLMAPIToken, cfg.MCPServers, cfg.LLMNative); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	if err := runInitScripts(ctx, setup.agents, cfg.AgentID.String(), cfg.EnvironmentID, cfg.WorkDir); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	tracingProxy, err := tracingproxy.Start(ctx, tracingproxy.Config{
		TracingAddress: cfg.TracingAddress,
		ListenAddress:  tracingProxyListenAddress,
		ThreadID:       cfg.ThreadID,
		WorkloadID:     cfg.WorkloadID,
	})
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	otlpEndpoint := "http://" + tracingProxy.Address()
	options := claude.Options{
		BinaryPath: cfg.AgentBinary,
		WorkDir:    cfg.WorkDir,
		Env: []string{
			"PATH=" + agentPathValue(),
			"LD_LIBRARY_PATH=/agyn/bin/lib",
			"CLAUDE_CODE_ENABLE_TELEMETRY=1",
			"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1",
			"OTEL_TRACES_EXPORTER=otlp",
			"OTEL_EXPORTER_OTLP_PROTOCOL=grpc",
			"OTEL_EXPORTER_OTLP_ENDPOINT=" + otlpEndpoint,
			"OTEL_METRICS_EXPORTER=none",
			"OTEL_LOGS_EXPORTER=none",
			"IS_SANDBOX=1",
		},
	}
	if model := claudeModel(cfg, setup.agent.GetModel()); model != "" {
		options.Model = model
		if !cfg.LLMNative {
			// A platform model is a UUID the vendor has never heard of, so the
			// CLI has to be told it is a model at all. A native model name is
			// the vendor's own and needs no such help.
			options.Env = append(options.Env,
				"ANTHROPIC_MODEL="+model,
				"ANTHROPIC_CUSTOM_MODEL_OPTION="+model,
			)
		}
	}
	if role := strings.TrimSpace(setup.agent.GetRole()); role != "" {
		options.SystemPrompt = role
	}
	claudeClient, err := claude.Start(ctx, options)
	if err != nil {
		tracingProxy.Close()
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	return &Daemon{
		cfg:          cfg,
		sdk:          SDKClaude,
		gatewayConn:  setup.gatewayConn,
		threads:      setup.threads,
		agents:       setup.agents,
		agentInbox:   setup.agentInbox,
		runners:      setup.runners,
		subscriber:   subscriber.New(setup.notifications, cfg.ThreadID),
		consumer:     platform.NewInboxConsumer(setup.agentInbox, pageSize, pageTimeout),
		claude:       claudeClient,
		agent:        setup.agent,
		tracingProxy: tracingProxy,
	}, nil
}

func (d *Daemon) ensureClaudeReady(ctx context.Context) error {
	d.claudeReadyMu.Lock()
	defer d.claudeReadyMu.Unlock()
	if d.claudeReady {
		return nil
	}
	if err := d.ensureMCPReady(ctx); err != nil {
		return err
	}
	d.claudeReady = true
	return nil
}

func (d *Daemon) handleClaudeMessage(ctx context.Context, message platform.Message) error {
	threadID := strings.TrimSpace(message.ThreadID)
	if threadID == "" {
		return fmt.Errorf("message %s missing thread id", message.ID)
	}
	inputText, err := buildInput(message)
	if err != nil {
		return err
	}
	if err := d.ensureClaudeReady(ctx); err != nil {
		return fmt.Errorf("prepare claude MCP servers: %w", err)
	}
	result, err := d.claude.Turn(ctx, claude.TurnParams{Prompt: inputText}, nil)
	if err != nil {
		return operationError(
			opClaudeTurn,
			0,
			fmt.Errorf("run claude turn for message %s on thread %s: %w", message.ID, threadID, err),
		)
	}
	response := strings.TrimSpace(result.Response)
	if err := d.publishFinalMessage(ctx, SDKClaude, message, response); err != nil {
		return err
	}
	if err := d.ackMessage(ctx, message); err != nil {
		return err
	}
	return nil
}

// claudeModel picks what to pin the CLI to: the platform Model UUID the proxy
// resolves, or the vendor's own model name. Unset in native mode -- the common
// case -- leaves the CLI on its own default and its own picker.
func claudeModel(cfg config.Config, platformModel string) string {
	if cfg.LLMNative {
		return strings.TrimSpace(cfg.LLMModelName)
	}
	return strings.TrimSpace(platformModel)
}
