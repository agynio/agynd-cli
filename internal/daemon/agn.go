package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	agnsdk "github.com/agynio/agn-sdk-go"
	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"github.com/agynio/agynd-cli/internal/config"
)

func newAgnDaemon(ctx context.Context, cfg config.Config, _ string) (*Daemon, error) {
	daemon, err := newDaemonBase(ctx, cfg)
	if err != nil {
		return nil, err
	}
	dir, configPath, err := writeAgnConfig(cfg.LLMBaseURL)
	if err != nil {
		daemon.Close()
		return nil, err
	}
	daemon.agnDir = dir

	agentEnv := []string{fmt.Sprintf("AGN_CONFIG_PATH=%s", configPath)}
	agent, err := agnsdk.Start(ctx, agnsdk.Options{
		BinaryPath: cfg.AgentBinary,
		Env:        agentEnv,
	})
	if err != nil {
		daemon.Close()
		return nil, err
	}
	daemon.sdk = "agn"
	daemon.agn = agent
	return daemon, nil
}

func (d *Daemon) handleAgnMessage(ctx context.Context, message *threadsv1.Message) error {
	threadID := strings.TrimSpace(message.GetThreadId())
	if threadID == "" {
		return fmt.Errorf("message %s missing thread id", message.GetId())
	}
	inputText, err := buildInput(message)
	if err != nil {
		return err
	}
	turnCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	result, err := d.agn.Turn(turnCtx, agnsdk.TurnParams{
		Prompt:   inputText,
		ThreadID: threadID,
	}, nil)
	cancel()
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	_, err = d.threads.SendMessage(sendCtx, &threadsv1.SendMessageRequest{
		ThreadId: threadID,
		SenderId: d.cfg.AgentID.String(),
		Body:     result.Response,
	})
	cancel()
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	ackCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	_, err = d.threads.AckMessages(ackCtx, &threadsv1.AckMessagesRequest{
		ParticipantId: d.cfg.AgentID.String(),
		MessageIds:    []string{message.GetId()},
	})
	cancel()
	if err != nil {
		return fmt.Errorf("ack message %s: %w", message.GetId(), err)
	}
	return nil
}
