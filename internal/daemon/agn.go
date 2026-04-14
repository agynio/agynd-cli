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

	setup, err := connectPlatform(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := runInitScripts(ctx, setup.agents, cfg.AgentID.String(), cfg.WorkDir); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	summarization, err := parseAgentSummarization(setup.agent.GetConfiguration())
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	_, configPath, err := writeAgnConfig(
		cfg.LLMBaseURL,
		cfg.LLMAPIToken,
		setup.agent.GetModel(),
		summarization,
		cfg.MCPServers,
	)
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	if err := waitForMCPServers(ctx, cfg.MCPServers, mcpReadyTimeout); err != nil {
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
		runners:      setup.runners,
		subscriber:   subscriber.New(setup.notifications, cfg.AgentID.String(), cfg.ThreadID),
		consumer:     platform.NewConsumer(setup.threads, pageSize, pageTimeout),
		agn:          agnClient,
		agent:        setup.agent,
		tracingProxy: tracingProxy,
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
		return err
	}
	response := strings.TrimSpace(result.Response)
	if response == "" {
		return fmt.Errorf("agn turn completed with empty response")
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
