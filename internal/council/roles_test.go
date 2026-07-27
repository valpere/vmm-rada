package council

import (
	"reflect"
	"testing"
)

func TestDefaultRoles(t *testing.T) {
	if len(DefaultRoles) == 0 {
		t.Fatal("DefaultRoles: got empty, want at least one role")
	}
	seen := make(map[string]bool, len(DefaultRoles))
	for i, r := range DefaultRoles {
		if r.Name == "" {
			t.Errorf("DefaultRoles[%d]: empty Name", i)
		}
		if r.Instruction == "" {
			t.Errorf("DefaultRoles[%d] (%q): empty Instruction", i, r.Name)
		}
		if r.Model != "" {
			t.Errorf("DefaultRoles[%d] (%q): Model must be empty in the template, got %q", i, r.Name, r.Model)
		}
		if seen[r.Name] {
			t.Errorf("DefaultRoles: duplicate role name %q", r.Name)
		}
		seen[r.Name] = true
	}
}

func TestDefaultRoles_ExactShape(t *testing.T) {
	if len(DefaultRoles) != 5 {
		t.Fatalf("len(DefaultRoles) = %d, want 5", len(DefaultRoles))
	}
	wantNames := []string{"Creator", "Critic", "Verifier", "Simplifier", "DevilsAdvocate"}
	for i, want := range wantNames {
		if DefaultRoles[i].Name != want {
			t.Errorf("DefaultRoles[%d].Name = %q, want %q", i, DefaultRoles[i].Name, want)
		}
	}
}

func TestDefaultRoleKeys_MatchesDefaultRoles(t *testing.T) {
	if len(DefaultRoleKeys) != len(DefaultRoles) {
		t.Fatalf("len(DefaultRoleKeys) = %d, len(DefaultRoles) = %d, want equal", len(DefaultRoleKeys), len(DefaultRoles))
	}
	wantKeys := []string{"creator", "critic", "verifier", "simplifier", "devils_advocate"}
	if !reflect.DeepEqual(DefaultRoleKeys, wantKeys) {
		t.Errorf("DefaultRoleKeys = %v, want %v", DefaultRoleKeys, wantKeys)
	}
}

func fullRoleModels() map[string]string {
	return map[string]string{
		"creator":         "model-creator",
		"critic":          "model-critic",
		"verifier":        "model-verifier",
		"simplifier":      "model-simplifier",
		"devils_advocate": "model-devils-advocate",
	}
}

func TestRolesWithModels_Happy(t *testing.T) {
	roles, err := RolesWithModels(fullRoleModels())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != len(DefaultRoles) {
		t.Fatalf("len(roles) = %d, want %d", len(roles), len(DefaultRoles))
	}
	for i, want := range []string{"model-creator", "model-critic", "model-verifier", "model-simplifier", "model-devils-advocate"} {
		if roles[i].Model != want {
			t.Errorf("roles[%d].Model = %q, want %q", i, roles[i].Model, want)
		}
		if roles[i].Name != DefaultRoles[i].Name {
			t.Errorf("roles[%d].Name = %q, want %q (template not preserved)", i, roles[i].Name, DefaultRoles[i].Name)
		}
	}
	// DefaultRoles itself must remain untouched (roles is a clone, not an alias).
	for i, r := range DefaultRoles {
		if r.Model != "" {
			t.Errorf("DefaultRoles[%d].Model = %q after RolesWithModels call, want empty (template mutated)", i, r.Model)
		}
	}
}

func TestRolesWithModels_MissingRole(t *testing.T) {
	models := fullRoleModels()
	delete(models, "devils_advocate")
	if _, err := RolesWithModels(models); err == nil {
		t.Fatal("expected error for missing role, got nil")
	}
}

func TestRolesWithModels_UnknownKey(t *testing.T) {
	models := fullRoleModels()
	delete(models, "devils_advocate")
	models["unknown_role"] = "some-model"
	if _, err := RolesWithModels(models); err == nil {
		t.Fatal("expected error for unknown role key, got nil")
	}
}

func TestRolesWithModels_EmptyValue(t *testing.T) {
	models := fullRoleModels()
	models["critic"] = ""
	if _, err := RolesWithModels(models); err == nil {
		t.Fatal("expected error for empty model value, got nil")
	}
}
