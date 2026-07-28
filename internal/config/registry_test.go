package config

import (
	"io"
	"log/slog"
	"testing"

	"github.com/valpere/vmm-rada/internal/council"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestBuildRegistry_NoYAMLFile_FallsBackToEnv is the regression check for
// cmd/eval's pre-existing bug: its old buildRegistry duplicate omitted
// "role-based" entirely. BuildRegistry now serves both binaries from one
// function, so this asserts role-based IS present via the env fallback path.
func TestBuildRegistry_NoYAMLFile_FallsBackToEnv(t *testing.T) {
	cfg := &Config{
		CouncilConfigPath:           "testdata/does_not_exist.yaml",
		DefaultCouncilType:          "default",
		DefaultCouncilModels:        []string{"model-a", "model-b"},
		DefaultCouncilChairmanModel: "chairman",
		DefaultCouncilTemperature:   0.7,
		RoleBasedModels:             []string{"model-a", "model-b", "model-c", "model-d", "model-e"},
		RoleBasedChairmanModel:      "role-chairman",
	}

	registry, err := BuildRegistry(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := registry["default"]; !ok {
		t.Error("registry missing \"default\" (PeerReview)")
	}
	if _, ok := registry["role-based"]; !ok {
		t.Error("registry missing \"role-based\" — this is exactly the bug that existed in cmd/eval's old duplicate buildRegistry")
	}
	if registry["role-based"].Strategy != council.RoleBased {
		t.Errorf("role-based: Strategy = %v, want RoleBased", registry["role-based"].Strategy)
	}
}

func TestBuildRegistry_EmptyCouncilConfigPath_ForcesEnvEvenIfFileExists(t *testing.T) {
	cfg := &Config{
		CouncilConfigPath:           "", // explicit empty — force env fallback
		DefaultCouncilType:          "default",
		DefaultCouncilModels:        []string{"model-a"},
		DefaultCouncilChairmanModel: "chairman",
		DefaultCouncilTemperature:   0.7,
	}

	registry, err := BuildRegistry(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only "default" should be present — no per-strategy env vars were set,
	// so the env fallback registers nothing else, even though
	// testdata/valid_full.yaml exists on disk right next to this test.
	if len(registry) != 1 {
		t.Errorf("len(registry) = %d, want 1 (env fallback with only DefaultCouncilModels set)", len(registry))
	}
	if _, ok := registry["default"]; !ok {
		t.Error("registry missing \"default\"")
	}
}

func TestBuildRegistry_MalformedYAML_ErrorsNoFallback(t *testing.T) {
	cfg := &Config{
		CouncilConfigPath:  "testdata/missing_role.yaml", // exists, but fails validation
		DefaultCouncilType: "default",
		// No env vars set at all — if this fell back to env silently, the
		// registry would be non-empty (DefaultCouncilModels always yields a
		// "default" entry, per config.Load()'s local-dev fallback). Asserting
		// the call errors, rather than asserting an empty registry, is the
		// precise check for "no silent fallback on a broken authored file".
	}

	_, err := BuildRegistry(cfg, discardLogger())
	if err == nil {
		t.Fatal("expected error for malformed YAML file, got nil (must not silently fall back to env)")
	}
}

func TestBuildRegistry_ValidYAMLFile_UsesYAML(t *testing.T) {
	cfg := &Config{
		CouncilConfigPath:         "testdata/valid_full.yaml",
		DefaultCouncilTemperature: 0.7,
		// Env vars deliberately left unset/different from the YAML content —
		// if the env fallback were used instead, this would produce a very
		// different registry (e.g. no "moa" key at all).
	}

	registry, err := BuildRegistry(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := registry["moa"]; !ok {
		t.Error("registry missing \"moa\" — YAML file should have been used, not the (empty) env fallback")
	}
	if len(registry) != 7 {
		t.Errorf("len(registry) = %d, want 7 (all strategies from valid_full.yaml)", len(registry))
	}
}
