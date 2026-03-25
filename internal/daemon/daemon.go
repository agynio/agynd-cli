package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	agnsdk "github.com/agynio/agn-sdk-go"
	notificationsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/notifications/v1"
	teamsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/teams/v1"
	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"github.com/agynio/agynd-cli/internal/codexbridge"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/agynio/agynd-cli/internal/subscriber"
	"github.com/agynio/agynd-cli/pkg/codex"
)

const pageSize int32 = 100

type Daemon struct {
	cfg           config.Config
	threadsConn   platformConn
	notifsConn    platformConn
	teamsConn     platformConn
	threads       threadsv1.ThreadsServiceClient
	notifications notificationsv1.NotificationsServiceClient
	teams         teamsv1.TeamsServiceClient
	subscriber    *subscriber.Subscriber
	codex         *codex.Client
	mapping       *codexbridge.ThreadMapping
	agn           *agnsdk.Client
	agnDir        string
	sdk           string
	agent         *teamsv1.Agent

	syncMu sync.Mutex
}

type platformConn interface {
	Close() error
}

func New(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	sdk := strings.ToLower(strings.TrimSpace(cfg.SDK))
	if sdk == "" {
		sdk = "codex"
	}
	switch sdk {
	case "codex":
		return newCodexDaemon(ctx, cfg, version)
	case "agn":
		return newAgnDaemon(ctx, cfg, version)
	default:
		return nil, fmt.Errorf("unknown sdk %q", sdk)
	}
}

func newDaemonBase(ctx context.Context, cfg config.Config) (*Daemon, error) {
	threadsConn, err := platform.Dial(ctx, cfg.ThreadsAddress)
	if err != nil {
		return nil, fmt.Errorf("dial threads: %w", err)
	}
	notifsConn, err := platform.Dial(ctx, cfg.NotificationsAddress)
	if err != nil {
		_ = threadsConn.Close()
		return nil, fmt.Errorf("dial notifications: %w", err)
	}
	teamsConn, err := platform.Dial(ctx, cfg.TeamsAddress)
	if err != nil {
		_ = threadsConn.Close()
		_ = notifsConn.Close()
		return nil, fmt.Errorf("dial teams: %w", err)
	}

	threadsClient := threadsv1.NewThreadsServiceClient(threadsConn)
	notificationsClient := notificationsv1.NewNotificationsServiceClient(notifsConn)
	teamsClient := teamsv1.NewTeamsServiceClient(teamsConn)

	agentResp, err := teamsClient.GetAgent(ctx, &teamsv1.GetAgentRequest{Id: cfg.AgentID.String()})
	if err != nil {
		_ = threadsConn.Close()
		_ = notifsConn.Close()
		_ = teamsConn.Close()
		return nil, fmt.Errorf("get agent: %w", err)
	}
	agent := agentResp.GetAgent()
	if agent == nil {
		_ = threadsConn.Close()
		_ = notifsConn.Close()
		_ = teamsConn.Close()
		return nil, fmt.Errorf("agent not found")
	}

	return &Daemon{
		cfg:           cfg,
		threadsConn:   threadsConn,
		notifsConn:    notifsConn,
		teamsConn:     teamsConn,
		threads:       threadsClient,
		notifications: notificationsClient,
		teams:         teamsClient,
		subscriber:    subscriber.New(notificationsClient),
		agent:         agent,
	}, nil
}

func newCodexDaemon(ctx context.Context, cfg config.Config, version string) (*Daemon, error) {
	daemon, err := newDaemonBase(ctx, cfg)
	if err != nil {
		return nil, err
	}

	mapping := codexbridge.NewThreadMapping()
	bridge := codexbridge.New(ctx, daemon.threads, cfg.AgentID.String(), mapping)
	codexClient, err := codex.NewClient(ctx,
		codex.WithBinary(cfg.AgentBinary),
		codex.WithWorkDir(cfg.WorkDir),
		codex.WithNotificationHandler(bridge),
		codex.WithApprovalHandler(codex.AutoApprovalHandler{}),
		codex.WithClientInfo("agynd", version),
	)
	if err != nil {
		daemon.Close()
		return nil, err
	}

	daemon.codex = codexClient
	daemon.mapping = mapping
	daemon.sdk = "codex"
	return daemon, nil
}

func (d *Daemon) Close() {
	if d.codex != nil {
		_ = d.codex.Close()
	}
	if d.agn != nil {
		_ = d.agn.Close()
	}
	if d.agnDir != "" {
		_ = os.RemoveAll(d.agnDir)
	}
	if d.threadsConn != nil {
		_ = d.threadsConn.Close()
	}
	if d.notifsConn != nil {
		_ = d.notifsConn.Close()
	}
	if d.teamsConn != nil {
		_ = d.teamsConn.Close()
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

	token := ""
	for {
		pageCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		resp, err := d.threads.GetUnackedMessages(pageCtx, &threadsv1.GetUnackedMessagesRequest{
			ParticipantId: d.cfg.AgentID.String(),
			PageSize:      pageSize,
			PageToken:     token,
		})
		cancel()
		if err != nil {
			return fmt.Errorf("get unacked messages: %w", err)
		}
		messages := resp.GetMessages()
		if len(messages) > 1 {
			sort.Slice(messages, func(i, j int) bool {
				left := messages[i].GetCreatedAt()
				right := messages[j].GetCreatedAt()
				if left == nil || right == nil {
					return messages[i].GetId() < messages[j].GetId()
				}
				if left.Seconds == right.Seconds {
					return left.Nanos < right.Nanos
				}
				return left.Seconds < right.Seconds
			})
		}
		for _, message := range messages {
			if message == nil {
				continue
			}
			if err := d.handleMessage(ctx, message); err != nil {
				return err
			}
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return nil
		}
	}
}

func (d *Daemon) handleMessage(ctx context.Context, message *threadsv1.Message) error {
	switch d.sdk {
	case "codex":
		return d.handleCodexMessage(ctx, message)
	case "agn":
		return d.handleAgnMessage(ctx, message)
	default:
		return fmt.Errorf("unknown sdk %q", d.sdk)
	}
}

func (d *Daemon) handleCodexMessage(ctx context.Context, message *threadsv1.Message) error {
	threadID := strings.TrimSpace(message.GetThreadId())
	if threadID == "" {
		return fmt.Errorf("message %s missing thread id", message.GetId())
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
	turnCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	_, err = d.codex.TurnStart(turnCtx, &codex.TurnStartParams{
		ThreadID: codexThreadID,
		Input:    []codex.UserInput{codex.NewTextUserInput(inputText)},
	})
	cancel()
	if err != nil {
		return err
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

func (d *Daemon) startCodexThread(ctx context.Context) (string, error) {
	params := &codex.ThreadStartParams{}
	if model := strings.TrimSpace(d.agent.GetModel()); model != "" {
		params.Model = &model
	}
	if name := strings.TrimSpace(d.agent.GetName()); name != "" {
		params.ServiceName = &name
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
	resp, err := d.codex.ThreadStart(ctx, params)
	if err != nil {
		return "", fmt.Errorf("start codex thread: %w", err)
	}
	return resp.Thread.ID, nil
}

func buildInput(message *threadsv1.Message) (string, error) {
	text := strings.TrimSpace(message.GetBody())
	if text == "" && len(message.GetFileIds()) > 0 {
		text = fmt.Sprintf("Received files: %s", strings.Join(message.GetFileIds(), ", "))
	}
	if text == "" {
		return "", fmt.Errorf("message %s has no content", message.GetId())
	}
	return text, nil
}
