package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	agnsdk "github.com/agynio/agn-sdk-go"
	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	"github.com/agynio/agynd-cli/internal/codexbridge"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/agynio/agynd-cli/internal/subscriber"
	"github.com/agynio/agynd-cli/internal/tracingproxy"
	claude "github.com/agynio/claude-sdk-go"
	codex "github.com/agynio/codex-sdk-go"
)

const (
	pageSize              int32 = 100
	pageTimeout                 = 30 * time.Second
	turnStartTimeout            = 5 * time.Minute
	turnCompletionTimeout       = 5 * time.Minute
	messagePublishTimeout       = 15 * time.Second
	messageAckTimeout           = 15 * time.Second
	mcpReadyTimeout             = 120 * time.Second
)

const (
	SDKCodex  = "codex"
	SDKAgn    = "agn"
	SDKClaude = "claude"
)

type Daemon struct {
	cfg           config.Config
	sdk           string
	gatewayConn   platformConn
	threads       *platform.Threads
	agents        gatewayv1.AgentsGatewayClient
	runners       runnersClient
	subscriber    *subscriber.Subscriber
	consumer      *platform.Consumer
	codex         codexClient
	mapping       *codexbridge.ThreadMapping
	mappingStore  *codexbridge.ThreadMappingStore
	tracker       *codexbridge.TurnTracker
	agn           *agnsdk.Client
	claude        claudeClient
	agent         *agentsv1.Agent
	tracingProxy  *tracingproxy.Proxy
	claudeReadyMu sync.Mutex
	claudeReady   bool

	processing atomic.Bool
	syncMu     sync.Mutex
}

type platformConn interface {
	Close() error
}

type codexClient interface {
	StartThread(ctx context.Context, params *codex.ThreadStartParams) (*codex.ThreadStartResponse, error)
	ResumeThread(ctx context.Context, params *codex.ThreadResumeParams) (*codex.ThreadResumeResponse, error)
	StartTurn(ctx context.Context, params *codex.TurnStartParams) (*codex.TurnStartResponse, error)
	Close() error
}

type claudeClient interface {
	Turn(ctx context.Context, params claude.TurnParams, handler claude.EventHandler) (*claude.TurnResult, error)
	Close() error
}

type runnersClient interface {
	TouchWorkload(ctx context.Context, workloadID string) error
}

type platformSetup struct {
	gatewayConn   platformConn
	threads       *platform.Threads
	notifications *platform.Notifications
	agents        gatewayv1.AgentsGatewayClient
	runners       *platform.Runners
	agent         *agentsv1.Agent
	skills        []skill
}

func New(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	switch cfg.SDK {
	case SDKCodex:
		return newCodexDaemon(ctx, cfg, version)
	case SDKAgn:
		return newAgnDaemon(ctx, cfg, version)
	case SDKClaude:
		return newClaudeDaemon(ctx, cfg, version)
	default:
		return nil, fmt.Errorf("unknown sdk %q", cfg.SDK)
	}
}

func connectPlatform(ctx context.Context, cfg config.Config) (*platformSetup, config.Config, error) {
	backoff := []time.Duration{
		1 * time.Second,
		1 * time.Second,
		2 * time.Second,
		3 * time.Second,
		5 * time.Second,
		5 * time.Second,
		8 * time.Second,
		10 * time.Second,
		15 * time.Second,
		15 * time.Second,
		15 * time.Second,
	}
	var lastErr error
	for i, delay := range backoff {
		if err := ctx.Err(); err != nil {
			return nil, config.Config{}, err
		}
		setup, updatedCfg, err := tryConnectPlatform(ctx, cfg)
		if err == nil {
			return setup, updatedCfg, nil
		}
		lastErr = err
		log.Printf(
			"platform connect attempt %d/%d failed: %v; retrying in %s",
			i+1,
			len(backoff)+1,
			err,
			delay,
		)
		select {
		case <-ctx.Done():
			return nil, config.Config{}, ctx.Err()
		case <-time.After(delay):
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, config.Config{}, err
	}
	setup, updatedCfg, err := tryConnectPlatform(ctx, cfg)
	if err != nil {
		return nil, config.Config{}, fmt.Errorf(
			"platform connect failed after %d attempts: %w (previous: %v)",
			len(backoff)+1,
			err,
			lastErr,
		)
	}
	return setup, updatedCfg, nil
}

func tryConnectPlatform(ctx context.Context, cfg config.Config) (*platformSetup, config.Config, error) {
	gatewayConn, err := platform.DialGateway(cfg.GatewayAddress)
	if err != nil {
		return nil, config.Config{}, fmt.Errorf("dial gateway: %w", err)
	}

	threadsGateway := gatewayv1.NewThreadsGatewayClient(gatewayConn)
	notificationsGateway := gatewayv1.NewNotificationsGatewayClient(gatewayConn)
	agentsClient := gatewayv1.NewAgentsGatewayClient(gatewayConn)
	runnersGateway := gatewayv1.NewRunnersGatewayClient(gatewayConn)

	threadsClient := platform.NewThreads(threadsGateway)
	notificationsClient := platform.NewNotifications(notificationsGateway)
	runnersClient := platform.NewRunners(runnersGateway)

	agentResp, err := agentsClient.GetAgent(ctx, &agentsv1.GetAgentRequest{Id: cfg.AgentID.String()})
	if err != nil {
		_ = gatewayConn.Close()
		return nil, config.Config{}, fmt.Errorf("get agent: %w", err)
	}
	agent := agentResp.GetAgent()
	if agent == nil {
		_ = gatewayConn.Close()
		return nil, config.Config{}, fmt.Errorf("agent not found")
	}

	skills, err := listSkills(ctx, agentsClient, cfg.AgentID.String())
	if err != nil {
		_ = gatewayConn.Close()
		return nil, config.Config{}, fmt.Errorf("list skills: %w", err)
	}

	mcpDefinitions, err := listMCPs(ctx, agentsClient, cfg.AgentID.String())
	if err != nil {
		_ = gatewayConn.Close()
		return nil, config.Config{}, fmt.Errorf("list MCPs: %w", err)
	}

	resolvedMCPs, err := resolveMCPServers(mcpDefinitions, cfg.MCPServers, cfg.MCPPort)
	if err != nil {
		_ = gatewayConn.Close()
		return nil, config.Config{}, err
	}
	updatedCfg := cfg
	updatedCfg.MCPServers = resolvedMCPs
	updatedCfg.MCPPort = nil

	return &platformSetup{
		gatewayConn:   gatewayConn,
		threads:       threadsClient,
		notifications: notificationsClient,
		agents:        agentsClient,
		runners:       runnersClient,
		agent:         agent,
		skills:        skills,
	}, updatedCfg, nil
}

func newCodexDaemon(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	setup, updatedCfg, err := connectPlatform(ctx, cfg)
	if err != nil {
		return nil, err
	}
	cfg = updatedCfg

	if _, err := writeSkills(cfg.SDK, setup.skills); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	tracker := codexbridge.NewTurnTracker()
	bridge := codexbridge.New(tracker)
	threadsMapping := codexbridge.NewThreadMapping()
	codexHome, err := writeCodexConfig(cfg.LLMBaseURL, cfg.MCPServers)
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	if err := runInitScripts(ctx, setup.agents, cfg.AgentID.String(), cfg.WorkDir); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}
	codexHomeValue := codexHomeEnv()
	mappingStore := codexbridge.NewThreadMappingStore(codexHomeValue)

	tracingProxy, err := tracingproxy.Start(ctx, tracingproxy.Config{
		TracingAddress: cfg.TracingAddress,
		ThreadID:       cfg.ThreadID,
		WorkloadID:     cfg.WorkloadID,
	})
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}
	otlpEndpoint := "http://" + tracingproxy.ListenAddress
	options := []codex.Option{
		codex.WithBinary(cfg.AgentBinary),
		codex.WithWorkDir(cfg.WorkDir),
		codex.WithEnv(map[string]string{
			"PATH":                        agentPathValue(),
			"CODEX_HOME":                  codexHome,
			"HOME":                        codexHomeValue,
			"OPENAI_API_KEY":              cfg.LLMAPIToken,
			"OTEL_EXPORTER_OTLP_ENDPOINT": otlpEndpoint,
		}),
		codex.WithNotificationHandler(bridge),
		codex.WithApprovalHandler(codex.AutoApprovalHandler{}),
		codex.WithClientInfo("agynd", version),
	}
	codexClient, err := codex.NewClient(ctx, options...)
	if err != nil {
		tracingProxy.Close()
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	return &Daemon{
		cfg:          cfg,
		sdk:          SDKCodex,
		gatewayConn:  setup.gatewayConn,
		threads:      setup.threads,
		agents:       setup.agents,
		runners:      setup.runners,
		subscriber:   subscriber.New(setup.notifications, cfg.AgentID.String(), cfg.ThreadID),
		consumer:     platform.NewConsumer(setup.threads, pageSize, pageTimeout),
		codex:        codexClient,
		mapping:      threadsMapping,
		mappingStore: mappingStore,
		tracker:      tracker,
		agent:        setup.agent,
		tracingProxy: tracingProxy,
	}, nil
}

func (d *Daemon) Close() {
	if d.codex != nil {
		_ = d.codex.Close()
	}
	if d.agn != nil {
		_ = d.agn.Close()
	}
	if d.claude != nil {
		_ = d.claude.Close()
	}
	if d.tracingProxy != nil {
		d.tracingProxy.Close()
	}
	if d.gatewayConn != nil {
		_ = d.gatewayConn.Close()
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

	go d.runKeepalive(ctx)

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
	d.processing.Store(true)
	defer func() {
		d.processing.Store(false)
		d.syncMu.Unlock()
	}()

	return d.consumer.Sync(ctx, d.cfg.AgentID.String(), d.cfg.ThreadID, func(message platform.Message) error {
		if d.tracingProxy != nil {
			d.tracingProxy.SetMessageID(message.ID)
			defer d.tracingProxy.ClearMessageID()
		}
		return d.handleMessage(ctx, message)
	})
}

func (d *Daemon) handleMessage(ctx context.Context, message platform.Message) error {
	switch d.sdk {
	case SDKCodex:
		return d.handleCodexMessage(ctx, message)
	case SDKAgn:
		return d.handleAgnMessage(ctx, message)
	case SDKClaude:
		return d.handleClaudeMessage(ctx, message)
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
	codexThreadID, err := d.ensureCodexThread(ctx, threadID)
	if err != nil {
		return err
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

func (d *Daemon) ensureCodexThread(ctx context.Context, platformThreadID string) (string, error) {
	if d.mappingStore == nil {
		return "", fmt.Errorf("codex mapping store is not configured")
	}
	if record, ok := d.mapping.RecordForPlatform(platformThreadID); ok {
		updated := record
		updated.LastUsedAtUnixMs = time.Now().UnixMilli()
		d.mapping.SetRecord(updated)
		if err := d.mappingStore.Save(updated); err != nil {
			return "", err
		}
		return record.CodexThreadID, nil
	}
	if err := waitForMCPServers(ctx, d.cfg.MCPServers, mcpReadyTimeout); err != nil {
		return "", err
	}
	record, ok, err := d.mappingStore.Load(platformThreadID)
	if err != nil {
		return "", err
	}
	if ok {
		if err := d.resumeCodexThread(ctx, record.CodexThreadID); err != nil {
			return "", err
		}
		record.LastUsedAtUnixMs = time.Now().UnixMilli()
		d.mapping.SetRecord(record)
		if err := d.mappingStore.Save(record); err != nil {
			return "", err
		}
		return record.CodexThreadID, nil
	}
	codexThreadID, err := d.startCodexThread(ctx)
	if err != nil {
		return "", err
	}
	now := time.Now().UnixMilli()
	newRecord := codexbridge.ThreadMappingRecord{
		PlatformThreadID: platformThreadID,
		CodexThreadID:    codexThreadID,
		CreatedAtUnixMs:  now,
		LastUsedAtUnixMs: now,
	}
	d.mapping.SetRecord(newRecord)
	if err := d.mappingStore.Save(newRecord); err != nil {
		return "", err
	}
	return codexThreadID, nil
}

func waitForMCPServers(ctx context.Context, servers []config.MCPServer, timeout time.Duration) error {
	if len(servers) == 0 {
		return nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for _, server := range servers {
		addr := fmt.Sprintf("localhost:%d", server.Port)
		attempt := 0
		for {
			attempt++
			conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
			if err == nil {
				_ = conn.Close()
				break
			}
			log.Printf("waiting for MCP server %s at %s (attempt %d): %v", server.Name, addr, attempt, err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("MCP server %s at %s not ready after %s", server.Name, addr, timeout)
			case <-time.After(2 * time.Second):
			}
		}
	}
	return nil
}

type codexThreadDefaults struct {
	model                 *string
	baseInstructions      *string
	developerInstructions *string
	cwd                   *string
}

func (d *Daemon) codexThreadDefaults() codexThreadDefaults {
	defaults := codexThreadDefaults{}
	if model := strings.TrimSpace(d.agent.GetModel()); model != "" {
		defaults.model = &model
	}
	if role := strings.TrimSpace(d.agent.GetRole()); role != "" {
		defaults.baseInstructions = &role
	}
	if config := strings.TrimSpace(d.agent.GetConfiguration()); config != "" {
		defaults.developerInstructions = &config
	}
	if d.cfg.WorkDir != "" {
		workDir := d.cfg.WorkDir
		defaults.cwd = &workDir
	}
	return defaults
}

func (d *Daemon) resumeCodexThread(ctx context.Context, codexThreadID string) error {
	params := &codex.ThreadResumeParams{ThreadID: codexThreadID}
	defaults := d.codexThreadDefaults()
	params.Model = defaults.model
	params.BaseInstructions = defaults.baseInstructions
	params.DeveloperInstructions = defaults.developerInstructions
	params.Cwd = defaults.cwd
	resp, err := d.codex.ResumeThread(ctx, params)
	if err != nil {
		return fmt.Errorf("resume codex thread: %w", err)
	}
	threadID := strings.TrimSpace(resp.Thread.ID)
	if threadID == "" {
		return fmt.Errorf("resume codex thread id missing")
	}
	if threadID != codexThreadID {
		return fmt.Errorf("resume codex thread id mismatch")
	}
	return nil
}

func (d *Daemon) startCodexThread(ctx context.Context) (string, error) {
	params := &codex.ThreadStartParams{}
	defaults := d.codexThreadDefaults()
	params.Model = defaults.model
	params.BaseInstructions = defaults.baseInstructions
	params.DeveloperInstructions = defaults.developerInstructions
	params.Cwd = defaults.cwd
	ephemeral := false
	params.Ephemeral = &ephemeral
	resp, err := d.codex.StartThread(ctx, params)
	if err != nil {
		return "", fmt.Errorf("start codex thread: %w", err)
	}
	return resp.Thread.ID, nil
}

func buildInput(message platform.Message) (string, error) {
	text := strings.TrimSpace(message.Body)
	if len(message.FileIDs) > 0 {
		lines := make([]string, 0, len(message.FileIDs)+1)
		if text != "" {
			lines = append(lines, text)
		}
		for _, fileID := range message.FileIDs {
			lines = append(lines, fmt.Sprintf("agyn://file/%s", fileID))
		}
		text = strings.Join(lines, "\n")
	}
	if text == "" {
		return "", fmt.Errorf("message %s has no content", message.ID)
	}
	return text, nil
}
