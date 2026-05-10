package config

import (
	"os"
	"testing"
)

// setenv sets an env var for the duration of the test and restores the prior
// value (or unsets it) via t.Cleanup.
func setenv(t *testing.T, key, value string) {
	t.Helper()
	prev, hadPrev := os.LookupEnv(key)
	os.Setenv(key, value)
	t.Cleanup(func() {
		if hadPrev {
			os.Setenv(key, prev)
		} else {
			os.Unsetenv(key)
		}
	})
}

// unsetenv unsets an env var for the duration of the test and restores the
// prior value via t.Cleanup.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	prev, hadPrev := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if hadPrev {
			os.Setenv(key, prev)
		}
	})
}

// baseEnv sets the minimum required environment for config.Load() to succeed.
func baseEnv(t *testing.T) {
	t.Helper()
	setenv(t, "OPENROUTER_API_KEY", "sk-test")
}

// ── TestLoad_LLMBaseURL ────────────────────────────────────────────────────

func TestLoad_LLMBaseURL_Unset(t *testing.T) {
	baseEnv(t)
	unsetenv(t, "LLM_API_BASE_URL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLMBaseURL != "" {
		t.Errorf("LLMBaseURL: got %q, want %q", cfg.LLMBaseURL, "")
	}
}

func TestLoad_LLMBaseURL_ValidHTTPS(t *testing.T) {
	baseEnv(t)
	const target = "https://api.ollama.com/v1/chat/completions"
	setenv(t, "LLM_API_BASE_URL", target)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLMBaseURL != target {
		t.Errorf("LLMBaseURL: got %q, want %q", cfg.LLMBaseURL, target)
	}
}

func TestLoad_LLMBaseURL_ValidHTTP(t *testing.T) {
	baseEnv(t)
	const target = "http://localhost:11434/v1/chat/completions"
	setenv(t, "LLM_API_BASE_URL", target)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLMBaseURL != target {
		t.Errorf("LLMBaseURL: got %q, want %q", cfg.LLMBaseURL, target)
	}
}

func TestLoad_LLMBaseURL_InvalidScheme(t *testing.T) {
	baseEnv(t)
	setenv(t, "LLM_API_BASE_URL", "ftp://example.com/v1/chat/completions")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for ftp scheme, got nil")
	}
}

func TestLoad_LLMBaseURL_NotAURL(t *testing.T) {
	baseEnv(t)
	setenv(t, "LLM_API_BASE_URL", "not-a-url")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-URL value, got nil")
	}
}

// ── TestLoad_LLMAPIMaxRetries ─────────────────────────────────────────────

func TestLoad_LLMAPIMaxRetries(t *testing.T) {
	tests := []struct {
		name string
		// raw is the env value to set; if empty the var is unset.
		raw  string
		set  bool
		want int
	}{
		{"unset uses default", "", false, 2},
		{"valid 0 disables retries", "0", true, 0},
		{"valid 1", "1", true, 1},
		{"valid 5", "5", true, 5},
		{"empty string uses default", "", true, 2},
		{"negative falls back to default with warn", "-3", true, 2},
		{"non-numeric falls back to default with warn", "loads", true, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			if tc.set {
				setenv(t, "LLM_API_MAX_RETRIES", tc.raw)
			} else {
				unsetenv(t, "LLM_API_MAX_RETRIES")
			}
			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.LLMAPIMaxRetries != tc.want {
				t.Errorf("LLMAPIMaxRetries: got %d, want %d", cfg.LLMAPIMaxRetries, tc.want)
			}
		})
	}
}

// ── TestLoad_ClarificationModels ──────────────────────────────────────────
//
// The config loader must NOT pre-fill the Stage 0 model fields from
// COUNCIL_MODELS / CHAIRMAN_MODEL when the dedicated env vars are unset.
// Resolution is the runner's job; the config is just transport.

func TestLoad_ClarificationModels_BothSet(t *testing.T) {
	baseEnv(t)
	setenv(t, "CLARIFICATION_MODELS", "openai/gpt-4o-mini, anthropic/claude-haiku-4-5")
	setenv(t, "CLARIFICATION_ARBITER_MODEL", "anthropic/claude-sonnet-4-5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantModels := []string{"openai/gpt-4o-mini", "anthropic/claude-haiku-4-5"}
	if len(cfg.ClarificationModels) != len(wantModels) {
		t.Fatalf("ClarificationModels: got %v, want %v", cfg.ClarificationModels, wantModels)
	}
	for i, m := range wantModels {
		if cfg.ClarificationModels[i] != m {
			t.Errorf("ClarificationModels[%d]: got %q, want %q", i, cfg.ClarificationModels[i], m)
		}
	}
	if cfg.ClarificationArbiterModel != "anthropic/claude-sonnet-4-5" {
		t.Errorf("ClarificationArbiterModel: got %q, want %q", cfg.ClarificationArbiterModel, "anthropic/claude-sonnet-4-5")
	}
}

func TestLoad_ClarificationModels_GeneratorsSet_ArbiterUnset_LeavesArbiterEmpty(t *testing.T) {
	baseEnv(t)
	setenv(t, "CLARIFICATION_MODELS", "openai/gpt-4o-mini")
	unsetenv(t, "CLARIFICATION_ARBITER_MODEL")
	// Pre-fill CHAIRMAN_MODEL to prove the loader does NOT pre-fill from it.
	setenv(t, "CHAIRMAN_MODEL", "google/gemini-3.1-pro-preview")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ClarificationModels) != 1 || cfg.ClarificationModels[0] != "openai/gpt-4o-mini" {
		t.Errorf("ClarificationModels: got %v, want [openai/gpt-4o-mini]", cfg.ClarificationModels)
	}
	if cfg.ClarificationArbiterModel != "" {
		t.Errorf("ClarificationArbiterModel: got %q, want empty (resolution is the runner's job)", cfg.ClarificationArbiterModel)
	}
}

func TestLoad_ClarificationModels_ArbiterWhitespace_TreatedAsUnset(t *testing.T) {
	// Accidental whitespace-only value must not bypass the runner's fall-back
	// to ct.ChairmanModel. Loader trims and treats as empty.
	baseEnv(t)
	setenv(t, "CLARIFICATION_ARBITER_MODEL", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClarificationArbiterModel != "" {
		t.Errorf("ClarificationArbiterModel: got %q, want empty (whitespace must be trimmed to unset)", cfg.ClarificationArbiterModel)
	}
}

func TestLoad_ClarificationModels_BothUnset_FieldsEmpty(t *testing.T) {
	baseEnv(t)
	unsetenv(t, "CLARIFICATION_MODELS")
	unsetenv(t, "CLARIFICATION_ARBITER_MODEL")
	// Pre-fill council defaults to prove the loader does NOT pre-fill from them.
	setenv(t, "COUNCIL_MODELS", "model-a,model-b")
	setenv(t, "CHAIRMAN_MODEL", "chairman-z")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ClarificationModels) != 0 {
		t.Errorf("ClarificationModels: got %v, want empty (legacy fall-through path)", cfg.ClarificationModels)
	}
	if cfg.ClarificationArbiterModel != "" {
		t.Errorf("ClarificationArbiterModel: got %q, want empty (legacy fall-through path)", cfg.ClarificationArbiterModel)
	}
}

// ── TestLoad_MajorityModels ───────────────────────────────────────────────
//
// Setting MAJORITY_MODELS is what registers the "majority" council type at
// startup; the loader leaves the field empty when unset (no pre-fill from
// COUNCIL_MODELS). MAJORITY_CHAIRMAN_MODEL is optional.

func TestLoad_MajorityModels_BothSet(t *testing.T) {
	baseEnv(t)
	setenv(t, "MAJORITY_MODELS", "openai/gpt-4o-mini, anthropic/claude-haiku-4-5, google/gemini-flash-1.5")
	setenv(t, "MAJORITY_CHAIRMAN_MODEL", "anthropic/claude-sonnet-4-5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"openai/gpt-4o-mini", "anthropic/claude-haiku-4-5", "google/gemini-flash-1.5"}
	if len(cfg.MajorityModels) != len(want) {
		t.Fatalf("MajorityModels: got %v, want %v", cfg.MajorityModels, want)
	}
	for i, m := range want {
		if cfg.MajorityModels[i] != m {
			t.Errorf("MajorityModels[%d]: got %q, want %q", i, cfg.MajorityModels[i], m)
		}
	}
	if cfg.MajorityChairmanModel != "anthropic/claude-sonnet-4-5" {
		t.Errorf("MajorityChairmanModel: got %q, want %q", cfg.MajorityChairmanModel, "anthropic/claude-sonnet-4-5")
	}
}

func TestLoad_MajorityModels_GeneratorsSet_ChairmanUnset(t *testing.T) {
	baseEnv(t)
	setenv(t, "MAJORITY_MODELS", "openai/gpt-4o-mini")
	unsetenv(t, "MAJORITY_CHAIRMAN_MODEL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.MajorityModels) != 1 || cfg.MajorityModels[0] != "openai/gpt-4o-mini" {
		t.Errorf("MajorityModels: got %v, want [openai/gpt-4o-mini]", cfg.MajorityModels)
	}
	if cfg.MajorityChairmanModel != "" {
		t.Errorf("MajorityChairmanModel: got %q, want empty (chairman is optional for Majority)", cfg.MajorityChairmanModel)
	}
}

func TestLoad_MajorityModels_BothUnset(t *testing.T) {
	baseEnv(t)
	unsetenv(t, "MAJORITY_MODELS")
	unsetenv(t, "MAJORITY_CHAIRMAN_MODEL")
	// Pre-fill council defaults to prove the loader does NOT pre-fill from them.
	setenv(t, "COUNCIL_MODELS", "model-a,model-b")
	setenv(t, "CHAIRMAN_MODEL", "chairman-z")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.MajorityModels) != 0 {
		t.Errorf("MajorityModels: got %v, want empty (registration is opt-in)", cfg.MajorityModels)
	}
	if cfg.MajorityChairmanModel != "" {
		t.Errorf("MajorityChairmanModel: got %q, want empty", cfg.MajorityChairmanModel)
	}
}

// ── TestLoad_GenerateRankRefineModels ─────────────────────────────────────
//
// Both GENERATE_RANK_REFINE_MODELS and GENERATE_RANK_REFINE_CHAIRMAN_MODEL
// must be set for the council type to register (no no-LLM path). Loader
// leaves both fields empty when env vars unset (no pre-fill); the wiring
// in cmd/server/main.go decides whether to register based on both fields.

func TestLoad_GenerateRankRefineModels_BothSet(t *testing.T) {
	baseEnv(t)
	setenv(t, "GENERATE_RANK_REFINE_MODELS", "openai/gpt-4o-mini, anthropic/claude-haiku-4-5, google/gemini-flash-1.5, qwen/qwen3.6-plus")
	setenv(t, "GENERATE_RANK_REFINE_CHAIRMAN_MODEL", "anthropic/claude-sonnet-4-5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"openai/gpt-4o-mini", "anthropic/claude-haiku-4-5", "google/gemini-flash-1.5", "qwen/qwen3.6-plus"}
	if len(cfg.GenerateRankRefineModels) != len(want) {
		t.Fatalf("GenerateRankRefineModels: got %v, want %v", cfg.GenerateRankRefineModels, want)
	}
	for i, m := range want {
		if cfg.GenerateRankRefineModels[i] != m {
			t.Errorf("GenerateRankRefineModels[%d]: got %q, want %q", i, cfg.GenerateRankRefineModels[i], m)
		}
	}
	if cfg.GenerateRankRefineChairmanModel != "anthropic/claude-sonnet-4-5" {
		t.Errorf("GenerateRankRefineChairmanModel: got %q, want %q", cfg.GenerateRankRefineChairmanModel, "anthropic/claude-sonnet-4-5")
	}
}

func TestLoad_GenerateRankRefineModels_ModelsOnly_ChairmanEmpty(t *testing.T) {
	baseEnv(t)
	setenv(t, "GENERATE_RANK_REFINE_MODELS", "openai/gpt-4o-mini")
	unsetenv(t, "GENERATE_RANK_REFINE_CHAIRMAN_MODEL")
	// Loader does NOT pre-fill from CHAIRMAN_MODEL — registration site decides
	// whether the partial config is enough (it isn't, for this strategy).
	setenv(t, "CHAIRMAN_MODEL", "global-chairman")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.GenerateRankRefineModels) != 1 || cfg.GenerateRankRefineModels[0] != "openai/gpt-4o-mini" {
		t.Errorf("GenerateRankRefineModels: got %v, want [openai/gpt-4o-mini]", cfg.GenerateRankRefineModels)
	}
	if cfg.GenerateRankRefineChairmanModel != "" {
		t.Errorf("GenerateRankRefineChairmanModel: got %q, want empty (loader does not pre-fill from CHAIRMAN_MODEL)", cfg.GenerateRankRefineChairmanModel)
	}
}

func TestLoad_GenerateRankRefineModels_BothUnset(t *testing.T) {
	baseEnv(t)
	unsetenv(t, "GENERATE_RANK_REFINE_MODELS")
	unsetenv(t, "GENERATE_RANK_REFINE_CHAIRMAN_MODEL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.GenerateRankRefineModels) != 0 {
		t.Errorf("GenerateRankRefineModels: got %v, want empty", cfg.GenerateRankRefineModels)
	}
	if cfg.GenerateRankRefineChairmanModel != "" {
		t.Errorf("GenerateRankRefineChairmanModel: got %q, want empty", cfg.GenerateRankRefineChairmanModel)
	}
}

func TestLoad_GenerateRankRefineModels_ChairmanWhitespace_TreatedAsUnset(t *testing.T) {
	// Match CLARIFICATION_ARBITER_MODEL / MAJORITY_CHAIRMAN_MODEL behaviour:
	// whitespace-only chairman is trimmed to empty so the registration
	// gate fires correctly.
	baseEnv(t)
	setenv(t, "GENERATE_RANK_REFINE_MODELS", "m-a")
	setenv(t, "GENERATE_RANK_REFINE_CHAIRMAN_MODEL", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GenerateRankRefineChairmanModel != "" {
		t.Errorf("GenerateRankRefineChairmanModel: got %q, want empty (whitespace trim)", cfg.GenerateRankRefineChairmanModel)
	}
}

// ── TestLoad_DebateModels ─────────────────────────────────────────────────
//
// Both DEBATE_MODELS and DEBATE_CHAIRMAN_MODEL must be set for the council
// type to register (Stage 3 chairman always runs; no no-LLM path).
// DEBATE_MAX_ROUNDS is optional; 0 = use the runner's default of 2.

func TestLoad_DebateModels_BothSet(t *testing.T) {
	baseEnv(t)
	setenv(t, "DEBATE_MODELS", "openai/gpt-4o-mini, anthropic/claude-haiku-4-5, google/gemini-flash-1.5, qwen/qwen3.6-plus")
	setenv(t, "DEBATE_CHAIRMAN_MODEL", "anthropic/claude-sonnet-4-5")
	setenv(t, "DEBATE_MAX_ROUNDS", "3")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"openai/gpt-4o-mini", "anthropic/claude-haiku-4-5", "google/gemini-flash-1.5", "qwen/qwen3.6-plus"}
	if len(cfg.DebateModels) != len(want) {
		t.Fatalf("DebateModels: got %v, want %v", cfg.DebateModels, want)
	}
	for i, m := range want {
		if cfg.DebateModels[i] != m {
			t.Errorf("DebateModels[%d]: got %q, want %q", i, cfg.DebateModels[i], m)
		}
	}
	if cfg.DebateChairmanModel != "anthropic/claude-sonnet-4-5" {
		t.Errorf("DebateChairmanModel: got %q, want %q", cfg.DebateChairmanModel, "anthropic/claude-sonnet-4-5")
	}
	if cfg.DebateMaxRounds != 3 {
		t.Errorf("DebateMaxRounds: got %d, want 3", cfg.DebateMaxRounds)
	}
}

func TestLoad_DebateModels_ModelsOnly_ChairmanEmpty(t *testing.T) {
	baseEnv(t)
	setenv(t, "DEBATE_MODELS", "openai/gpt-4o-mini")
	unsetenv(t, "DEBATE_CHAIRMAN_MODEL")
	// Loader does NOT pre-fill from CHAIRMAN_MODEL — the registration site
	// at cmd/server/main.go decides whether the partial config is enough.
	setenv(t, "CHAIRMAN_MODEL", "global-chairman")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.DebateModels) != 1 || cfg.DebateModels[0] != "openai/gpt-4o-mini" {
		t.Errorf("DebateModels: got %v, want [openai/gpt-4o-mini]", cfg.DebateModels)
	}
	if cfg.DebateChairmanModel != "" {
		t.Errorf("DebateChairmanModel: got %q, want empty (loader does not pre-fill from CHAIRMAN_MODEL)", cfg.DebateChairmanModel)
	}
}

func TestLoad_DebateModels_BothUnset(t *testing.T) {
	baseEnv(t)
	unsetenv(t, "DEBATE_MODELS")
	unsetenv(t, "DEBATE_CHAIRMAN_MODEL")
	unsetenv(t, "DEBATE_MAX_ROUNDS")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.DebateModels) != 0 {
		t.Errorf("DebateModels: got %v, want empty", cfg.DebateModels)
	}
	if cfg.DebateChairmanModel != "" {
		t.Errorf("DebateChairmanModel: got %q, want empty", cfg.DebateChairmanModel)
	}
	if cfg.DebateMaxRounds != 0 {
		t.Errorf("DebateMaxRounds: got %d, want 0 (sentinel for runner default)", cfg.DebateMaxRounds)
	}
}

func TestLoad_DebateMaxRounds_Invalid_DefaultsToZero(t *testing.T) {
	// Invalid DEBATE_MAX_ROUNDS warns and falls back to 0 (which the runner
	// treats as the default 2). Mirrors how CLARIFICATION_MAX_* handle bad input.
	baseEnv(t)
	setenv(t, "DEBATE_MODELS", "m-a")
	setenv(t, "DEBATE_CHAIRMAN_MODEL", "chairman-z")
	setenv(t, "DEBATE_MAX_ROUNDS", "not-a-number")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DebateMaxRounds != 0 {
		t.Errorf("DebateMaxRounds: got %d, want 0 (invalid input → runner default)", cfg.DebateMaxRounds)
	}
}

// ── TestLoad_MoaModels ────────────────────────────────────────────────────
//
// All three MOA_* env vars must be set for the council type to register
// (no no-LLM path; every layer requires at least one model). Loader leaves
// fields empty when unset — the wiring in cmd/server/main.go decides whether
// to register.

func TestLoad_MoaModels_AllSet(t *testing.T) {
	baseEnv(t)
	setenv(t, "MOA_PROPOSER_MODELS", "openai/gpt-4o-mini, anthropic/claude-haiku-4-5, google/gemini-flash-1.5, qwen/qwen3.6-plus")
	setenv(t, "MOA_AGGREGATOR_MODELS", "anthropic/claude-sonnet-4-5, openai/gpt-4o")
	setenv(t, "MOA_REFINER_MODEL", "anthropic/claude-opus-4-7")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantProposers := []string{"openai/gpt-4o-mini", "anthropic/claude-haiku-4-5", "google/gemini-flash-1.5", "qwen/qwen3.6-plus"}
	if len(cfg.MoaProposerModels) != len(wantProposers) {
		t.Fatalf("MoaProposerModels: got %v, want %v", cfg.MoaProposerModels, wantProposers)
	}
	for i, m := range wantProposers {
		if cfg.MoaProposerModels[i] != m {
			t.Errorf("MoaProposerModels[%d]: got %q, want %q", i, cfg.MoaProposerModels[i], m)
		}
	}
	wantAggregators := []string{"anthropic/claude-sonnet-4-5", "openai/gpt-4o"}
	if len(cfg.MoaAggregatorModels) != len(wantAggregators) {
		t.Fatalf("MoaAggregatorModels: got %v, want %v", cfg.MoaAggregatorModels, wantAggregators)
	}
	for i, m := range wantAggregators {
		if cfg.MoaAggregatorModels[i] != m {
			t.Errorf("MoaAggregatorModels[%d]: got %q, want %q", i, cfg.MoaAggregatorModels[i], m)
		}
	}
	if cfg.MoaRefinerModel != "anthropic/claude-opus-4-7" {
		t.Errorf("MoaRefinerModel: got %q, want %q", cfg.MoaRefinerModel, "anthropic/claude-opus-4-7")
	}
}

func TestLoad_MoaModels_PartialConfig_FieldsPopulatedAsParsed(t *testing.T) {
	// Loader must NOT enforce all-or-nothing; the registration site at
	// cmd/server/main.go decides whether the partial config is enough.
	// Loader returns whatever was set, with empty fields for the unset vars.
	baseEnv(t)
	setenv(t, "MOA_PROPOSER_MODELS", "openai/gpt-4o-mini")
	setenv(t, "MOA_AGGREGATOR_MODELS", "anthropic/claude-sonnet-4-5")
	unsetenv(t, "MOA_REFINER_MODEL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.MoaProposerModels) != 1 || cfg.MoaProposerModels[0] != "openai/gpt-4o-mini" {
		t.Errorf("MoaProposerModels: got %v, want [openai/gpt-4o-mini]", cfg.MoaProposerModels)
	}
	if len(cfg.MoaAggregatorModels) != 1 || cfg.MoaAggregatorModels[0] != "anthropic/claude-sonnet-4-5" {
		t.Errorf("MoaAggregatorModels: got %v, want [anthropic/claude-sonnet-4-5]", cfg.MoaAggregatorModels)
	}
	if cfg.MoaRefinerModel != "" {
		t.Errorf("MoaRefinerModel: got %q, want empty", cfg.MoaRefinerModel)
	}
}

func TestLoad_MoaModels_AllUnset(t *testing.T) {
	baseEnv(t)
	unsetenv(t, "MOA_PROPOSER_MODELS")
	unsetenv(t, "MOA_AGGREGATOR_MODELS")
	unsetenv(t, "MOA_REFINER_MODEL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.MoaProposerModels) != 0 {
		t.Errorf("MoaProposerModels: got %v, want empty", cfg.MoaProposerModels)
	}
	if len(cfg.MoaAggregatorModels) != 0 {
		t.Errorf("MoaAggregatorModels: got %v, want empty", cfg.MoaAggregatorModels)
	}
	if cfg.MoaRefinerModel != "" {
		t.Errorf("MoaRefinerModel: got %q, want empty", cfg.MoaRefinerModel)
	}
}

func TestLoad_MoaRefiner_Whitespace_TreatedAsUnset(t *testing.T) {
	// Mirror the trim-to-empty pattern shared with CLARIFICATION_ARBITER_MODEL,
	// MAJORITY_CHAIRMAN_MODEL, GENERATE_RANK_REFINE_CHAIRMAN_MODEL,
	// DEBATE_CHAIRMAN_MODEL — so the registration gate fires correctly.
	baseEnv(t)
	setenv(t, "MOA_PROPOSER_MODELS", "m-a")
	setenv(t, "MOA_AGGREGATOR_MODELS", "m-b")
	setenv(t, "MOA_REFINER_MODEL", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MoaRefinerModel != "" {
		t.Errorf("MoaRefinerModel: got %q, want empty (whitespace trim)", cfg.MoaRefinerModel)
	}
}

// ── TestLoad_DelphiModels ─────────────────────────────────────────────────
//
// Both DELPHI_MODELS and DELPHI_CHAIRMAN_MODEL must be set for the council
// type to register (Stage 3 chairman always runs; no no-LLM path).
// DELPHI_MAX_ROUNDS and DELPHI_CONVERGENCE_THRESHOLD are optional; 0 = use
// the runner's defaults (3 rounds, 0.1 threshold).

func TestLoad_DelphiModels_AllSet(t *testing.T) {
	baseEnv(t)
	setenv(t, "DELPHI_MODELS", "openai/gpt-4o-mini, anthropic/claude-haiku-4-5, google/gemini-flash-1.5, qwen/qwen3.6-plus")
	setenv(t, "DELPHI_CHAIRMAN_MODEL", "anthropic/claude-sonnet-4-5")
	setenv(t, "DELPHI_MAX_ROUNDS", "5")
	setenv(t, "DELPHI_CONVERGENCE_THRESHOLD", "0.05")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"openai/gpt-4o-mini", "anthropic/claude-haiku-4-5", "google/gemini-flash-1.5", "qwen/qwen3.6-plus"}
	if len(cfg.DelphiModels) != len(want) {
		t.Fatalf("DelphiModels: got %v, want %v", cfg.DelphiModels, want)
	}
	for i, m := range want {
		if cfg.DelphiModels[i] != m {
			t.Errorf("DelphiModels[%d]: got %q, want %q", i, cfg.DelphiModels[i], m)
		}
	}
	if cfg.DelphiChairmanModel != "anthropic/claude-sonnet-4-5" {
		t.Errorf("DelphiChairmanModel: got %q, want %q", cfg.DelphiChairmanModel, "anthropic/claude-sonnet-4-5")
	}
	if cfg.DelphiMaxRounds != 5 {
		t.Errorf("DelphiMaxRounds: got %d, want 5", cfg.DelphiMaxRounds)
	}
	if cfg.DelphiConvergenceThreshold != 0.05 {
		t.Errorf("DelphiConvergenceThreshold: got %f, want 0.05", cfg.DelphiConvergenceThreshold)
	}
}

func TestLoad_DelphiModels_OptionalsUnset(t *testing.T) {
	baseEnv(t)
	setenv(t, "DELPHI_MODELS", "openai/gpt-4o-mini")
	setenv(t, "DELPHI_CHAIRMAN_MODEL", "anthropic/claude-sonnet-4-5")
	unsetenv(t, "DELPHI_MAX_ROUNDS")
	unsetenv(t, "DELPHI_CONVERGENCE_THRESHOLD")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.DelphiModels) != 1 || cfg.DelphiModels[0] != "openai/gpt-4o-mini" {
		t.Errorf("DelphiModels: got %v, want [openai/gpt-4o-mini]", cfg.DelphiModels)
	}
	if cfg.DelphiChairmanModel != "anthropic/claude-sonnet-4-5" {
		t.Errorf("DelphiChairmanModel: got %q", cfg.DelphiChairmanModel)
	}
	if cfg.DelphiMaxRounds != 0 {
		t.Errorf("DelphiMaxRounds: got %d, want 0 (sentinel for runner default)", cfg.DelphiMaxRounds)
	}
	if cfg.DelphiConvergenceThreshold != 0.0 {
		t.Errorf("DelphiConvergenceThreshold: got %f, want 0.0 (sentinel for runner default)", cfg.DelphiConvergenceThreshold)
	}
}

func TestLoad_DelphiModels_ModelsOnly_ChairmanEmpty(t *testing.T) {
	baseEnv(t)
	setenv(t, "DELPHI_MODELS", "openai/gpt-4o-mini")
	unsetenv(t, "DELPHI_CHAIRMAN_MODEL")
	// Loader does NOT pre-fill from CHAIRMAN_MODEL — registration site decides.
	setenv(t, "CHAIRMAN_MODEL", "global-chairman")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.DelphiModels) != 1 {
		t.Errorf("DelphiModels: got %v", cfg.DelphiModels)
	}
	if cfg.DelphiChairmanModel != "" {
		t.Errorf("DelphiChairmanModel: got %q, want empty (loader does not pre-fill from CHAIRMAN_MODEL)", cfg.DelphiChairmanModel)
	}
}

func TestLoad_DelphiModels_AllUnset(t *testing.T) {
	baseEnv(t)
	unsetenv(t, "DELPHI_MODELS")
	unsetenv(t, "DELPHI_CHAIRMAN_MODEL")
	unsetenv(t, "DELPHI_MAX_ROUNDS")
	unsetenv(t, "DELPHI_CONVERGENCE_THRESHOLD")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.DelphiModels) != 0 {
		t.Errorf("DelphiModels: got %v, want empty", cfg.DelphiModels)
	}
	if cfg.DelphiChairmanModel != "" {
		t.Errorf("DelphiChairmanModel: got %q, want empty", cfg.DelphiChairmanModel)
	}
	if cfg.DelphiMaxRounds != 0 {
		t.Errorf("DelphiMaxRounds: got %d, want 0", cfg.DelphiMaxRounds)
	}
	if cfg.DelphiConvergenceThreshold != 0.0 {
		t.Errorf("DelphiConvergenceThreshold: got %f, want 0.0", cfg.DelphiConvergenceThreshold)
	}
}

func TestLoad_DelphiConvergenceThreshold_Invalid(t *testing.T) {
	// Negative, ≥ 1.0, and non-numeric all fall back to 0 sentinel (= runner
	// default). Test each via a sub-table.
	tests := []struct {
		name string
		raw  string
	}{
		{"negative", "-0.1"},
		{"zero", "0"},
		{"one", "1.0"},
		{"greater than one", "1.5"},
		{"non-numeric", "convergent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			setenv(t, "DELPHI_MODELS", "m-a")
			setenv(t, "DELPHI_CHAIRMAN_MODEL", "chairman-z")
			setenv(t, "DELPHI_CONVERGENCE_THRESHOLD", tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.DelphiConvergenceThreshold != 0.0 {
				t.Errorf("invalid input %q: got %f, want 0.0 (runner default sentinel)", tc.raw, cfg.DelphiConvergenceThreshold)
			}
		})
	}
}
