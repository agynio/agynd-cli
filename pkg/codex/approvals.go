package codex

import "context"

type ApprovalHandler interface {
	OnCommandApproval(context.Context, *CommandExecutionRequestApprovalParams) (*CommandExecutionRequestApprovalResponse, error)
	OnFileChangeApproval(context.Context, *FileChangeRequestApprovalParams) (*FileChangeRequestApprovalResponse, error)
	OnPermissionsApproval(context.Context, *PermissionsRequestApprovalParams) (*PermissionsRequestApprovalResponse, error)
	OnToolUserInput(context.Context, *ToolRequestUserInputParams) (*ToolRequestUserInputResponse, error)
	OnDynamicToolCall(context.Context, *DynamicToolCallParams) (*DynamicToolCallResponse, error)
}

type AutoApprovalHandler struct{}

func (AutoApprovalHandler) OnCommandApproval(_ context.Context, _ *CommandExecutionRequestApprovalParams) (*CommandExecutionRequestApprovalResponse, error) {
	return &CommandExecutionRequestApprovalResponse{Decision: ApprovalDecisionAccept}, nil
}

func (AutoApprovalHandler) OnFileChangeApproval(_ context.Context, _ *FileChangeRequestApprovalParams) (*FileChangeRequestApprovalResponse, error) {
	return &FileChangeRequestApprovalResponse{Decision: ApprovalDecisionAccept}, nil
}

func (AutoApprovalHandler) OnPermissionsApproval(_ context.Context, params *PermissionsRequestApprovalParams) (*PermissionsRequestApprovalResponse, error) {
	return &PermissionsRequestApprovalResponse{Permissions: params.Permissions, Scope: params.Scope}, nil
}

func (AutoApprovalHandler) OnToolUserInput(_ context.Context, params *ToolRequestUserInputParams) (*ToolRequestUserInputResponse, error) {
	answers := make([]string, len(params.Questions))
	return &ToolRequestUserInputResponse{Answers: answers}, nil
}

func (AutoApprovalHandler) OnDynamicToolCall(_ context.Context, _ *DynamicToolCallParams) (*DynamicToolCallResponse, error) {
	return &DynamicToolCallResponse{Success: false}, nil
}
