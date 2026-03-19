package codex

type AskForApprovalMode string

const (
	AskForApprovalUntrusted AskForApprovalMode = "untrusted"
	AskForApprovalOnFailure AskForApprovalMode = "on-failure"
	AskForApprovalOnRequest AskForApprovalMode = "on-request"
	AskForApprovalNever     AskForApprovalMode = "never"
)

type ApprovalsReviewer string

const (
	ApprovalsReviewerUser          ApprovalsReviewer = "user"
	ApprovalsReviewerGuardianAgent ApprovalsReviewer = "guardian_subagent"
)

type SandboxMode string

const (
	SandboxModeReadOnly         SandboxMode = "read-only"
	SandboxModeWorkspaceWrite   SandboxMode = "workspace-write"
	SandboxModeDangerFullAccess SandboxMode = "danger-full-access"
)

type SandboxPolicyType string

const (
	SandboxPolicyDangerFullAccess SandboxPolicyType = "dangerFullAccess"
	SandboxPolicyReadOnly         SandboxPolicyType = "readOnly"
	SandboxPolicyExternalSandbox  SandboxPolicyType = "externalSandbox"
	SandboxPolicyWorkspaceWrite   SandboxPolicyType = "workspaceWrite"
)

type MessagePhase string

const (
	MessagePhaseCommentary  MessagePhase = "commentary"
	MessagePhaseFinalAnswer MessagePhase = "final_answer"
)

type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
)

type ServiceTier string

const (
	ServiceTierFast ServiceTier = "fast"
	ServiceTierFlex ServiceTier = "flex"
)

type Personality string

const (
	PersonalityNone      Personality = "none"
	PersonalityFriendly  Personality = "friendly"
	PersonalityPragmatic Personality = "pragmatic"
)

type CommandExecutionStatus string

const (
	CommandExecutionStatusInProgress CommandExecutionStatus = "inProgress"
	CommandExecutionStatusCompleted  CommandExecutionStatus = "completed"
	CommandExecutionStatusFailed     CommandExecutionStatus = "failed"
	CommandExecutionStatusDeclined   CommandExecutionStatus = "declined"
)

type PatchApplyStatus string

const (
	PatchApplyStatusInProgress PatchApplyStatus = "inProgress"
	PatchApplyStatusCompleted  PatchApplyStatus = "completed"
	PatchApplyStatusFailed     PatchApplyStatus = "failed"
	PatchApplyStatusDeclined   PatchApplyStatus = "declined"
)

type TurnStatus string

const (
	TurnStatusCompleted   TurnStatus = "completed"
	TurnStatusInterrupted TurnStatus = "interrupted"
	TurnStatusFailed      TurnStatus = "failed"
	TurnStatusInProgress  TurnStatus = "inProgress"
)

type ThreadSortKey string

const (
	ThreadSortKeyCreatedAt ThreadSortKey = "created_at"
	ThreadSortKeyUpdatedAt ThreadSortKey = "updated_at"
)

type ThreadSourceKind string

const (
	ThreadSourceCLI             ThreadSourceKind = "cli"
	ThreadSourceVSCode          ThreadSourceKind = "vscode"
	ThreadSourceExec            ThreadSourceKind = "exec"
	ThreadSourceAppServer       ThreadSourceKind = "appServer"
	ThreadSourceSubAgent        ThreadSourceKind = "subAgent"
	ThreadSourceSubAgentReview  ThreadSourceKind = "subAgentReview"
	ThreadSourceSubAgentCompact ThreadSourceKind = "subAgentCompact"
	ThreadSourceSubAgentSpawn   ThreadSourceKind = "subAgentThreadSpawn"
	ThreadSourceSubAgentOther   ThreadSourceKind = "subAgentOther"
	ThreadSourceUnknown         ThreadSourceKind = "unknown"
)

type ThreadItemType string

const (
	ThreadItemTypeUserMessage         ThreadItemType = "userMessage"
	ThreadItemTypeAgentMessage        ThreadItemType = "agentMessage"
	ThreadItemTypePlan                ThreadItemType = "plan"
	ThreadItemTypeReasoning           ThreadItemType = "reasoning"
	ThreadItemTypeCommandExecution    ThreadItemType = "commandExecution"
	ThreadItemTypeFileChange          ThreadItemType = "fileChange"
	ThreadItemTypeMcpToolCall         ThreadItemType = "mcpToolCall"
	ThreadItemTypeDynamicToolCall     ThreadItemType = "dynamicToolCall"
	ThreadItemTypeCollabAgentToolCall ThreadItemType = "collabAgentToolCall"
	ThreadItemTypeWebSearch           ThreadItemType = "webSearch"
	ThreadItemTypeImageView           ThreadItemType = "imageView"
	ThreadItemTypeImageGeneration     ThreadItemType = "imageGeneration"
	ThreadItemTypeEnteredReviewMode   ThreadItemType = "enteredReviewMode"
	ThreadItemTypeExitedReviewMode    ThreadItemType = "exitedReviewMode"
	ThreadItemTypeContextCompaction   ThreadItemType = "contextCompaction"
)

type UserInputType string

const (
	UserInputTypeText       UserInputType = "text"
	UserInputTypeImage      UserInputType = "image"
	UserInputTypeLocalImage UserInputType = "localImage"
	UserInputTypeSkill      UserInputType = "skill"
	UserInputTypeMention    UserInputType = "mention"
)

type ApprovalDecision string

const (
	ApprovalDecisionAccept           ApprovalDecision = "accept"
	ApprovalDecisionAcceptForSession ApprovalDecision = "acceptForSession"
	ApprovalDecisionDecline          ApprovalDecision = "decline"
	ApprovalDecisionCancel           ApprovalDecision = "cancel"
)

type DynamicToolCallStatus string

const (
	DynamicToolCallStatusInProgress DynamicToolCallStatus = "inProgress"
	DynamicToolCallStatusCompleted  DynamicToolCallStatus = "completed"
	DynamicToolCallStatusFailed     DynamicToolCallStatus = "failed"
)

type McpToolCallStatus string

const (
	McpToolCallStatusInProgress McpToolCallStatus = "inProgress"
	McpToolCallStatusCompleted  McpToolCallStatus = "completed"
	McpToolCallStatusFailed     McpToolCallStatus = "failed"
)

type InputModality string

const (
	InputModalityText  InputModality = "text"
	InputModalityImage InputModality = "image"
)
