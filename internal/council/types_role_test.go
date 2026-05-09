package council

import "testing"

func TestStrategyConstants_AllUnique(t *testing.T) {
	strategies := []Strategy{
		PeerReview, RoleBased, Majority, GenerateRankRefine,
		MultiAgentDebate, MixtureOfAgents, Delphi,
	}
	seen := make(map[Strategy]struct{}, len(strategies))
	for _, s := range strategies {
		if _, dup := seen[s]; dup {
			t.Fatalf("strategy %d duplicated in iota block", s)
		}
		seen[s] = struct{}{}
	}
}

func TestCouncilTypeHasRoles(t *testing.T) {
	ct := CouncilType{
		Name:     "test",
		Strategy: RoleBased,
		Roles: []Role{
			{Name: "critic", Instruction: "Find bugs."},
		},
	}
	if len(ct.Roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(ct.Roles))
	}
	if ct.Roles[0].Name != "critic" {
		t.Fatalf("unexpected role name %q", ct.Roles[0].Name)
	}
}
