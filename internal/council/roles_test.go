package council

import "testing"

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
		if seen[r.Name] {
			t.Errorf("DefaultRoles: duplicate role name %q", r.Name)
		}
		seen[r.Name] = true
	}
}
