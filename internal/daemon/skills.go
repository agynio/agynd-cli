package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"google.golang.org/grpc"
)

const skillsPageSize int32 = 100

type skillsClient interface {
	ListSkills(ctx context.Context, in *agentsv1.ListSkillsRequest, opts ...grpc.CallOption) (*agentsv1.ListSkillsResponse, error)
}

type skill struct {
	ID        string
	Name      string
	Body      string
	CreatedAt time.Time
}

func listSkills(ctx context.Context, client skillsClient, agentID string) ([]skill, error) {
	if client == nil {
		return nil, fmt.Errorf("skills client is required")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	var skills []skill
	pageToken := ""
	for {
		resp, err := client.ListSkills(ctx, &agentsv1.ListSkillsRequest{
			AgentId:   agentID,
			PageSize:  skillsPageSize,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, entry := range resp.GetSkills() {
			parsed, err := skillFromProto(entry)
			if err != nil {
				return nil, err
			}
			skills = append(skills, parsed)
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			break
		}
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].CreatedAt.Equal(skills[j].CreatedAt) {
			return skills[i].ID < skills[j].ID
		}
		return skills[i].CreatedAt.Before(skills[j].CreatedAt)
	})
	return skills, nil
}

func skillFromProto(entry *agentsv1.Skill) (skill, error) {
	if entry == nil {
		return skill{}, fmt.Errorf("skill is required")
	}
	meta := entry.GetMeta()
	if meta == nil {
		return skill{}, fmt.Errorf("skill meta is required")
	}
	id := strings.TrimSpace(meta.GetId())
	if id == "" {
		return skill{}, fmt.Errorf("skill id is required")
	}
	createdAt := meta.GetCreatedAt()
	if createdAt == nil {
		return skill{}, fmt.Errorf("skill created at is required")
	}
	name := strings.TrimSpace(entry.GetName())
	if err := validateSkillName(name); err != nil {
		return skill{}, err
	}
	body := entry.GetBody()
	if strings.TrimSpace(body) == "" {
		return skill{}, fmt.Errorf("skill body is required")
	}
	return skill{
		ID:        id,
		Name:      name,
		Body:      body,
		CreatedAt: createdAt.AsTime(),
	}, nil
}

func writeSkills(sdk string, skills []skill) (string, error) {
	if len(skills) == 0 {
		return "", nil
	}
	skillsDir, err := skillsDirForSDK(sdk)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		return "", fmt.Errorf("create skills dir: %w", err)
	}
	seen := make(map[string]struct{}, len(skills))
	for _, entry := range skills {
		if _, ok := seen[entry.Name]; ok {
			return "", fmt.Errorf("duplicate skill name %q", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		path := filepath.Join(skillsDir, entry.Name)
		if err := os.WriteFile(path, []byte(entry.Body), 0o600); err != nil {
			return "", fmt.Errorf("write skill %s: %w", entry.Name, err)
		}
	}
	return skillsDir, nil
}

func skillsDirForSDK(sdk string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	switch sdk {
	case SDKCodex:
		return filepath.Join(home, ".codex", "skills"), nil
	case SDKAgn:
		return filepath.Join(home, ".agyn", "agn", "skills"), nil
	case SDKClaude:
		return filepath.Join(home, ".claude", "skills"), nil
	default:
		return "", fmt.Errorf("unknown sdk %q", sdk)
	}
}

func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("skill name %q contains path separator", name)
	}
	return nil
}

func buildSystemPrompt(role string, skills []skill) string {
	role = strings.TrimSpace(role)
	if role == "" && len(skills) == 0 {
		return ""
	}
	parts := make([]string, 0, len(skills)+1)
	if role != "" {
		parts = append(parts, role)
	}
	for _, entry := range skills {
		body := strings.TrimSpace(entry.Body)
		if body == "" {
			continue
		}
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n")
}
