package config

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/valpere/vmm-rada/internal/council"
)

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func TestLoadCouncilRegistryYAML_ValidFull(t *testing.T) {
	registry, err := LoadCouncilRegistryYAML(testdataPath(t, "valid_full.yaml"), 0.7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantKeys := []string{"default", "role-based", "majority", "generate-rank-refine", "debate", "moa", "delphi"}
	for _, k := range wantKeys {
		if _, ok := registry[k]; !ok {
			t.Errorf("registry missing expected key %q", k)
		}
	}
	if len(registry) != len(wantKeys) {
		t.Errorf("len(registry) = %d, want %d", len(registry), len(wantKeys))
	}

	rb := registry["role-based"]
	if rb.Strategy != council.RoleBased {
		t.Errorf("role-based: Strategy = %v, want RoleBased", rb.Strategy)
	}
	if len(rb.Roles) != 5 {
		t.Fatalf("role-based: len(Roles) = %d, want 5", len(rb.Roles))
	}
	if rb.QuorumMin != 5 {
		t.Errorf("role-based: QuorumMin = %d, want 5 (default = len(roles), quorum: omitted)", rb.QuorumMin)
	}
	wantRoleModels := map[string]string{
		"Creator":        "role-creator-model",
		"Critic":         "role-critic-model",
		"Verifier":       "role-verifier-model",
		"Simplifier":     "role-simplifier-model",
		"DevilsAdvocate": "role-devils-advocate-model",
	}
	for _, r := range rb.Roles {
		want, ok := wantRoleModels[r.Name]
		if !ok {
			t.Errorf("role-based: unexpected role name %q", r.Name)
			continue
		}
		if r.Model != want {
			t.Errorf("role-based: role %q Model = %q, want %q", r.Name, r.Model, want)
		}
	}

	moa := registry["moa"]
	if moa.Strategy != council.MixtureOfAgents {
		t.Errorf("moa: Strategy = %v, want MixtureOfAgents", moa.Strategy)
	}
	if moa.RefinerModel != "moa-refiner" {
		t.Errorf("moa: RefinerModel = %q, want %q", moa.RefinerModel, "moa-refiner")
	}
	if len(moa.ProposerModels) != 2 || len(moa.AggregatorModels) != 1 {
		t.Errorf("moa: ProposerModels/AggregatorModels lengths = %d/%d, want 2/1", len(moa.ProposerModels), len(moa.AggregatorModels))
	}
	if moa.Models != nil || moa.ChairmanModel != "" {
		t.Errorf("moa: Models/ChairmanModel must stay unused, got Models=%v ChairmanModel=%q", moa.Models, moa.ChairmanModel)
	}

	pr := registry["default"]
	if pr.Strategy != council.PeerReview || pr.ChairmanModel != "chairman-model" || len(pr.Models) != 2 {
		t.Errorf("default: got %+v", pr)
	}
	if pr.Temperature != 0.7 {
		t.Errorf("default: Temperature = %v, want default 0.7 (no override in fixture)", pr.Temperature)
	}
}

func TestLoadCouncilRegistryYAML_MissingRole(t *testing.T) {
	_, err := LoadCouncilRegistryYAML(testdataPath(t, "missing_role.yaml"), 0.7)
	if err == nil {
		t.Fatal("expected error for missing role, got nil")
	}
}

func TestLoadCouncilRegistryYAML_UnknownStrategy(t *testing.T) {
	_, err := LoadCouncilRegistryYAML(testdataPath(t, "unknown_strategy.yaml"), 0.7)
	if err == nil {
		t.Fatal("expected error for unknown strategy, got nil")
	}
}

func TestLoadCouncilRegistryYAML_DuplicateStrategy(t *testing.T) {
	_, err := LoadCouncilRegistryYAML(testdataPath(t, "duplicate_strategy.yaml"), 0.7)
	if err == nil {
		t.Fatal("expected error for duplicate strategy across two entries, got nil")
	}
}

func TestLoadCouncilRegistryYAML_MoAPartial(t *testing.T) {
	_, err := LoadCouncilRegistryYAML(testdataPath(t, "moa_partial.yaml"), 0.7)
	if err == nil {
		t.Fatal("expected error for MoA missing aggregators, got nil")
	}
}

func TestLoadCouncilRegistryYAML_UnknownField(t *testing.T) {
	_, err := LoadCouncilRegistryYAML(testdataPath(t, "unknown_field.yaml"), 0.7)
	if err == nil {
		t.Fatal("expected error for unknown field (typo'd 'arbitor'), got nil")
	}
}

func TestLoadCouncilRegistryYAML_EmptyCouncils(t *testing.T) {
	_, err := LoadCouncilRegistryYAML(testdataPath(t, "empty_councils.yaml"), 0.7)
	if err == nil {
		t.Fatal("expected error for empty councils map, got nil")
	}
}

func TestLoadCouncilRegistryYAML_Overrides(t *testing.T) {
	registry, err := LoadCouncilRegistryYAML(testdataPath(t, "overrides.yaml"), 0.7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr := registry["default"]
	if pr.Temperature != 0.3 {
		t.Errorf("default: Temperature = %v, want overridden 0.3", pr.Temperature)
	}
	if pr.QuorumMin != 5 {
		t.Errorf("default: QuorumMin = %d, want overridden 5", pr.QuorumMin)
	}

	grr := registry["generate-rank-refine"]
	if grr.RefineTopK != 2 {
		t.Errorf("generate-rank-refine: RefineTopK = %d, want overridden 2", grr.RefineTopK)
	}
}

func TestLoadCouncilRegistryYAML_FileNotFound(t *testing.T) {
	_, err := LoadCouncilRegistryYAML(testdataPath(t, "does_not_exist.yaml"), 0.7)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected error wrapping fs.ErrNotExist, got %v", err)
	}
}

// TestLoadCouncilRegistryYAML_ShippedConfig loads the actual configs/council.yaml
// shipped with the repo, so a hand-edit that breaks the real file (not just
// the test fixtures) fails CI.
func TestLoadCouncilRegistryYAML_ShippedConfig(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "council.yaml")
	registry, err := LoadCouncilRegistryYAML(path, 0.7)
	if err != nil {
		t.Fatalf("configs/council.yaml failed to load: %v", err)
	}

	wantKeys := []string{"default", "role-based", "majority", "generate-rank-refine", "debate", "moa", "delphi"}
	for _, k := range wantKeys {
		if _, ok := registry[k]; !ok {
			t.Errorf("configs/council.yaml missing expected key %q", k)
		}
	}
	if len(registry) != len(wantKeys) {
		t.Errorf("configs/council.yaml: len(registry) = %d, want %d", len(registry), len(wantKeys))
	}
}

// TestShippedConfigCoversAllStrategies is the YAML-path counterpart to
// internal/council's TestAllStrategiesRegisteredOrExempted, which only
// checks the env-var fallback registration path (registry_env.go). Since
// configs/council.yaml is now the PRIMARY registration mechanism whenever
// it's present, a Strategy declared in types.go but missing from this file
// would be silently unreachable in the common case — this test makes that
// structurally impossible to introduce unnoticed, the same way
// TestAllStrategiesRegisteredOrExempted already does for the fallback path.
func TestShippedConfigCoversAllStrategies(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "council.yaml")
	registry, err := LoadCouncilRegistryYAML(path, 0.7)
	if err != nil {
		t.Fatalf("configs/council.yaml failed to load: %v", err)
	}

	covered := make(map[council.Strategy]bool, len(registry))
	for _, ct := range registry {
		covered[ct.Strategy] = true
	}

	for _, strat := range council.AllStrategies() {
		if !covered[strat] {
			t.Errorf("configs/council.yaml has no entry for strategy %q — every declared "+
				"Strategy must have a registration in the shipped YAML config, since it's "+
				"the primary registration mechanism whenever present", strat)
		}
	}
}
