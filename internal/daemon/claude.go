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

	setup, err := connectPlatform(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := writeClaudeSettings(claudeBaseURL(cfg.LLMBaseURL), cfg.LLMAPIToken, cfg.MCPServers); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	tracingProxy, err := tracingproxy.Start(ctx, tracingproxy.Config{
		TracingAddress: cfg.TracingAddress,
		ThreadID:       cfg.ThreadID,
	})
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	otlpEndpoint := "http://" + tracingproxy.ListenAddress
	options := claude.Options{
		BinaryPath: cfg.AgentBinary,
		WorkDir:    cfg.WorkDir,
		Env: []string{
			"PATH=" + agentPathValue(),
			"LD_LIBRARY_PATH=/agyn-bin/lib",
			"OTEL_EXPORTER_OTLP_ENDPOINT=" + otlpEndpoint,
			"IS_SANDBOX=1",
		},
	}
	if model := strings.TrimSpace(setup.agent.GetModel()); model != "" {
		options.Model = model
		options.Env = append(options.Env,
			"ANTHROPIC_MODEL="+model,
			"ANTHROPIC_CUSTOM_MODEL_OPTION="+model,
		)
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
		runners:      setup.runners,
		subscriber:   subscriber.New(setup.notifications, cfg.AgentID.String()),
		consumer:     platform.NewConsumer(setup.threads, pageSize, pageTimeout),
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
	if err := waitForMCPServers(ctx, d.cfg.MCPServers, mcpReadyTimeout); err != nil {
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
		return err
	}
	turnCtx, cancel := context.WithTimeout(ctx, turnCompletionTimeout)
	defer cancel()
	result, err := d.claude.Turn(turnCtx, claude.TurnParams{Prompt: inputText}, nil)
	if err != nil {
		return err
	}
	response := strings.TrimSpace(result.Response)
	if response == "" {
		return fmt.Errorf("claude turn completed with empty response")
	}
	publishCtx, cancel := context.WithTimeout(ctx, messagePublishTimeout)
	_, err = d.threads.SendMessage(publishCtx, threadID, d.cfg.AgentID.String(), response, nil)
	cancel()
	if err != nil {
		return err
	}
	ackCtx, cancel := context.WithTimeout(ctx, messageAckTimeout)
	err = d.threads.AckMessages(ackCtx, d.cfg.AgentID.String(), []string{message.ID})
	cancel()
	if err != nil {
		return fmt.Errorf("ack message %s: %w", message.ID, err)
	}
	return nil
}
