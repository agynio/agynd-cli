package daemon

import (
	"context"
	"fmt"
	"strings"

	agnsdk "github.com/agynio/agn-sdk-go"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/agynio/agynd-cli/internal/subscriber"
	"github.com/agynio/agynd-cli/internal/tracing"
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

	if err := runInitScripts(ctx, setup.agents, cfg.AgentID.String(), cfg.EnvironmentID, cfg.WorkDir); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	if err := waitForMCPServers(ctx, cfg.MCPServers, mcpReadyTimeout); err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	// What the platform hands the agent CLI is exported here; what the CLI
	// did with it is exported by the trace hook, into the trace both derive
	// from the workload.
	tracingExporter, err := tracing.NewExporter(tracing.Config{
		Address:    cfg.TracingAddress,
		WorkloadID: cfg.WorkloadID,
	})
	if err != nil {
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	// agn reports its own turns rather than keeping a transcript to read back,
	// so its spans arrive over OTLP and need something listening for them. The
	// hook the other CLIs use never runs here, and the daemon exports only the
	// invocation message -- without this the model call is never exported.
	tracingProxy, err := tracingproxy.Start(ctx, tracingproxy.Config{
		TracingAddress: cfg.TracingAddress,
		ThreadID:       cfg.ThreadID,
		WorkloadID:     cfg.WorkloadID,
	})
	if err != nil {
		_ = tracingExporter.Close()
		_ = setup.gatewayConn.Close()
		return nil, err
	}

	agnClient, err := agnsdk.Start(ctx, agnsdk.Options{
		BinaryPath: cfg.AgentBinary,
		Env: []string{
			"PATH=" + agentPathValue(),
			"AGN_CONFIG_PATH=" + configPath,
			// Its own listener rather than whatever the workload was handed:
			// the proxy stamps the thread and workload onto what it forwards.
			"OTEL_EXPORTER_OTLP_ENDPOINT=http://" + tracingProxy.Address(),
			// Handed the same trace the hook is given for the other CLIs.
			// Without it agn rooted its spans itself, and the message and the
			// model call it caused landed in separate traces.
			traceparentEnv + "=" + traceparentFor(cfg.WorkloadID),
		},
	})
	if err != nil {
		tracingProxy.Close()
		_ = tracingExporter.Close()
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
		tracing:      tracingExporter,
		tracingProxy: tracingProxy,
		mcpReady:     true,
	}, nil
}

func (d *Daemon) handleAgnMessage(ctx context.Context, message platform.Message) error {
	threadID := strings.TrimSpace(message.ThreadID)
	inputText, err := buildInput(message)
	if err != nil {
		return err
	}
	// The instance, not the thread. Agent state is instance-scoped -- an
	// instance serves many threads and they share one conversation -- and
	// keying it by thread meant a reply arriving on another thread began a
	// conversation with no memory of the one that started it. Which thread a
	// message came from is in its header.
	result, err := d.agn.Turn(ctx, agnsdk.TurnParams{
		Prompt:   inputText,
		ThreadID: d.selfID(),
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
