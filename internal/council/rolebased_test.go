package council

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// roleCouncilFixture returns a Council wired for RoleBased strategy with 2 roles.
func roleCouncilFixture(complete func(ctx context.Context, req CompletionRequest) (CompletionResponse, error)) *Council {
	registry := map[string]CouncilType{
		"roles": {
			Name:          "roles",
			Strategy:      RoleBased,
			Models:        []string{"model-a", "model-b"},
			ChairmanModel: "chairman",
			Temperature:   0.7,
			Roles: []Role{
				{Name: "security", Instruction: "Find security issues."},
				{Name: "logic", Instruction: "Find logic errors."},
			},
		},
	}
	return NewCouncil(&mockLLMClient{complete: complete}, registry, nil)
}

func TestRunRoleBased_Stage1_ParallelRoles(t *testing.T) {
	var mu sync.Mutex
	called := map[string]int{}

	c := roleCouncilFixture(func(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
		mu.Lock()
		called[req.Model]++
		mu.Unlock()
		return makeResponse(`[{"file":"a.go","line":1,"severity":"low","body":"ok"}]`), nil
	})

	var events []string
	err := c.RunFull(context.Background(), "diff here", "roles", func(eventType string, _ any) {
		events = append(events, eventType)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2 role models + 1 chairman = 3 calls total
	total := 0
	for _, n := range called {
		total += n
	}
	if total != 3 {
		t.Errorf("expected 3 LLM calls (2 roles + chairman), got %d", total)
	}
}

func TestRunRoleBased_EmitsAllThreeEvents(t *testing.T) {
	c := roleCouncilFixture(func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
		return makeResponse("findings"), nil
	})

	var events []string
	err := c.RunFull(context.Background(), "query", "roles", func(eventType string, _ any) {
		events = append(events, eventType)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"stage1_complete", "stage2_complete", "stage3_complete"}
	if len(events) != len(want) {
		t.Fatalf("expected events %v, got %v", want, events)
	}
	for i, e := range want {
		if events[i] != e {
			t.Errorf("event[%d]: want %q, got %q", i, e, events[i])
		}
	}
}

func TestRunRoleBased_QuorumFailure_ReturnsError(t *testing.T) {
	c := roleCouncilFixture(func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
		if req.Model != "chairman" {
			return CompletionResponse{}, errors.New("role model down")
		}
		return makeResponse("ok"), nil
	})

	err := c.RunFull(context.Background(), "query", "roles", func(string, any) {})
	if err == nil {
		t.Fatal("expected quorum error, got nil")
	}
	var qe *QuorumError
	if !errors.As(err, &qe) {
		t.Errorf("expected *QuorumError, got %T: %v", err, err)
	}
}

func TestRunRoleBased_Stage1UsesRoleInstructions(t *testing.T) {
	var systemPrompts []string
	var mu sync.Mutex

	c := roleCouncilFixture(func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
		for _, m := range req.Messages {
			if m.Role == "system" {
				mu.Lock()
				systemPrompts = append(systemPrompts, m.Content)
				mu.Unlock()
			}
		}
		return makeResponse("[]"), nil
	})

	_ = c.RunFull(context.Background(), "query", "roles", func(string, any) {})

	if len(systemPrompts) < 2 {
		t.Fatalf("expected at least 2 system prompts (one per role), got %d", len(systemPrompts))
	}
	found := map[string]bool{}
	for _, p := range systemPrompts {
		if p == "Find security issues." {
			found["security"] = true
		}
		if p == "Find logic errors." {
			found["logic"] = true
		}
	}
	if !found["security"] {
		t.Error("security role instruction not sent to any model")
	}
	if !found["logic"] {
		t.Error("logic role instruction not sent to any model")
	}
}

func TestRunRoleBased_Stage2CompleteData_HasLabelToModel(t *testing.T) {
	c := roleCouncilFixture(func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
		return makeResponse("[]"), nil
	})

	var stage2Data any
	_ = c.RunFull(context.Background(), "query", "roles", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			stage2Data = data
		}
	})

	d, ok := stage2Data.(Stage2CompleteData)
	if !ok {
		t.Fatalf("stage2_complete data must be Stage2CompleteData, got %T", stage2Data)
	}
	if d.Metadata.LabelToModel == nil {
		t.Fatal("LabelToModel must not be nil")
	}
	if _, ok := d.Metadata.LabelToModel["security"]; !ok {
		t.Error("LabelToModel must contain 'security' role")
	}
}

func TestRunRoleBased_ModelsAssignedByIndex(t *testing.T) {
	// 3 roles, 2 models → models assigned: role0→model-a, role1→model-b, role2→model-a
	registry := map[string]CouncilType{
		"three-roles": {
			Name:          "three-roles",
			Strategy:      RoleBased,
			Models:        []string{"model-a", "model-b"},
			ChairmanModel: "chairman",
			Temperature:   0.7,
			Roles: []Role{
				{Name: "r0", Instruction: "Role 0"},
				{Name: "r1", Instruction: "Role 1"},
				{Name: "r2", Instruction: "Role 2"},
			},
		},
	}
	var mu sync.Mutex
	modelUsed := map[string]string{} // system instruction → model used

	c := NewCouncil(&mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			for _, m := range req.Messages {
				if m.Role == "system" {
					mu.Lock()
					modelUsed[m.Content] = req.Model
					mu.Unlock()
				}
			}
			return makeResponse("[]"), nil
		},
	}, registry, nil)

	_ = c.RunFull(context.Background(), "q", "three-roles", func(string, any) {})

	if modelUsed["Role 0"] != "model-a" {
		t.Errorf("role 0 should use model-a, got %q", modelUsed["Role 0"])
	}
	if modelUsed["Role 1"] != "model-b" {
		t.Errorf("role 1 should use model-b, got %q", modelUsed["Role 1"])
	}
	if modelUsed["Role 2"] != "model-a" {
		t.Errorf("role 2 should cycle back to model-a, got %q", modelUsed["Role 2"])
	}
}

