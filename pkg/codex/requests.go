package codex

import "context"

func (c *Client) ThreadStart(ctx context.Context, params *ThreadStartParams) (*ThreadStartResponse, error) {
	var resp ThreadStartResponse
	if err := c.Request(ctx, "thread/start", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ThreadResume(ctx context.Context, params *ThreadResumeParams) (*ThreadResumeResponse, error) {
	var resp ThreadResumeResponse
	if err := c.Request(ctx, "thread/resume", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ThreadRead(ctx context.Context, params *ThreadReadParams) (*ThreadReadResponse, error) {
	var resp ThreadReadResponse
	if err := c.Request(ctx, "thread/read", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ThreadList(ctx context.Context, params *ThreadListParams) (*ThreadListResponse, error) {
	var resp ThreadListResponse
	if err := c.Request(ctx, "thread/list", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) TurnStart(ctx context.Context, params *TurnStartParams) (*TurnStartResponse, error) {
	var resp TurnStartResponse
	if err := c.Request(ctx, "turn/start", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) TurnSteer(ctx context.Context, params *TurnSteerParams) (*TurnSteerResponse, error) {
	var resp TurnSteerResponse
	if err := c.Request(ctx, "turn/steer", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) TurnInterrupt(ctx context.Context, params *TurnInterruptParams) (*TurnInterruptResponse, error) {
	var resp TurnInterruptResponse
	if err := c.Request(ctx, "turn/interrupt", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ModelList(ctx context.Context, params *ModelListParams) (*ModelListResponse, error) {
	var resp ModelListResponse
	if err := c.Request(ctx, "model/list", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ConfigRead(ctx context.Context, params *ConfigReadParams) (*ConfigReadResponse, error) {
	var resp ConfigReadResponse
	if err := c.Request(ctx, "config/read", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
