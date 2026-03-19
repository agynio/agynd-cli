package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var errClientClosed = errors.New("codex client closed")

type Client struct {
	transport           *jsonlTransport
	cmd                 *exec.Cmd
	stdin               io.WriteCloser
	stderrBuf           *bytes.Buffer
	notificationHandler NotificationHandler
	approvalHandler     ApprovalHandler
	initResult          *InitializeResponse
	ctx                 context.Context
	cancel              context.CancelFunc

	mu         sync.Mutex
	pending    map[int64]chan rpcResponse
	processErr error

	closeOnce sync.Once
	done      chan struct{}

	nextID int64
}

type rpcMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type rpcResponse struct {
	result json.RawMessage
	err    *RPCError
}

func NewClient(ctx context.Context, opts ...Option) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	options := clientOptions{
		binary:              "codex",
		args:                []string{"app-server"},
		clientInfo:          ClientInfo{Name: "agynd", Version: "dev"},
		notificationHandler: NopNotificationHandler{},
		approvalHandler:     AutoApprovalHandler{},
	}
	for _, opt := range opts {
		opt(&options)
	}
	if options.clientInfo.Name == "" || options.clientInfo.Version == "" {
		return nil, fmt.Errorf("client info must include name and version")
	}
	if len(options.args) == 0 {
		options.args = []string{"app-server"}
	}
	env, err := buildEnv(options.env)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(options.binary, options.args...)
	cmd.Env = env
	if options.workDir != "" {
		cmd.Dir = options.workDir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open stderr: %w", err)
	}

	clientCtx, cancel := context.WithCancel(context.Background())
	transport := newJSONLTransport(stdout, stdin)
	client := &Client{
		transport:           transport,
		cmd:                 cmd,
		stdin:               stdin,
		stderrBuf:           &bytes.Buffer{},
		notificationHandler: options.notificationHandler,
		approvalHandler:     options.approvalHandler,
		pending:             make(map[int64]chan rpcResponse),
		done:                make(chan struct{}),
		ctx:                 clientCtx,
		cancel:              cancel,
	}

	if client.notificationHandler == nil {
		client.notificationHandler = NopNotificationHandler{}
	}
	if client.approvalHandler == nil {
		client.approvalHandler = AutoApprovalHandler{}
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	go client.captureStderr(stderr, options.stderrWriter)
	go client.readLoop()
	go client.waitLoop()

	initParams := InitializeParams{ClientInfo: options.clientInfo}
	if options.experimentalAPI || len(options.optOutNotifications) > 0 {
		initParams.Capabilities = &InitializeCapabilities{
			ExperimentalAPI:           options.experimentalAPI,
			OptOutNotificationMethods: options.optOutNotifications,
		}
	}
	var initResp InitializeResponse
	if err := client.Request(ctx, "initialize", &initParams, &initResp); err != nil {
		_ = client.Close()
		return nil, err
	}
	client.initResult = &initResp
	if err := client.sendNotification("initialized", nil); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) InitializeResult() *InitializeResponse {
	return c.initResult
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
	})

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-c.done:
	case <-timer.C:
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		<-c.done
	}

	if c.processErr != nil {
		return c.processErr
	}
	return nil
}

func (c *Client) Request(ctx context.Context, method string, params any, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.checkClosed(); err != nil {
		return err
	}
	requestID := atomic.AddInt64(&c.nextID, 1)
	responseCh := make(chan rpcResponse, 1)

	c.mu.Lock()
	c.pending[requestID] = responseCh
	c.mu.Unlock()

	msg := struct {
		Method string `json:"method"`
		ID     int64  `json:"id"`
		Params any    `json:"params,omitempty"`
	}{
		Method: method,
		ID:     requestID,
		Params: params,
	}

	if err := c.transport.WriteMessage(msg); err != nil {
		c.removePending(requestID)
		return err
	}

	select {
	case resp := <-responseCh:
		if resp.err != nil {
			return resp.err
		}
		if result == nil {
			return nil
		}
		if len(resp.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.result, result); err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		c.removePending(requestID)
		return ctx.Err()
	case <-c.done:
		if c.processErr != nil {
			return c.processErr
		}
		return errClientClosed
	}
}

func (c *Client) sendNotification(method string, params any) error {
	if params == nil {
		payload := struct {
			Method string `json:"method"`
		}{Method: method}
		return c.transport.WriteMessage(payload)
	}
	payload := struct {
		Method string `json:"method"`
		Params any    `json:"params"`
	}{
		Method: method,
		Params: params,
	}
	return c.transport.WriteMessage(payload)
}

func (c *Client) readLoop() {
	for {
		line, err := c.transport.ReadMessage()
		if err != nil {
			return
		}
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		switch {
		case msg.ID != nil && msg.Method != "":
			c.handleServerRequest(msg)
		case msg.ID != nil:
			c.handleResponse(msg)
		case msg.Method != "":
			c.handleNotification(msg)
		}
	}
}

func (c *Client) waitLoop() {
	err := c.cmd.Wait()
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			c.setProcessError(&ProcessError{ExitCode: exitErr.ExitCode(), Stderr: strings.TrimSpace(c.stderrBuf.String())})
		} else {
			c.setProcessError(err)
		}
	} else {
		c.setProcessError(nil)
	}
}

func (c *Client) setProcessError(err error) {
	c.mu.Lock()
	if err != nil {
		c.processErr = err
	}
	errToSend := err
	if errToSend == nil {
		errToSend = errClientClosed
	}
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- rpcResponse{err: &RPCError{Code: -32000, Message: errToSend.Error()}}
	}
	c.mu.Unlock()
	close(c.done)
	c.cancel()
}

func (c *Client) handleResponse(msg rpcMessage) {
	if msg.ID == nil {
		return
	}
	c.mu.Lock()
	responseCh := c.pending[*msg.ID]
	if responseCh != nil {
		delete(c.pending, *msg.ID)
	}
	c.mu.Unlock()
	if responseCh == nil {
		return
	}
	responseCh <- rpcResponse{result: msg.Result, err: msg.Error}
}

func (c *Client) handleNotification(msg rpcMessage) {
	if c.notificationHandler == nil {
		return
	}
	params := msg.Params
	switch msg.Method {
	case "turn/started":
		var payload TurnStartedNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnTurnStarted(&payload)
			return
		}
	case "turn/completed":
		var payload TurnCompletedNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnTurnCompleted(&payload)
			return
		}
	case "item/started":
		var payload ItemStartedNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnItemStarted(&payload)
			return
		}
	case "item/completed":
		var payload ItemCompletedNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnItemCompleted(&payload)
			return
		}
	case "item/agentMessage/delta":
		var payload AgentMessageDeltaNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnAgentMessageDelta(&payload)
			return
		}
	case "item/commandExecution/outputDelta":
		var payload CommandExecutionOutputDeltaNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnCommandOutputDelta(&payload)
			return
		}
	case "item/fileChange/outputDelta":
		var payload FileChangeOutputDeltaNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnFileChangeDelta(&payload)
			return
		}
	case "thread/tokenUsage/updated":
		var payload ThreadTokenUsageUpdatedNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnTokenUsageUpdated(&payload)
			return
		}
	case "error":
		var payload ErrorNotification
		if err := json.Unmarshal(params, &payload); err == nil {
			c.notificationHandler.OnError(&payload)
			return
		}
	}
	c.notificationHandler.OnNotification(msg.Method, params)
}

func (c *Client) handleServerRequest(msg rpcMessage) {
	if msg.ID == nil {
		return
	}
	if c.approvalHandler == nil {
		c.approvalHandler = AutoApprovalHandler{}
	}
	var (
		result any
		err    error
	)
	switch msg.Method {
	case "item/commandExecution/requestApproval":
		var params CommandExecutionRequestApprovalParams
		err = json.Unmarshal(msg.Params, &params)
		if err == nil {
			result, err = c.approvalHandler.OnCommandApproval(c.ctx, &params)
		}
	case "item/fileChange/requestApproval":
		var params FileChangeRequestApprovalParams
		err = json.Unmarshal(msg.Params, &params)
		if err == nil {
			result, err = c.approvalHandler.OnFileChangeApproval(c.ctx, &params)
		}
	case "item/permissions/requestApproval":
		var params PermissionsRequestApprovalParams
		err = json.Unmarshal(msg.Params, &params)
		if err == nil {
			result, err = c.approvalHandler.OnPermissionsApproval(c.ctx, &params)
		}
	case "item/tool/requestUserInput":
		var params ToolRequestUserInputParams
		err = json.Unmarshal(msg.Params, &params)
		if err == nil {
			result, err = c.approvalHandler.OnToolUserInput(c.ctx, &params)
		}
	case "item/tool/call":
		var params DynamicToolCallParams
		err = json.Unmarshal(msg.Params, &params)
		if err == nil {
			result, err = c.approvalHandler.OnDynamicToolCall(c.ctx, &params)
		}
	default:
		err = fmt.Errorf("unsupported server request: %s", msg.Method)
	}
	if err != nil {
		_ = c.sendErrorResponse(*msg.ID, err)
		return
	}
	_ = c.sendResultResponse(*msg.ID, result)
}

func (c *Client) sendResultResponse(id int64, result any) error {
	return c.transport.WriteMessage(struct {
		ID     int64 `json:"id"`
		Result any   `json:"result"`
	}{
		ID:     id,
		Result: result,
	})
}

func (c *Client) sendErrorResponse(id int64, err error) error {
	rpcErr := &RPCError{Code: -32000, Message: err.Error()}
	return c.transport.WriteMessage(struct {
		ID    int64     `json:"id"`
		Error *RPCError `json:"error"`
	}{
		ID:    id,
		Error: rpcErr,
	})
}

func (c *Client) captureStderr(r io.Reader, extra io.Writer) {
	if extra != nil {
		_, _ = io.Copy(io.MultiWriter(c.stderrBuf, extra), r)
		return
	}
	_, _ = io.Copy(c.stderrBuf, r)
}

func (c *Client) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) checkClosed() error {
	select {
	case <-c.done:
		if c.processErr != nil {
			return c.processErr
		}
		return errClientClosed
	default:
		return nil
	}
}

func buildEnv(overrides map[string]string) ([]string, error) {
	envMap := map[string]string{}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	for key, value := range overrides {
		envMap[key] = value
	}
	required := []string{"OPENAI_API_KEY", "PATH", "HOME"}
	for _, key := range required {
		if envMap[key] == "" {
			return nil, fmt.Errorf("%s must be set", key)
		}
	}
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+envMap[key])
	}
	return env, nil
}
