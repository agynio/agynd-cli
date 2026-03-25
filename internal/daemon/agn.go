package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"

	agnsdk "github.com/agynio/agn-sdk-go"
	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/agynio/agynd-cli/internal/subscriber"
)

func newAgnDaemon(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	_ = version

	gatewayConn, err := platform.DialGateway(cfg.GatewayAddress)
	if err != nil {
		return nil, fmt.Errorf("dial gateway: %w", err)
	}

	threadsGateway := gatewayv1.NewThreadsGatewayClient(gatewayConn)
	notificationsGateway := gatewayv1.NewNotificationsGatewayClient(gatewayConn)
	agentsClient := gatewayv1.NewAgentsGatewayClient(gatewayConn)

	threadsClient := platform.NewThreads(threadsGateway)
	notificationsClient := platform.NewNotifications(notificationsGateway)

	agentResp, err := agentsClient.GetAgent(ctx, &agentsv1.GetAgentRequest{Id: cfg.AgentID.String()})
	if err != nil {
		_ = gatewayConn.Close()
		return nil, fmt.Errorf("get agent: %w", err)
	}
	agent := agentResp.GetAgent()
	if agent == nil {
		_ = gatewayConn.Close()
		return nil, fmt.Errorf("agent not found")
	}

	agnDir, configPath, err := writeAgnConfig(cfg.LLMBaseURL)
	if err != nil {
		_ = gatewayConn.Close()
		return nil, err
	}

	agnClient, err := agnsdk.Start(ctx, agnsdk.Options{
		BinaryPath: cfg.AgentBinary,
		Env: []string{
			"AGN_CONFIG_PATH=" + configPath,
			"OPENAI_API_KEY=platform",
		},
	})
	if err != nil {
		_ = gatewayConn.Close()
		_ = os.RemoveAll(agnDir)
		return nil, err
	}

	return &Daemon{
		cfg:         cfg,
		sdk:         "agn",
		gatewayConn: gatewayConn,
		threads:     threadsClient,
		agents:      agentsClient,
		subscriber:  subscriber.New(notificationsClient, cfg.AgentID.String()),
		consumer:    platform.NewConsumer(threadsClient, pageSize, pageTimeout),
		agn:         agnClient,
		agnDir:      agnDir,
		agent:       agent,
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
