package daemon

import (
	"context"
	"fmt"
	"strings"

	agnsdk "github.com/agynio/agn-sdk-go"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/agynio/agynd-cli/internal/subscriber"
	"github.com/agynio/agynd-cli/internal/tracingproxy"
)

func newAgnDaemon(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	// version is unused: the agn SDK has no client-info metadata.
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

	agentConfig, err := parseAgentConfiguration(setup.agent.GetConfiguration())
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}
	systemPrompt := buildSystemPrompt(agentConfig.SystemPrompt, setup.skills)

	_, configPath, err := writeAgnConfig(
		cfg.LLMBaseURL,
		cfg.LLMAPIToken,
		setup.agent.GetModel(),
		systemPrompt,
		agentConfig.Summarization,
		cfg.MCPServers,
	)
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	if err := runInitScripts(ctx, setup.agents, cfg.AgentID.String(), cfg.WorkDir); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	if err := waitForMCPServers(ctx, cfg.MCPServers, mcpReadyTimeout); err != nil {
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
	agnClient, err := agnsdk.Start(ctx, agnsdk.Options{
		BinaryPath: cfg.AgentBinary,
		Env: []string{
			"PATH=" + agentPathValue(),
			"AGN_CONFIG_PATH=" + configPath,
			"OTEL_EXPORTER_OTLP_ENDPOINT=" + otlpEndpoint,
		},
	})
	if err != nil {
		tracingProxy.Close()
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	return &Daemon{
		cfg:          cfg,
		sdk:          SDKAgn,
		gatewayConn:  setup.gatewayConn,
		threads:      setup.threads,
		agents:       setup.agents,
		agentInbox:   setup.agentInbox,
		runners:      setup.runners,
		subscriber:   subscriber.New(setup.notifications, cfg.ThreadID),
		consumer:     platform.NewInboxConsumer(setup.agentInbox, pageSize, pageTimeout),
		agn:          agnClient,
		agent:        setup.agent,
		tracingProxy: tracingProxy,
		mcpReady:     true,
	}, nil
}

func (d *Daemon) handleAgnMessage(ctx context.Context, message platform.Message) error {
	threadID := strings.TrimSpace(message.ThreadID)
	if threadID == "" {
		return fmt.Errorf("message %s missing thread id", message.ID)
	}
	inputText, err := buildInput(message)
	if err != nil {
		return err
	}
	result, err := d.agn.Turn(ctx, agnsdk.TurnParams{
		Prompt:   inputText,
		ThreadID: threadID,
	}, nil)
	if err != nil {
		return operationError(
			opAgnTurn,
			0,
			fmt.Errorf("run agn turn for message %s on thread %s: %w", message.ID, threadID, err),
		)
	}
	response := strings.TrimSpace(result.Response)
	if err := d.publishFinalMessage(ctx, SDKAgn, message, response); err != nil {
		return err
	}
	if err := d.ackMessage(ctx, message); err != nil {
		return err
	}
	return nil
}
