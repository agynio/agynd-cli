package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateSkillNameRejectsNonSlug(t *testing.T) {
	for _, name := range []string{"", ".", "..", "Skill One", "skill/one", "skill_one", "-skill", "SKILL"} {
		if err := validateSkillName(name); err == nil {
			t.Fatalf("expected error for skill name %q", name)
		}
	}
	for _, name := range []string{"skill", "skill-one", "s", "s3-review-2"} {
		if err := validateSkillName(name); err != nil {
			t.Fatalf("expected %q to be valid, got %v", name, err)
		}
	}
}

func TestSkillsDirForSDK(t *testing.T) {
	t.Setenv("HOME", "")

	dir, err := skillsDirForSDK(SDKCodex)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(codexDefaultHome, ".agents", "skills")
	if dir != expected {
		t.Fatalf("expected skills dir %q, got %q", expected, dir)
	}

	if _, err := skillsDirForSDK(SDKAgn); err == nil {
		t.Fatal("expected agn to have no skills directory")
	}
}

func TestWriteSkillsPlacesSkillMarkdown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := writeSkills(SDKCodex, []skill{{
		ID:          "1",
		Name:        "release-notes",
		Description: `Use when drafting notes: the "changelog" kind`,
		Body:        "# Release notes\n\nDo the thing.",
	}})
	if err != nil {
		t.Fatalf("write skills: %v", err)
	}

	path := filepath.Join(dir, "release-notes", "SKILL.md")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	got := string(contents)
	want := "---\nname: \"release-notes\"\ndescription: \"Use when drafting notes: the \\\"changelog\\\" kind\"\n---\n\n# Release notes\n\nDo the thing.\n"
	if got != want {
		t.Fatalf("unexpected SKILL.md:\n%q\nwant:\n%q", got, want)
	}
}

func TestWriteSkillsSkipsDuplicateNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := writeSkills(SDKCodex, []skill{
		{ID: "1", Name: "review", Description: "first", Body: "first body"},
		{ID: "2", Name: "review", Description: "second", Body: "second body"},
	})
	if err != nil {
		t.Fatalf("write skills: %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(dir, "review", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(contents), "first body") {
		t.Fatalf("expected the first skill to win, got %q", string(contents))
	}
}

func TestListSkillsSkipsMalformed(t *testing.T) {
	client := &stubSkillsClient{skills: []*agentsv1.Skill{
		protoSkill("1", "Bad Name", "described", "body"),
		protoSkill("2", "no-description", "", "body"),
		protoSkill("3", "good", "described", "body"),
	}}

	skills, err := listSkills(t.Context(), client, "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "good" {
		t.Fatalf("expected only the valid skill, got %+v", skills)
	}
}

type stubSkillsClient struct {
	skills []*agentsv1.Skill
}

func (s *stubSkillsClient) ListSkills(_ context.Context, _ *agentsv1.ListSkillsRequest, _ ...grpc.CallOption) (*agentsv1.ListSkillsResponse, error) {
	return &agentsv1.ListSkillsResponse{Skills: s.skills}, nil
}

func protoSkill(id, name, description, body string) *agentsv1.Skill {
	return &agentsv1.Skill{
		Meta: &agentsv1.EntityMeta{
			Id:        id,
			CreatedAt: timestamppb.New(time.Unix(0, 0)),
		},
		Name:        name,
		Description: description,
		Body:        body,
	}
}
