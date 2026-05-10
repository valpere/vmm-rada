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
