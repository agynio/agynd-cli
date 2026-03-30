package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	agnsdk "github.com/agynio/agn-sdk-go"
	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
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

const (
	SDKCodex  = "codex"
	SDKAgn    = "agn"
	SDKClaude = "claude"
)

type Daemon struct {
	cfg         config.Config
	sdk         string
	gatewayConn platformConn
	threads     *platform.Threads
	agents      gatewayv1.AgentsGatewayClient
	subscriber  *subscriber.Subscriber
	consumer    *platform.Consumer
	codex       *codex.Client
	codexHome   string
	mapping     *codexbridge.ThreadMapping
	tracker     *codexbridge.TurnTracker
	agn         *agnsdk.Client
	agnDir      string
	agent       *agentsv1.Agent

	syncMu sync.Mutex
}

type platformConn interface {
	Close() error
}

type platformSetup struct {
	gatewayConn   platformConn
	threads       *platform.Threads
	notifications *platform.Notifications
	agents        gatewayv1.AgentsGatewayClient
	agent         *agentsv1.Agent
}

func New(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	switch cfg.SDK {
	case SDKCodex:
		return newCodexDaemon(ctx, cfg, version)
	case SDKAgn:
		return newAgnDaemon(ctx, cfg, version)
	case SDKClaude:
		return nil, fmt.Errorf("sdk %q is not yet supported", cfg.SDK)
	default:
		return nil, fmt.Errorf("unknown sdk %q", cfg.SDK)
	}
}

func connectPlatform(ctx context.Context, cfg config.Config) (*platformSetup, error) {
	backoff := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}
	var lastErr error
	for i, delay := range backoff {
		setup, err := tryConnectPlatform(ctx, cfg)
		if err == nil {
			return setup, nil
		}
		lastErr = err
		log.Printf("platform connect attempt %d/%d failed: %v; retrying in %s", i+1, len(backoff)+1, err, delay)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	setup, err := tryConnectPlatform(ctx, cfg)
	if err != nil {
		if lastErr != nil {
			return nil, fmt.Errorf("platform connect failed after %d attempts: %w (previous: %v)", len(backoff)+1, err, lastErr)
		}
		return nil, fmt.Errorf("platform connect failed after %d attempts: %w", len(backoff)+1, err)
	}
	return setup, nil
}

func tryConnectPlatform(ctx context.Context, cfg config.Config) (*platformSetup, error) {
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

	return &platformSetup{
		gatewayConn:   gatewayConn,
		threads:       threadsClient,
		notifications: notificationsClient,
		agents:        agentsClient,
		agent:         agent,
	}, nil
}

func newCodexDaemon(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	setup, err := connectPlatform(ctx, cfg)
	if err != nil {
		return nil, err
	}

	tracker := codexbridge.NewTurnTracker()
	bridge := codexbridge.New(tracker)
	threadsMapping := codexbridge.NewThreadMapping()
	codexHome, err := writeCodexConfig(cfg.LLMBaseURL)
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}
	options := []codex.Option{
		codex.WithBinary(cfg.AgentBinary),
		codex.WithWorkDir(cfg.WorkDir),
		codex.WithEnv(map[string]string{
			"CODEX_HOME":     codexHome,
			"OPENAI_API_KEY": "platform",
		}),
		codex.WithNotificationHandler(bridge),
		codex.WithApprovalHandler(codex.AutoApprovalHandler{}),
		codex.WithClientInfo("agynd", version),
	}
	codexClient, err := codex.NewClient(ctx, options...)
	if err != nil {
		_ = setup.gatewayConn.Close()
		_ = os.RemoveAll(codexHome)
		return nil, err
	}

	return &Daemon{
		cfg:         cfg,
		sdk:         SDKCodex,
		gatewayConn: setup.gatewayConn,
		threads:     setup.threads,
		agents:      setup.agents,
		subscriber:  subscriber.New(setup.notifications, cfg.AgentID.String()),
		consumer:    platform.NewConsumer(setup.threads, pageSize, pageTimeout),
		codex:       codexClient,
		codexHome:   codexHome,
		mapping:     threadsMapping,
		tracker:     tracker,
		agent:       setup.agent,
	}, nil
}

func (d *Daemon) Close() {
	if d.codex != nil {
		_ = d.codex.Close()
	}
	if d.agn != nil {
		_ = d.agn.Close()
	}
	if d.gatewayConn != nil {
		_ = d.gatewayConn.Close()
	}
	if d.codexHome != "" {
		_ = os.RemoveAll(d.codexHome)
	}
	if d.agnDir != "" {
		_ = os.RemoveAll(d.agnDir)
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
	switch d.sdk {
	case SDKCodex:
		return d.handleCodexMessage(ctx, message)
	case SDKAgn:
		return d.handleAgnMessage(ctx, message)
	default:
		return fmt.Errorf("unknown sdk %q", d.sdk)
	}
}

func (d *Daemon) handleCodexMessage(ctx context.Context, message platform.Message) error {
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
	model := strings.TrimSpace(d.cfg.ModelOverride)
	if model == "" {
		model = strings.TrimSpace(d.agent.GetModel())
	}
	if model != "" {
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
