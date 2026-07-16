package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
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
	pageSize                  int32 = 100
	pageTimeout                     = 30 * time.Second
	turnStartTimeout                = 5 * time.Minute
	messagePublishTimeout           = 15 * time.Second
	messageAckTimeout               = 15 * time.Second
	mcpReadyTimeout                 = 4 * time.Minute
	llmReadyTimeout                 = 120 * time.Second
	codexReadbackTimeout            = 10 * time.Second
	codexReadbackPollInterval       = 250 * time.Millisecond
	syncRetryInitialBackoff         = 1 * time.Second
	syncRetryMaxBackoff             = 30 * time.Second
	opSyncPageFetch                 = "sync_page_fetch"
	opCodexStartTurn                = "codex_start_turn"
	opMessagePublish                = "publish"
	opMessageAck                    = "ack"
	opKeepaliveTouch                = "keepalive_touch"
	opCodexWaitTurnCompletion       = "codex_wait_turn_completion"
	opCodexTurnResult               = "codex_turn_result"
	opAgnTurn                       = "agn_turn"
	opClaudeTurn                    = "claude_turn"
	opProcessSignalShutdown         = "process_signal/shutdown"
	tracingProxyListenAddress       = "127.0.0.1:0"
	mcpLoopbackHost                 = "127.0.0.1"
)

const (
	SDKCodex  = "codex"
	SDKAgn    = "agn"
	SDKClaude = "claude"
)

const mcpProbeID = "agynd-mcp-ready"

type Daemon struct {
	cfg           config.Config
	sdk           string
	gatewayConn   platformConn
	threads       *platform.Threads
	agents        gatewayv1.AgentsGatewayClient
	runners       runnersClient
	subscriber    messageSubscriber
	consumer      messageConsumer
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
	mcpReadyMu    sync.Mutex
	mcpReady      bool

	processing     atomic.Bool
	processingWake chan struct{}
	syncMu         sync.Mutex
}

type platformConn interface {
	Close() error
}

type codexClient interface {
	StartThread(ctx context.Context, params *codex.ThreadStartParams) (*codex.ThreadStartResponse, error)
	ResumeThread(ctx context.Context, params *codex.ThreadResumeParams) (*codex.ThreadResumeResponse, error)
	ReadThread(ctx context.Context, params *codex.ThreadReadParams) (*codex.ThreadReadResponse, error)
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

type messageSubscriber interface {
	Run(ctx context.Context) error
	Started() <-chan struct{}
	Wake() <-chan struct{}
}

type messageConsumer interface {
	Sync(ctx context.Context, participantID string, threadID string, handle func(platform.Message) error) error
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
	if cfg.Mode == config.ModeHolder {
		return newHolderDaemon(cfg), nil
	}
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

func newHolderDaemon(cfg config.Config) *Daemon {
	return &Daemon{
		cfg: cfg,
		sdk: config.ModeHolder,
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
	codexHome, err := writeCodexConfig(cfg.LLMBaseURL, cfg.MCPServers, otlpEndpoint)
	if err != nil {
		tracingProxy.Close()
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	if err := runInitScripts(ctx, setup.agents, cfg.AgentID.String(), cfg.WorkDir); err != nil {
		tracingProxy.Close()
		_ = setup.gatewayConn.Close()
		return nil, err
	}
	codexHomeValue := codexHomeEnv()
	mappingStore := codexbridge.NewThreadMappingStore(codexHomeValue)
	options := []codex.Option{
		codex.WithBinary(cfg.AgentBinary),
		codex.WithWorkDir(cfg.WorkDir),
		codex.WithEnv(codexEnv(cfg, codexHome, codexHomeValue, otlpEndpoint)),
		codex.WithNotificationHandler(bridge),
		codex.WithApprovalHandler(codex.AutoApprovalHandler{}),
		codex.WithClientInfo("agynd", version),
	}
	codexClient, err := newCodexClient(ctx, cfg, options...)
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
		subscriber:   subscriber.New(setup.notifications, cfg.ThreadID),
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
	if d.sdk == config.ModeHolder {
		return runHolder(ctx, d.cfg.WorkDir)
	}
	d.ensureProcessingWake()
	go d.runKeepalive(ctx)

	go func() {
		if err := d.subscriber.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("subscriber stopped: %v", err)
		}
	}()
	select {
	case <-ctx.Done():
		return operationError(opProcessSignalShutdown, 0, ctx.Err())
	case <-d.subscriber.Started():
	}

	backoff := syncRetryInitialBackoff
	var retryTimer *time.Timer
	var retry <-chan time.Time
	defer func() {
		if retryTimer != nil {
			retryTimer.Stop()
		}
	}()
	scheduleRetry := func(delay time.Duration) {
		if retryTimer == nil {
			retryTimer = time.NewTimer(delay)
		} else {
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retryTimer.Reset(delay)
		}
		retry = retryTimer.C
	}
	clearRetry := func() {
		if retryTimer != nil {
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
		}
		retry = nil
		backoff = syncRetryInitialBackoff
	}
	scheduleFailureRetry := func(operation string, err error) {
		delay := backoff
		log.Printf("%s failed: %v; retrying in %s", operation, err, delay)
		backoff = nextSyncRetryBackoff(backoff)
		scheduleRetry(delay)
	}
	handleSyncFailure := func(operation string, err error) error {
		if isTerminalAgentProcessingError(err) {
			log.Printf("%s terminal agent processing failure: %v", operation, err)
			return err
		}
		scheduleFailureRetry(operation, err)
		return nil
	}

	if err := d.syncMessages(ctx); err != nil {
		if terminalErr := handleSyncFailure("initial sync messages", err); terminalErr != nil {
			return terminalErr
		}
	} else {
		clearRetry()
	}

	for {
		select {
		case <-ctx.Done():
			return operationError(opProcessSignalShutdown, 0, ctx.Err())
		case <-d.subscriber.Wake():
			if err := d.syncMessages(ctx); err != nil {
				if terminalErr := handleSyncFailure("sync messages", err); terminalErr != nil {
					return terminalErr
				}
			} else {
				clearRetry()
			}
		case <-retry:
			if err := d.syncMessages(ctx); err != nil {
				if terminalErr := handleSyncFailure("sync messages retry", err); terminalErr != nil {
					return terminalErr
				}
			} else {
				clearRetry()
			}
		}
	}
}

type operationFailure struct {
	op      string
	timeout time.Duration
	err     error
}

type terminalCodexTurnError struct {
	err error
}

func (e *terminalCodexTurnError) Error() string {
	return e.err.Error()
}

func (e *terminalCodexTurnError) Unwrap() error {
	return e.err
}

func terminalCodexTurn(err error) error {
	return &terminalCodexTurnError{err: err}
}

func (e *operationFailure) Error() string {
	if errors.Is(e.err, context.DeadlineExceeded) {
		if e.timeout > 0 {
			return fmt.Sprintf("%s timed out after %s: %v", e.op, e.timeout, e.err)
		}
		return fmt.Sprintf("%s timed out: %v", e.op, e.err)
	}
	if errors.Is(e.err, context.Canceled) {
		return fmt.Sprintf("%s canceled: %v", e.op, e.err)
	}
	if e.timeout > 0 {
		return fmt.Sprintf("%s failed (timeout %s): %v", e.op, e.timeout, e.err)
	}
	return fmt.Sprintf("%s failed: %v", e.op, e.err)
}

func (e *operationFailure) Unwrap() error {
	return e.err
}

func operationError(op string, timeout time.Duration, err error) error {
	if err == nil {
		return nil
	}
	return &operationFailure{op: op, timeout: timeout, err: err}
}

func isTerminalAgentProcessingError(err error) bool {
	var terminalErr *terminalCodexTurnError
	return errors.As(err, &terminalErr)
}

func operationContextErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
		return ctxErr
	}
	return err
}

func (d *Daemon) publishResponse(ctx context.Context, sdk string, threadID string, message platform.Message, response string) error {
	publishCtx, cancel := context.WithTimeout(ctx, messagePublishTimeout)
	_, err := d.threads.SendMessage(publishCtx, threadID, d.cfg.AgentID.String(), response, nil)
	err = operationContextErr(publishCtx, err)
	cancel()
	if err != nil {
		return operationError(
			opMessagePublish,
			messagePublishTimeout,
			fmt.Errorf("publish %s response for message %s on thread %s: %w", sdk, message.ID, threadID, err),
		)
	}
	return nil
}

func (d *Daemon) ackMessage(ctx context.Context, message platform.Message) error {
	ackCtx, cancel := context.WithTimeout(ctx, messageAckTimeout)
	err := d.threads.AckMessages(ackCtx, d.cfg.AgentID.String(), []string{message.ID})
	err = operationContextErr(ackCtx, err)
	cancel()
	if err != nil {
		return operationError(
			opMessageAck,
			messageAckTimeout,
			fmt.Errorf("ack message %s: %w", message.ID, err),
		)
	}
	return nil
}

func (d *Daemon) ensureProcessingWake() {
	if d.processingWake == nil {
		d.processingWake = make(chan struct{}, 1)
	}
}

func (d *Daemon) signalProcessingStarted() {
	if d.processingWake == nil {
		return
	}
	select {
	case d.processingWake <- struct{}{}:
	default:
	}
}

func nextSyncRetryBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return syncRetryInitialBackoff
	}
	next := current * 2
	if next > syncRetryMaxBackoff {
		return syncRetryMaxBackoff
	}
	return next
}

func (d *Daemon) syncMessages(ctx context.Context) error {
	d.syncMu.Lock()
	d.processing.Store(true)
	d.signalProcessingStarted()
	defer func() {
		d.processing.Store(false)
		d.syncMu.Unlock()
	}()

	if err := d.consumer.Sync(ctx, d.cfg.AgentID.String(), d.cfg.ThreadID, func(message platform.Message) error {
		if d.tracingProxy != nil {
			d.tracingProxy.SetMessageID(message.ID)
		}
		if err := d.ensureMCPReady(ctx); err != nil {
			return fmt.Errorf("wait for MCP servers before processing message %s: %w", message.ID, err)
		}
		return d.handleMessage(ctx, message)
	}); err != nil {
		var pageFetchErr *platform.PageFetchError
		if !errors.As(err, &pageFetchErr) {
			return err
		}
		return operationError(
			opSyncPageFetch,
			pageTimeout,
			fmt.Errorf("sync unacked messages for participant %s thread %s: %w", d.cfg.AgentID.String(), d.cfg.ThreadID, pageFetchErr),
		)
	}
	return nil
}

func (d *Daemon) ensureMCPReady(ctx context.Context) error {
	d.mcpReadyMu.Lock()
	defer d.mcpReadyMu.Unlock()
	if d.mcpReady {
		return nil
	}
	if err := waitForMCPServers(ctx, d.cfg.MCPServers, mcpReadyTimeout); err != nil {
		return err
	}
	d.mcpReady = true
	return nil
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
	if err := waitForZitiLLMService(ctx, d.cfg.LLMBaseURL, llmReadyTimeout); err != nil {
		return fmt.Errorf("wait for LLM service before codex turn: %w", err)
	}
	turnCtx, cancel := context.WithTimeout(ctx, turnStartTimeout)
	turnResp, err := d.codex.StartTurn(turnCtx, &codex.TurnStartParams{
		ThreadID: codexThreadID,
		Input:    []codex.UserInput{codex.NewTextUserInput(inputText)},
	})
	err = operationContextErr(turnCtx, err)
	cancel()
	if err != nil {
		return operationError(opCodexStartTurn, turnStartTimeout, fmt.Errorf("start codex turn for message %s on thread %s: %w", message.ID, codexThreadID, err))
	}
	turnID := strings.TrimSpace(turnResp.Turn.ID)
	if turnID == "" {
		return fmt.Errorf("codex turn id missing")
	}
	completionCh := d.tracker.Register(turnID)
	select {
	case result := <-completionCh:
		if result.ThreadID != "" && result.ThreadID != codexThreadID {
			return operationError(
				opCodexTurnResult,
				0,
				terminalCodexTurn(fmt.Errorf("codex turn %s thread mismatch for message %s", turnID, message.ID)),
			)
		}
		if result.Err != nil {
			if !errors.Is(result.Err, codexbridge.ErrMissingAgentMessage) {
				return operationError(
					opCodexTurnResult,
					0,
					terminalCodexTurn(fmt.Errorf("codex turn %s failed for message %s: %w", turnID, message.ID, result.Err)),
				)
			}
			readbackMessage, readbackErr := d.readCodexTurnMessage(ctx, codexThreadID, turnID)
			if readbackErr != nil {
				turnErr := fmt.Errorf("codex turn %s failed for message %s: %w; readback failed: %w", turnID, message.ID, result.Err, readbackErr)
				if errors.Is(readbackErr, codexbridge.ErrMissingAgentMessage) {
					turnErr = terminalCodexTurn(turnErr)
				}
				return operationError(
					opCodexTurnResult,
					0,
					turnErr,
				)
			}
			result.Message = readbackMessage
		}
		if strings.TrimSpace(result.Message) == "" {
			return operationError(
				opCodexTurnResult,
				0,
				terminalCodexTurn(fmt.Errorf("codex turn %s completed with empty response for message %s", turnID, message.ID)),
			)
		}
		if err := d.publishResponse(ctx, SDKCodex, threadID, message, result.Message); err != nil {
			return err
		}
		if err := d.ackMessage(ctx, message); err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		d.tracker.Cancel(turnID)
		return operationError(
			opCodexWaitTurnCompletion,
			0,
			fmt.Errorf("wait for codex turn %s completion for message %s: %w", turnID, message.ID, ctx.Err()),
		)
	}
}

func (d *Daemon) readCodexTurnMessage(ctx context.Context, codexThreadID, turnID string) (string, error) {
	readbackCtx, cancel := context.WithTimeout(ctx, codexReadbackTimeout)
	defer cancel()

	ticker := time.NewTicker(codexReadbackPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		message, err := d.readCodexTurnMessageOnce(readbackCtx, codexThreadID, turnID)
		if err == nil {
			return message, nil
		}
		if !isRetryableCodexReadbackError(err) {
			return "", err
		}
		lastErr = err

		select {
		case <-readbackCtx.Done():
			return "", fmt.Errorf("read codex turn %s after completion timed out after %s: %w", turnID, codexReadbackTimeout, lastErr)
		case <-ticker.C:
		}
	}
}

func isRetryableCodexReadbackError(err error) bool {
	return errors.Is(err, codexbridge.ErrMissingAgentMessage)
}

func (d *Daemon) readCodexTurnMessageOnce(ctx context.Context, codexThreadID, turnID string) (string, error) {
	threadResp, err := d.codex.ReadThread(ctx, &codex.ThreadReadParams{
		ThreadID:     codexThreadID,
		IncludeTurns: true,
	})
	if err != nil {
		return "", fmt.Errorf("read codex thread %s: %w", codexThreadID, err)
	}
	for _, turn := range threadResp.Thread.Turns {
		if turn.ID != turnID {
			continue
		}
		message, err := codexbridge.ExtractFinalAnswer(turn)
		if err != nil {
			return "", err
		}
		return message, nil
	}
	return "", fmt.Errorf("codex turn %s not found in thread %s", turnID, codexThreadID)
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
			return "", fmt.Errorf("save codex thread mapping for platform thread %s: %w", platformThreadID, err)
		}
		return record.CodexThreadID, nil
	}
	record, ok, err := d.mappingStore.Load(platformThreadID)
	if err != nil {
		return "", fmt.Errorf("load codex thread mapping for platform thread %s: %w", platformThreadID, err)
	}
	if ok {
		if err := d.resumeCodexThread(ctx, record.CodexThreadID); err != nil {
			return "", err
		}
		record.LastUsedAtUnixMs = time.Now().UnixMilli()
		d.mapping.SetRecord(record)
		if err := d.mappingStore.Save(record); err != nil {
			return "", fmt.Errorf("save codex thread mapping for platform thread %s: %w", platformThreadID, err)
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
		return "", fmt.Errorf("save codex thread mapping for platform thread %s: %w", platformThreadID, err)
	}
	return codexThreadID, nil
}

func waitForZitiLLMService(ctx context.Context, llmBaseURL string, timeout time.Duration) error {
	addr, ok, err := zitiLLMServiceAddress(llmBaseURL)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return waitForTCPService(ctx, addr, timeout, "LLM service")
}

func zitiLLMServiceAddress(llmBaseURL string) (string, bool, error) {
	if !isZitiLLMBaseURL(llmBaseURL) {
		return "", false, nil
	}
	parsed, err := url.Parse(strings.TrimSpace(llmBaseURL))
	if err != nil {
		return "", false, fmt.Errorf("parse LLM base URL: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", false, fmt.Errorf("LLM base URL missing host")
	}
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", false, fmt.Errorf("LLM base URL scheme %q has no default port", parsed.Scheme)
		}
	}
	return net.JoinHostPort(host, port), true, nil
}

type tcpServiceTarget struct {
	label string
	addr  string
}

type mcpServiceTarget struct {
	label string
	addr  string
	url   string
}

func waitForTCPService(ctx context.Context, addr string, timeout time.Duration, label string) error {
	return waitForTCPServices(ctx, []tcpServiceTarget{{label: label, addr: addr}}, timeout)
}

func waitForTCPServices(ctx context.Context, targets []tcpServiceTarget, timeout time.Duration) error {
	if len(targets) == 0 {
		return nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for _, target := range targets {
		attempt := 0
		for {
			attempt++
			conn, err := net.DialTimeout("tcp", target.addr, 1*time.Second)
			if err == nil {
				_ = conn.Close()
				log.Printf("%s at %s ready after %d attempt(s)", target.label, target.addr, attempt)
				break
			}
			log.Printf("waiting for %s at %s (attempt %d): %v", target.label, target.addr, attempt, err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("%s at %s not ready after %s", target.label, target.addr, timeout)
			case <-time.After(2 * time.Second):
			}
		}
	}
	return nil
}

func waitForMCPServers(ctx context.Context, servers []config.MCPServer, timeout time.Duration) error {
	targets := make([]mcpServiceTarget, 0, len(servers))
	for _, server := range servers {
		addr := net.JoinHostPort(mcpLoopbackHost, fmt.Sprintf("%d", server.Port))
		targets = append(targets, mcpServiceTarget{
			label: fmt.Sprintf("MCP server %s", server.Name),
			addr:  addr,
			url:   mcpEndpoint(server.Port),
		})
	}
	return waitForMCPServices(ctx, targets, timeout)
}

func mcpEndpoint(port int) string {
	return fmt.Sprintf("http://%s/mcp", net.JoinHostPort(mcpLoopbackHost, fmt.Sprintf("%d", port)))
}

func waitForMCPServices(ctx context.Context, targets []mcpServiceTarget, timeout time.Duration) error {
	if len(targets) == 0 {
		return nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	client := &http.Client{Timeout: 10 * time.Second}
	for _, target := range targets {
		attempt := 0
		for {
			attempt++
			if err := probeMCPService(ctx, client, target.url); err == nil {
				log.Printf("%s at %s ready after %d attempt(s)", target.label, target.addr, attempt)
				break
			} else {
				log.Printf("waiting for %s at %s (attempt %d): %v", target.label, target.addr, attempt, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("%s at %s not ready after %s", target.label, target.addr, timeout)
			case <-time.After(2 * time.Second):
			}
		}
	}
	return nil
}

func probeMCPService(ctx context.Context, client *http.Client, endpoint string) error {
	body := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"agynd","version":"mcp-ready"}}}`, mcpProbeID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status %s", resp.Status)
	}
	return readMCPProbeResult(resp.Body)
}

type mcpProbeResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func readMCPProbeResult(body io.Reader) error {
	reader := bufio.NewReader(io.LimitReader(body, 64*1024))
	var raw bytes.Buffer
	var eventData []string
	var lastEventErr error
	sawSSE := false

	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			raw.WriteString(line)
			trimmed := strings.TrimRight(line, "\r\n")
			if data, ok := strings.CutPrefix(trimmed, "data:"); ok {
				sawSSE = true
				eventData = append(eventData, strings.TrimSpace(data))
			} else if sawSSE && strings.TrimSpace(trimmed) == "" {
				if len(eventData) > 0 {
					if err := validateMCPProbePayload([]byte(strings.Join(eventData, "\n"))); err == nil {
						return nil
					} else {
						lastEventErr = err
					}
				}
				eventData = nil
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return err
	}

	if sawSSE {
		if len(eventData) > 0 {
			if err := validateMCPProbePayload([]byte(strings.Join(eventData, "\n"))); err == nil {
				return nil
			} else {
				lastEventErr = err
			}
		}
		if lastEventErr != nil {
			return lastEventErr
		}
		return fmt.Errorf("initialize SSE response missing data")
	}
	return validateMCPProbePayload(raw.Bytes())
}

func validateMCPProbePayload(payload []byte) error {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return fmt.Errorf("initialize response is empty")
	}
	var response mcpProbeResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return fmt.Errorf("parse initialize response: %w", err)
	}
	if response.JSONRPC != "2.0" {
		return fmt.Errorf("initialize response jsonrpc %q does not match 2.0", response.JSONRPC)
	}
	if response.ID != mcpProbeID {
		return fmt.Errorf("initialize response id %q does not match %q", response.ID, mcpProbeID)
	}
	if len(bytes.TrimSpace(response.Error)) > 0 && !bytes.Equal(bytes.TrimSpace(response.Error), []byte("null")) {
		return fmt.Errorf("initialize response contains error")
	}
	result := bytes.TrimSpace(response.Result)
	if len(result) == 0 || bytes.Equal(result, []byte("null")) {
		return fmt.Errorf("initialize response missing result")
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
