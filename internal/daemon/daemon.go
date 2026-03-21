package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"github.com/agynio/agynd-cli/internal/codexbridge"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/agynio/agynd-cli/internal/subscriber"
	codex "github.com/agynio/codex-sdk-go"
)

const (
	pageSize              int32 = 100
	pageTimeout                 = 30 * time.Second
	turnStartTimeout            = 5 * time.Minute
	turnCompletionTimeout       = 5 * time.Minute
	messagePublishTimeout       = 15 * time.Second
	messageAckTimeout           = 15 * time.Second
)

type Daemon struct {
	cfg        config.Config
	conns      *platform.Connections
	threads    *platform.Threads
	agents     agentsv1.AgentsServiceClient
	subscriber *subscriber.Subscriber
	consumer   *platform.Consumer
	codex      *codex.Client
	mapping    *codexbridge.ThreadMapping
	tracker    *codexbridge.TurnTracker
	agent      *agentsv1.Agent

	syncMu sync.Mutex
}

func New(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	conns, err := platform.DialConnections(ctx, cfg.ThreadsAddress, cfg.NotificationsAddress, cfg.TeamsAddress)
	if err != nil {
		return nil, err
	}

	threadsClient := platform.NewThreads(conns.Threads)
	notificationsClient := platform.NewNotifications(conns.Notifications)
	agentsClient := agentsv1.NewAgentsServiceClient(conns.Teams)

	agentResp, err := agentsClient.GetAgent(ctx, &agentsv1.GetAgentRequest{Id: cfg.AgentID.String()})
	if err != nil {
		conns.Close()
		return nil, fmt.Errorf("get agent: %w", err)
	}
	agent := agentResp.GetAgent()
	if agent == nil {
		conns.Close()
		return nil, fmt.Errorf("agent not found")
	}

	tracker := codexbridge.NewTurnTracker()
	bridge := codexbridge.New(tracker)
	threadsMapping := codexbridge.NewThreadMapping()
	codexClient, err := codex.NewClient(ctx,
		codex.WithBinary(cfg.CodexBinary),
		codex.WithWorkDir(cfg.WorkDir),
		codex.WithNotificationHandler(bridge),
		codex.WithApprovalHandler(codex.AutoApprovalHandler{}),
		codex.WithClientInfo("agynd", version),
		codex.WithEnv(map[string]string{"OPENAI_API_KEY": cfg.OpenAIAPIKey}),
	)
	if err != nil {
		conns.Close()
		return nil, err
	}

	return &Daemon{
		cfg:        cfg,
		conns:      conns,
		threads:    threadsClient,
		agents:     agentsClient,
		subscriber: subscriber.New(notificationsClient, cfg.AgentID.String()),
		consumer:   platform.NewConsumer(threadsClient, pageSize, pageTimeout),
		codex:      codexClient,
		mapping:    threadsMapping,
		tracker:    tracker,
		agent:      agent,
	}, nil
}

func (d *Daemon) Close() {
	if d.codex != nil {
		_ = d.codex.Close()
	}
	if d.conns != nil {
		d.conns.Close()
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	if err := d.syncMessages(ctx); err != nil {
		return err
	}

	go func() {
		if err := d.subscriber.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("subscriber stopped: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.subscriber.Wake():
			if err := d.syncMessages(ctx); err != nil {
				log.Printf("sync messages failed: %v", err)
			}
		}
	}
}

func (d *Daemon) syncMessages(ctx context.Context) error {
	d.syncMu.Lock()
	defer d.syncMu.Unlock()

	return d.consumer.Sync(ctx, d.cfg.AgentID.String(), func(message platform.Message) error {
		return d.handleMessage(ctx, message)
	})
}

func (d *Daemon) handleMessage(ctx context.Context, message platform.Message) error {
	threadID := strings.TrimSpace(message.ThreadID)
	if threadID == "" {
		return fmt.Errorf("message %s missing thread id", message.ID)
	}
	inputText, err := buildInput(message)
	if err != nil {
		return err
	}
	codexThreadID, ok := d.mapping.CodexForPlatform(threadID)
	if !ok {
		codexThreadID, err = d.startCodexThread(ctx)
		if err != nil {
			return err
		}
		d.mapping.Set(threadID, codexThreadID)
	}
	turnCtx, cancel := context.WithTimeout(ctx, turnStartTimeout)
	turnResp, err := d.codex.StartTurn(turnCtx, &codex.TurnStartParams{
		ThreadID: codexThreadID,
		Input:    []codex.UserInput{codex.NewTextUserInput(inputText)},
	})
	cancel()
	if err != nil {
		return err
	}
	turnID := strings.TrimSpace(turnResp.Turn.ID)
	if turnID == "" {
		return fmt.Errorf("codex turn id missing")
	}
	completionCh := d.tracker.Register(turnID)
	completionCtx, cancel := context.WithTimeout(ctx, turnCompletionTimeout)
	defer cancel()
	select {
	case result := <-completionCh:
		if result.Err != nil {
			return result.Err
		}
		if result.ThreadID != codexThreadID {
			return fmt.Errorf("turn %s thread mismatch", turnID)
		}
		if strings.TrimSpace(result.Message) == "" {
			return fmt.Errorf("turn %s completed with empty response", turnID)
		}
		publishCtx, cancel := context.WithTimeout(ctx, messagePublishTimeout)
		_, err := d.threads.SendMessage(publishCtx, threadID, d.cfg.AgentID.String(), result.Message, nil)
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
	case <-completionCtx.Done():
		d.tracker.Cancel(turnID)
		return completionCtx.Err()
	}
}

func (d *Daemon) startCodexThread(ctx context.Context) (string, error) {
	params := &codex.ThreadStartParams{}
	if model := strings.TrimSpace(d.agent.GetModel()); model != "" {
		params.Model = &model
	}
	if role := strings.TrimSpace(d.agent.GetRole()); role != "" {
		params.BaseInstructions = &role
	}
	if config := strings.TrimSpace(d.agent.GetConfiguration()); config != "" {
		params.DeveloperInstructions = &config
	}
	if d.cfg.WorkDir != "" {
		params.Cwd = &d.cfg.WorkDir
	}
	resp, err := d.codex.StartThread(ctx, params)
	if err != nil {
		return "", fmt.Errorf("start codex thread: %w", err)
	}
	return resp.Thread.ID, nil
}

func buildInput(message platform.Message) (string, error) {
	text := strings.TrimSpace(message.Body)
	if text == "" && len(message.FileIDs) > 0 {
		text = fmt.Sprintf("Received files: %s", strings.Join(message.FileIDs, ", "))
	}
	if text == "" {
		return "", fmt.Errorf("message %s has no content", message.ID)
	}
	return text, nil
}
