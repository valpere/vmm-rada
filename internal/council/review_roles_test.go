package council

import "testing"

func TestDefaultReviewRoles_Count(t *testing.T) {
	if len(DefaultReviewRoles) < 3 {
		t.Fatalf("expected at least 3 default review roles, got %d", len(DefaultReviewRoles))
	}
}

func TestDefaultReviewRoles_UniqueNames(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range DefaultReviewRoles {
		if r.Name == "" {
			t.Error("role has empty name")
		}
		if seen[r.Name] {
			t.Errorf("duplicate role name: %q", r.Name)
		}
		seen[r.Name] = true
	}
}

func TestDefaultReviewRoles_InstructionsNonEmpty(t *testing.T) {
	for _, r := range DefaultReviewRoles {
		if r.Instruction == "" {
			t.Errorf("role %q has empty instruction", r.Name)
		}
	}
}

func TestNewCodeReviewCouncilType_Strategy(t *testing.T) {
	models := []string{"model-a", "model-b", "model-c", "model-d"}
	chairman := "chairman-model"
	ct := NewCodeReviewCouncilType(models, chairman, 0.7)

	if ct.Strategy != RoleBased {
		t.Errorf("expected RoleBased strategy, got %d", ct.Strategy)
	}
	if ct.Name != "code-review" {
		t.Errorf("expected name 'code-review', got %q", ct.Name)
	}
	if len(ct.Roles) != len(DefaultReviewRoles) {
		t.Errorf("expected %d roles, got %d", len(DefaultReviewRoles), len(ct.Roles))
	}
	for i, r := range ct.Roles {
		if r.Name != DefaultReviewRoles[i].Name {
			t.Errorf("role[%d]: expected %q, got %q", i, DefaultReviewRoles[i].Name, r.Name)
		}
	}
}
