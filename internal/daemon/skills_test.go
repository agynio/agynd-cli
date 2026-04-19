package daemon

import "testing"

func TestValidateSkillNameTraversal(t *testing.T) {
	for _, name := range []string{".", ".."} {
		if err := validateSkillName(name); err == nil {
			t.Fatalf("expected error for skill name %q", name)
		}
	}
}
