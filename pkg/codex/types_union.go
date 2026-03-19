package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type AskForApproval struct {
	Mode     AskForApprovalMode
	Granular *AskForApprovalGranular
}

type AskForApprovalGranular struct {
	McpElicitations    bool `json:"mcp_elicitations"`
	RequestPermissions bool `json:"request_permissions,omitempty"`
	Rules              bool `json:"rules"`
	SandboxApproval    bool `json:"sandbox_approval"`
	SkillApproval      bool `json:"skill_approval,omitempty"`
}

func (a *AskForApproval) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var mode AskForApprovalMode
		if err := json.Unmarshal(data, &mode); err != nil {
			return err
		}
		a.Mode = mode
		a.Granular = nil
		return nil
	}
	var wrapper struct {
		Granular AskForApprovalGranular `json:"granular"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	a.Mode = ""
	a.Granular = &wrapper.Granular
	return nil
}

func (a AskForApproval) MarshalJSON() ([]byte, error) {
	if a.Granular != nil {
		return json.Marshal(struct {
			Granular *AskForApprovalGranular `json:"granular"`
		}{
			Granular: a.Granular,
		})
	}
	if a.Mode == "" {
		return nil, fmt.Errorf("ask for approval missing value")
	}
	return json.Marshal(a.Mode)
}

type SandboxPolicy struct {
	Type SandboxPolicyType
	Raw  json.RawMessage
}

func (s *SandboxPolicy) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var aux struct {
		Type SandboxPolicyType `json:"type"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	s.Type = aux.Type
	s.Raw = append(s.Raw[:0], data...)
	return nil
}

func (s SandboxPolicy) MarshalJSON() ([]byte, error) {
	if len(s.Raw) > 0 {
		return s.Raw, nil
	}
	if s.Type == "" {
		return nil, fmt.Errorf("sandbox policy missing type")
	}
	return json.Marshal(struct {
		Type SandboxPolicyType `json:"type"`
	}{
		Type: s.Type,
	})
}

type ThreadItem struct {
	Type                ThreadItemType
	UserMessage         *UserMessageThreadItem
	AgentMessage        *AgentMessageThreadItem
	Plan                *PlanThreadItem
	Reasoning           *ReasoningThreadItem
	CommandExecution    *CommandExecutionThreadItem
	FileChange          *FileChangeThreadItem
	McpToolCall         *McpToolCallThreadItem
	DynamicToolCall     *DynamicToolCallThreadItem
	CollabAgentToolCall *CollabAgentToolCallThreadItem
	WebSearch           *WebSearchThreadItem
	ImageView           *ImageViewThreadItem
	ImageGeneration     *ImageGenerationThreadItem
	EnteredReviewMode   *EnteredReviewModeThreadItem
	ExitedReviewMode    *ExitedReviewModeThreadItem
	ContextCompaction   *ContextCompactionThreadItem
	Raw                 json.RawMessage
}

func (t *ThreadItem) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var aux struct {
		Type ThreadItemType `json:"type"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	t.Type = aux.Type
	switch aux.Type {
	case ThreadItemTypeUserMessage:
		var item UserMessageThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.UserMessage = &item
	case ThreadItemTypeAgentMessage:
		var item AgentMessageThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.AgentMessage = &item
	case ThreadItemTypePlan:
		var item PlanThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.Plan = &item
	case ThreadItemTypeReasoning:
		var item ReasoningThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.Reasoning = &item
	case ThreadItemTypeCommandExecution:
		var item CommandExecutionThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.CommandExecution = &item
	case ThreadItemTypeFileChange:
		var item FileChangeThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.FileChange = &item
	case ThreadItemTypeMcpToolCall:
		var item McpToolCallThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.McpToolCall = &item
	case ThreadItemTypeDynamicToolCall:
		var item DynamicToolCallThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.DynamicToolCall = &item
	case ThreadItemTypeCollabAgentToolCall:
		var item CollabAgentToolCallThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.CollabAgentToolCall = &item
	case ThreadItemTypeWebSearch:
		var item WebSearchThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.WebSearch = &item
	case ThreadItemTypeImageView:
		var item ImageViewThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.ImageView = &item
	case ThreadItemTypeImageGeneration:
		var item ImageGenerationThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.ImageGeneration = &item
	case ThreadItemTypeEnteredReviewMode:
		var item EnteredReviewModeThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.EnteredReviewMode = &item
	case ThreadItemTypeExitedReviewMode:
		var item ExitedReviewModeThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.ExitedReviewMode = &item
	case ThreadItemTypeContextCompaction:
		var item ContextCompactionThreadItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		t.ContextCompaction = &item
	default:
		t.Raw = append(t.Raw[:0], data...)
	}
	return nil
}

func (t ThreadItem) MarshalJSON() ([]byte, error) {
	switch {
	case t.UserMessage != nil:
		return json.Marshal(t.UserMessage)
	case t.AgentMessage != nil:
		return json.Marshal(t.AgentMessage)
	case t.Plan != nil:
		return json.Marshal(t.Plan)
	case t.Reasoning != nil:
		return json.Marshal(t.Reasoning)
	case t.CommandExecution != nil:
		return json.Marshal(t.CommandExecution)
	case t.FileChange != nil:
		return json.Marshal(t.FileChange)
	case t.McpToolCall != nil:
		return json.Marshal(t.McpToolCall)
	case t.DynamicToolCall != nil:
		return json.Marshal(t.DynamicToolCall)
	case t.CollabAgentToolCall != nil:
		return json.Marshal(t.CollabAgentToolCall)
	case t.WebSearch != nil:
		return json.Marshal(t.WebSearch)
	case t.ImageView != nil:
		return json.Marshal(t.ImageView)
	case t.ImageGeneration != nil:
		return json.Marshal(t.ImageGeneration)
	case t.EnteredReviewMode != nil:
		return json.Marshal(t.EnteredReviewMode)
	case t.ExitedReviewMode != nil:
		return json.Marshal(t.ExitedReviewMode)
	case t.ContextCompaction != nil:
		return json.Marshal(t.ContextCompaction)
	case len(t.Raw) > 0:
		return t.Raw, nil
	default:
		return nil, fmt.Errorf("thread item missing payload")
	}
}

type UserInput struct {
	Type       UserInputType
	Text       *TextUserInput
	Image      *ImageUserInput
	LocalImage *LocalImageUserInput
	Skill      *SkillUserInput
	Mention    *MentionUserInput
	Raw        json.RawMessage
}

func (u *UserInput) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var aux struct {
		Type UserInputType `json:"type"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	u.Type = aux.Type
	switch aux.Type {
	case UserInputTypeText:
		var item TextUserInput
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		u.Text = &item
	case UserInputTypeImage:
		var item ImageUserInput
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		u.Image = &item
	case UserInputTypeLocalImage:
		var item LocalImageUserInput
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		u.LocalImage = &item
	case UserInputTypeSkill:
		var item SkillUserInput
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		u.Skill = &item
	case UserInputTypeMention:
		var item MentionUserInput
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		u.Mention = &item
	default:
		u.Raw = append(u.Raw[:0], data...)
	}
	return nil
}

func (u UserInput) MarshalJSON() ([]byte, error) {
	switch {
	case u.Text != nil:
		return json.Marshal(u.Text)
	case u.Image != nil:
		return json.Marshal(u.Image)
	case u.LocalImage != nil:
		return json.Marshal(u.LocalImage)
	case u.Skill != nil:
		return json.Marshal(u.Skill)
	case u.Mention != nil:
		return json.Marshal(u.Mention)
	case len(u.Raw) > 0:
		return u.Raw, nil
	default:
		return nil, fmt.Errorf("user input missing payload")
	}
}

func NewTextUserInput(text string) UserInput {
	return UserInput{Text: &TextUserInput{Type: UserInputTypeText, Text: text}}
}
