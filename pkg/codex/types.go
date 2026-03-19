package codex

import "encoding/json"

type ClientInfo struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Title   *string `json:"title,omitempty"`
}

type InitializeCapabilities struct {
	ExperimentalAPI           bool     `json:"experimentalApi,omitempty"`
	OptOutNotificationMethods []string `json:"optOutNotificationMethods,omitempty"`
}

type InitializeParams struct {
	ClientInfo   ClientInfo              `json:"clientInfo"`
	Capabilities *InitializeCapabilities `json:"capabilities,omitempty"`
}

type InitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type ThreadStartParams struct {
	Model                 *string            `json:"model,omitempty"`
	ModelProvider         *string            `json:"modelProvider,omitempty"`
	Cwd                   *string            `json:"cwd,omitempty"`
	BaseInstructions      *string            `json:"baseInstructions,omitempty"`
	DeveloperInstructions *string            `json:"developerInstructions,omitempty"`
	ApprovalPolicy        *AskForApproval    `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     *ApprovalsReviewer `json:"approvalsReviewer,omitempty"`
	Sandbox               *SandboxMode       `json:"sandbox,omitempty"`
	ServiceTier           *ServiceTier       `json:"serviceTier,omitempty"`
	Personality           *Personality       `json:"personality,omitempty"`
	Ephemeral             *bool              `json:"ephemeral,omitempty"`
	Config                map[string]any     `json:"config,omitempty"`
	ServiceName           *string            `json:"serviceName,omitempty"`
}

type ThreadStartResponse struct {
	Thread            Thread            `json:"thread"`
	Model             string            `json:"model"`
	ModelProvider     string            `json:"modelProvider"`
	Cwd               string            `json:"cwd"`
	ApprovalPolicy    AskForApproval    `json:"approvalPolicy"`
	ApprovalsReviewer ApprovalsReviewer `json:"approvalsReviewer"`
	Sandbox           SandboxPolicy     `json:"sandbox"`
	ServiceTier       *ServiceTier      `json:"serviceTier,omitempty"`
	ReasoningEffort   *ReasoningEffort  `json:"reasoningEffort,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID              string             `json:"threadId"`
	Model                 *string            `json:"model,omitempty"`
	ModelProvider         *string            `json:"modelProvider,omitempty"`
	Cwd                   *string            `json:"cwd,omitempty"`
	BaseInstructions      *string            `json:"baseInstructions,omitempty"`
	DeveloperInstructions *string            `json:"developerInstructions,omitempty"`
	ApprovalPolicy        *AskForApproval    `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     *ApprovalsReviewer `json:"approvalsReviewer,omitempty"`
	Sandbox               *SandboxMode       `json:"sandbox,omitempty"`
	ServiceTier           *ServiceTier       `json:"serviceTier,omitempty"`
	Personality           *Personality       `json:"personality,omitempty"`
	Config                map[string]any     `json:"config,omitempty"`
}

type ThreadResumeResponse struct {
	Thread            Thread            `json:"thread"`
	Model             string            `json:"model"`
	ModelProvider     string            `json:"modelProvider"`
	Cwd               string            `json:"cwd"`
	ApprovalPolicy    AskForApproval    `json:"approvalPolicy"`
	ApprovalsReviewer ApprovalsReviewer `json:"approvalsReviewer"`
	Sandbox           SandboxPolicy     `json:"sandbox"`
	ServiceTier       *ServiceTier      `json:"serviceTier,omitempty"`
	ReasoningEffort   *ReasoningEffort  `json:"reasoningEffort,omitempty"`
}

type ThreadReadParams struct {
	ThreadID     string `json:"threadId"`
	IncludeTurns bool   `json:"includeTurns,omitempty"`
}

type ThreadReadResponse struct {
	Thread Thread `json:"thread"`
}

type ThreadListParams struct {
	Archived       *bool              `json:"archived,omitempty"`
	Cursor         *string            `json:"cursor,omitempty"`
	Cwd            *string            `json:"cwd,omitempty"`
	Limit          *uint32            `json:"limit,omitempty"`
	ModelProviders []string           `json:"modelProviders,omitempty"`
	SearchTerm     *string            `json:"searchTerm,omitempty"`
	SortKey        *ThreadSortKey     `json:"sortKey,omitempty"`
	SourceKinds    []ThreadSourceKind `json:"sourceKinds,omitempty"`
}

type ThreadListResponse struct {
	Data       []Thread `json:"data"`
	NextCursor *string  `json:"nextCursor,omitempty"`
}

type TurnStartParams struct {
	ThreadID          string             `json:"threadId"`
	Input             []UserInput        `json:"input"`
	Model             *string            `json:"model,omitempty"`
	Effort            *ReasoningEffort   `json:"effort,omitempty"`
	Personality       *Personality       `json:"personality,omitempty"`
	ApprovalPolicy    *AskForApproval    `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer *ApprovalsReviewer `json:"approvalsReviewer,omitempty"`
	SandboxPolicy     *SandboxPolicy     `json:"sandboxPolicy,omitempty"`
	ServiceTier       *ServiceTier       `json:"serviceTier,omitempty"`
	Cwd               *string            `json:"cwd,omitempty"`
	OutputSchema      json.RawMessage    `json:"outputSchema,omitempty"`
	Summary           json.RawMessage    `json:"summary,omitempty"`
}

type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type TurnSteerParams struct {
	ExpectedTurnID string      `json:"expectedTurnId"`
	Input          []UserInput `json:"input"`
	ThreadID       string      `json:"threadId"`
}

type TurnSteerResponse struct {
	TurnID string `json:"turnId"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type TurnInterruptResponse struct{}

type ModelListParams struct {
	Cursor        *string `json:"cursor,omitempty"`
	IncludeHidden *bool   `json:"includeHidden,omitempty"`
	Limit         *uint32 `json:"limit,omitempty"`
}

type ModelListResponse struct {
	Data       []Model `json:"data"`
	NextCursor *string `json:"nextCursor,omitempty"`
}

type Model struct {
	AvailabilityNux           json.RawMessage         `json:"availabilityNux,omitempty"`
	DefaultReasoningEffort    ReasoningEffort         `json:"defaultReasoningEffort"`
	Description               string                  `json:"description"`
	DisplayName               string                  `json:"displayName"`
	Hidden                    bool                    `json:"hidden"`
	ID                        string                  `json:"id"`
	InputModalities           []InputModality         `json:"inputModalities,omitempty"`
	IsDefault                 bool                    `json:"isDefault"`
	Model                     string                  `json:"model"`
	SupportedReasoningEfforts []ReasoningEffortOption `json:"supportedReasoningEfforts"`
	SupportsPersonality       bool                    `json:"supportsPersonality,omitempty"`
	Upgrade                   *string                 `json:"upgrade,omitempty"`
	UpgradeInfo               json.RawMessage         `json:"upgradeInfo,omitempty"`
}

type ReasoningEffortOption struct {
	Description     string          `json:"description"`
	ReasoningEffort ReasoningEffort `json:"reasoningEffort"`
}

type ConfigReadParams struct {
	Cwd           *string `json:"cwd,omitempty"`
	IncludeLayers *bool   `json:"includeLayers,omitempty"`
}

type ConfigReadResponse struct {
	Config json.RawMessage   `json:"config"`
	Layers []json.RawMessage `json:"layers,omitempty"`
}

type Thread struct {
	ID            string          `json:"id"`
	CliVersion    string          `json:"cliVersion"`
	CreatedAt     int64           `json:"createdAt"`
	UpdatedAt     int64           `json:"updatedAt"`
	Cwd           string          `json:"cwd"`
	Ephemeral     bool            `json:"ephemeral"`
	ModelProvider string          `json:"modelProvider"`
	Preview       string          `json:"preview"`
	Source        json.RawMessage `json:"source"`
	Status        json.RawMessage `json:"status"`
	Turns         []Turn          `json:"turns"`
	Name          *string         `json:"name,omitempty"`
	GitInfo       *GitInfo        `json:"gitInfo,omitempty"`
	Path          *string         `json:"path,omitempty"`
	AgentNickname *string         `json:"agentNickname,omitempty"`
	AgentRole     *string         `json:"agentRole,omitempty"`
}

type GitInfo struct {
	Root   string `json:"root,omitempty"`
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

type Turn struct {
	ID     string       `json:"id"`
	Status TurnStatus   `json:"status"`
	Items  []ThreadItem `json:"items"`
	Error  *TurnError   `json:"error,omitempty"`
}

type TurnError struct {
	Message           string          `json:"message"`
	AdditionalDetails *string         `json:"additionalDetails,omitempty"`
	CodexErrorInfo    json.RawMessage `json:"codexErrorInfo,omitempty"`
}

type ByteRange struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

type TextElement struct {
	ByteRange   ByteRange `json:"byteRange"`
	Placeholder *string   `json:"placeholder,omitempty"`
}

type TokenUsageBreakdown struct {
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	InputTokens           int64 `json:"inputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
	TotalTokens           int64 `json:"totalTokens"`
}

type ThreadTokenUsage struct {
	Last               TokenUsageBreakdown `json:"last"`
	Total              TokenUsageBreakdown `json:"total"`
	ModelContextWindow *int64              `json:"modelContextWindow,omitempty"`
}

type TurnStartedNotification struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
}

type TurnCompletedNotification struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
}

type ItemStartedNotification struct {
	ThreadID string     `json:"threadId"`
	TurnID   string     `json:"turnId"`
	Item     ThreadItem `json:"item"`
}

type ItemCompletedNotification struct {
	ThreadID string     `json:"threadId"`
	TurnID   string     `json:"turnId"`
	Item     ThreadItem `json:"item"`
}

type AgentMessageDeltaNotification struct {
	Delta    string `json:"delta"`
	ItemID   string `json:"itemId"`
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type CommandExecutionOutputDeltaNotification struct {
	Delta    string `json:"delta"`
	ItemID   string `json:"itemId"`
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type FileChangeOutputDeltaNotification struct {
	Delta    string `json:"delta"`
	ItemID   string `json:"itemId"`
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type ThreadTokenUsageUpdatedNotification struct {
	ThreadID   string           `json:"threadId"`
	TurnID     string           `json:"turnId"`
	TokenUsage ThreadTokenUsage `json:"tokenUsage"`
}

type ErrorNotification struct {
	ThreadID  string    `json:"threadId"`
	TurnID    string    `json:"turnId"`
	Error     TurnError `json:"error"`
	WillRetry bool      `json:"willRetry"`
}

type UserMessageThreadItem struct {
	ID      string         `json:"id"`
	Type    ThreadItemType `json:"type"`
	Content []UserInput    `json:"content"`
}

type AgentMessageThreadItem struct {
	ID             string          `json:"id"`
	Type           ThreadItemType  `json:"type"`
	Text           string          `json:"text"`
	Phase          *MessagePhase   `json:"phase,omitempty"`
	MemoryCitation json.RawMessage `json:"memoryCitation,omitempty"`
}

type PlanThreadItem struct {
	ID   string         `json:"id"`
	Type ThreadItemType `json:"type"`
	Text string         `json:"text"`
}

type ReasoningThreadItem struct {
	ID      string         `json:"id"`
	Type    ThreadItemType `json:"type"`
	Content []string       `json:"content,omitempty"`
	Summary []string       `json:"summary,omitempty"`
}

type CommandExecutionThreadItem struct {
	ID               string                 `json:"id"`
	Type             ThreadItemType         `json:"type"`
	Command          string                 `json:"command"`
	CommandActions   []json.RawMessage      `json:"commandActions"`
	Cwd              string                 `json:"cwd"`
	Status           CommandExecutionStatus `json:"status"`
	AggregatedOutput *string                `json:"aggregatedOutput,omitempty"`
	DurationMs       *int64                 `json:"durationMs,omitempty"`
	ExitCode         *int32                 `json:"exitCode,omitempty"`
	ProcessID        *string                `json:"processId,omitempty"`
}

type FileChangeThreadItem struct {
	ID      string             `json:"id"`
	Type    ThreadItemType     `json:"type"`
	Status  PatchApplyStatus   `json:"status"`
	Changes []FileUpdateChange `json:"changes"`
}

type FileUpdateChange struct {
	Diff string          `json:"diff"`
	Kind json.RawMessage `json:"kind"`
	Path string          `json:"path"`
}

type McpToolCallThreadItem struct {
	ID         string            `json:"id"`
	Type       ThreadItemType    `json:"type"`
	Server     string            `json:"server"`
	Tool       string            `json:"tool"`
	Status     McpToolCallStatus `json:"status"`
	Arguments  json.RawMessage   `json:"arguments"`
	Result     json.RawMessage   `json:"result,omitempty"`
	Error      json.RawMessage   `json:"error,omitempty"`
	DurationMs *int64            `json:"durationMs,omitempty"`
}

type DynamicToolCallThreadItem struct {
	ID           string                `json:"id"`
	Type         ThreadItemType        `json:"type"`
	Tool         string                `json:"tool"`
	Status       DynamicToolCallStatus `json:"status"`
	Arguments    json.RawMessage       `json:"arguments"`
	ContentItems []json.RawMessage     `json:"contentItems,omitempty"`
	Success      *bool                 `json:"success,omitempty"`
	DurationMs   *int64                `json:"durationMs,omitempty"`
}

type CollabAgentToolCallThreadItem struct {
	ID                string                     `json:"id"`
	Type              ThreadItemType             `json:"type"`
	SenderThreadID    string                     `json:"senderThreadId"`
	ReceiverThreadIDs []string                   `json:"receiverThreadIds"`
	AgentsStates      map[string]json.RawMessage `json:"agentsStates"`
	Tool              json.RawMessage            `json:"tool"`
	Status            json.RawMessage            `json:"status"`
	Model             *string                    `json:"model,omitempty"`
	Prompt            *string                    `json:"prompt,omitempty"`
	ReasoningEffort   *ReasoningEffort           `json:"reasoningEffort,omitempty"`
}

type WebSearchThreadItem struct {
	ID     string          `json:"id"`
	Type   ThreadItemType  `json:"type"`
	Query  string          `json:"query"`
	Action json.RawMessage `json:"action,omitempty"`
}

type ImageViewThreadItem struct {
	ID   string         `json:"id"`
	Type ThreadItemType `json:"type"`
	Path string         `json:"path"`
}

type ImageGenerationThreadItem struct {
	ID            string         `json:"id"`
	Type          ThreadItemType `json:"type"`
	Result        string         `json:"result"`
	Status        string         `json:"status"`
	RevisedPrompt *string        `json:"revisedPrompt,omitempty"`
}

type EnteredReviewModeThreadItem struct {
	ID     string         `json:"id"`
	Type   ThreadItemType `json:"type"`
	Review string         `json:"review"`
}

type ExitedReviewModeThreadItem struct {
	ID     string         `json:"id"`
	Type   ThreadItemType `json:"type"`
	Review string         `json:"review"`
}

type ContextCompactionThreadItem struct {
	ID   string         `json:"id"`
	Type ThreadItemType `json:"type"`
}

type TextUserInput struct {
	Type         UserInputType `json:"type"`
	Text         string        `json:"text"`
	TextElements []TextElement `json:"text_elements,omitempty"`
}

type ImageUserInput struct {
	Type UserInputType `json:"type"`
	URL  string        `json:"url"`
}

type LocalImageUserInput struct {
	Type UserInputType `json:"type"`
	Path string        `json:"path"`
}

type SkillUserInput struct {
	Type UserInputType `json:"type"`
	Name string        `json:"name"`
	Path string        `json:"path"`
}

type MentionUserInput struct {
	Type UserInputType `json:"type"`
	Name string        `json:"name"`
	Path string        `json:"path"`
}

type CommandExecutionRequestApprovalParams struct {
	Command string `json:"command,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
}

type CommandExecutionRequestApprovalResponse struct {
	Decision ApprovalDecision `json:"decision"`
}

type FileChangeRequestApprovalParams struct {
	Reason  *string            `json:"reason,omitempty"`
	Changes []FileUpdateChange `json:"changes,omitempty"`
}

type FileChangeRequestApprovalResponse struct {
	Decision ApprovalDecision `json:"decision"`
}

type PermissionsRequestApprovalParams struct {
	Permissions json.RawMessage `json:"permissions,omitempty"`
	Scope       json.RawMessage `json:"scope,omitempty"`
}

type PermissionsRequestApprovalResponse struct {
	Permissions json.RawMessage `json:"permissions"`
	Scope       json.RawMessage `json:"scope"`
}

type ToolUserInputQuestion struct {
	ID      string   `json:"id,omitempty"`
	Prompt  string   `json:"prompt,omitempty"`
	Type    string   `json:"type,omitempty"`
	Options []string `json:"options,omitempty"`
	Default string   `json:"default,omitempty"`
}

type ToolRequestUserInputParams struct {
	Questions []ToolUserInputQuestion `json:"questions,omitempty"`
}

type ToolRequestUserInputResponse struct {
	Answers []string `json:"answers"`
}

type DynamicToolCallParams struct {
	Tool      string          `json:"tool,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type DynamicToolCallResponse struct {
	ContentItems []json.RawMessage `json:"contentItems,omitempty"`
	Success      bool              `json:"success"`
}
