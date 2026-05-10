package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds all server configuration sourced from environment variables.
// It contains raw primitive fields only — no domain types.
type Config struct {
	OpenRouterAPIKey            string
	LLMBaseURL                  string
	DataDir                     string
	DefaultCouncilType          string
	Port                        string
	DefaultCouncilModels        []string
	DefaultCouncilChairmanModel string
	DefaultCouncilTemperature   float64

	// Stage 0 clarification loop. ClarificationMaxRounds == 0 disables the feature (set CLARIFICATION_MAX_ROUNDS=0 to disable).
	ClarificationMaxRounds            int
	ClarificationMaxTotalQuestions    int
	ClarificationMaxQuestionsPerRound int

	// Stage 0 model overrides. Both fields are intentionally left empty when
	// their env vars are unset; the runner resolves the fall-through chain
	// (env → council type's models → error) at request time so the per-
	// council-type hop survives. Do NOT pre-fill from DefaultCouncilModels /
	// DefaultCouncilChairmanModel here.
	ClarificationModels       []string
	ClarificationArbiterModel string

	// Majority strategy registration. Setting MajorityModels is what registers
	// the "majority" council type — without it, the strategy ships compiled-in
	// but is not reachable via the API. MajorityChairmanModel is optional;
	// when empty, ties cause a loud error rather than silent tiebreak.
	MajorityModels        []string
	MajorityChairmanModel string

	// GenerateRankRefine strategy registration. BOTH GenerateRankRefineModels
	// AND GenerateRankRefineChairmanModel must be set for the council type to
	// be registered — unlike Majority, this strategy has no no-LLM path
	// (Stage 2 ranking and Stage 3 refinement are both LLM calls). When models
	// are set but chairman is missing, registration is skipped with a warn log.
	GenerateRankRefineModels        []string
	GenerateRankRefineChairmanModel string

	// MultiAgentDebate strategy registration. BOTH DebateModels AND
	// DebateChairmanModel must be set for the council type to be registered.
	// DebateMaxRounds is optional; 0 = use the runner's default of 2.
	// Cost note: this strategy fires N + N*R + 1 LLM calls per request; with
	// the default 4 debaters × 2 rounds + chairman that's 13 calls.
	DebateModels        []string
	DebateChairmanModel string
	DebateMaxRounds     int

	// MixtureOfAgents strategy registration. ALL THREE fields must be set for
	// the council type to be registered — no no-LLM path. Each layer needs at
	// least one model. Cost: N_proposers + N_aggregators + 1 LLM calls per
	// request (default 4 + 2 + 1 = 7 calls). Layer-specific fields, not the
	// generic Models / ChairmanModel — see CouncilType's field-usage matrix.
	MoaProposerModels   []string
	MoaAggregatorModels []string
	MoaRefinerModel     string

	// Delphi strategy registration. BOTH DelphiModels AND DelphiChairmanModel
	// must be set for the council type to be registered (Stage 3 chairman
	// always runs; no no-LLM path). DelphiMaxRounds and
	// DelphiConvergenceThreshold are optional; 0 = use the runner's defaults
	// (3 rounds, 0.1 threshold).
	// Cost note: this strategy fires N + N×R + 1 LLM calls in the worst
	// case (no convergence); with the default 4 raters × 3 rounds + chairman
	// that's 17 calls. Convergence at round 2 → 9 calls.
	DelphiModels               []string
	DelphiChairmanModel        string
	DelphiMaxRounds            int
	DelphiConvergenceThreshold float64

	// LLMAPIMaxRetries is the number of retries the OpenRouter client attempts
	// on transient failures (HTTP 429/502/503/504, network timeouts, EOFs).
	// 0 disables retries. Default: 2 (3 total attempts including the initial).
	LLMAPIMaxRetries int
}

// Load reads configuration from environment variables and returns an error if
// any required variable is missing. It never panics.
func Load() (*Config, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OPENROUTER_API_KEY is required but not set")
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data/conversations"
	}

	councilType := os.Getenv("DEFAULT_COUNCIL_TYPE")
	if councilType == "" {
		councilType = "default"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	var models []string
	if raw := os.Getenv("COUNCIL_MODELS"); raw != "" {
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				models = append(models, m)
			}
		}
	}
	if len(models) == 0 {
		slog.Warn("COUNCIL_MODELS not set; using local-dev fallback models")
		models = []string{
			"openai/gpt-4o-mini",
			"anthropic/claude-haiku-4-5",
			"google/gemini-flash-1.5",
		}
	}

	chairmanModel := os.Getenv("CHAIRMAN_MODEL")
	if chairmanModel == "" {
		slog.Warn("CHAIRMAN_MODEL not set; using local-dev fallback model")
		chairmanModel = "openai/gpt-4o-mini"
	}

	temperature := 0.7
	if raw := os.Getenv("DEFAULT_COUNCIL_TEMPERATURE"); raw != "" {
		if t, err := strconv.ParseFloat(raw, 64); err == nil {
			temperature = t
		} else {
			slog.Warn("DEFAULT_COUNCIL_TEMPERATURE is invalid; using fallback value",
				"value", raw, "error", err, "fallback", temperature)
		}
	}

	var llmBaseURL string
	if raw := strings.TrimSpace(os.Getenv("LLM_API_BASE_URL")); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "" {
			return nil, fmt.Errorf("LLM_API_BASE_URL must be a valid absolute http/https URL with a host, got %q", raw)
		}
		llmBaseURL = raw
	}

	clarificationMaxRounds := 2
	if raw := os.Getenv("CLARIFICATION_MAX_ROUNDS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			clarificationMaxRounds = v
		} else {
			slog.Warn("CLARIFICATION_MAX_ROUNDS is invalid; using fallback value",
				"value", raw, "error", err, "fallback", clarificationMaxRounds)
		}
	}

	clarificationMaxTotalQuestions := 5
	if raw := os.Getenv("CLARIFICATION_MAX_TOTAL_QUESTIONS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			clarificationMaxTotalQuestions = v
		} else {
			slog.Warn("CLARIFICATION_MAX_TOTAL_QUESTIONS is invalid; using fallback value",
				"value", raw, "error", err, "fallback", clarificationMaxTotalQuestions)
		}
	}

	clarificationMaxQuestionsPerRound := 3
	if raw := os.Getenv("CLARIFICATION_MAX_QUESTIONS_PER_ROUND"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			clarificationMaxQuestionsPerRound = v
		} else {
			slog.Warn("CLARIFICATION_MAX_QUESTIONS_PER_ROUND is invalid; using fallback value",
				"value", raw, "error", err, "fallback", clarificationMaxQuestionsPerRound)
		}
	}

	// Stage 0 generator pool. Empty slice when unset — runner resolves the
	// fall-through to the council type's Models. Comma-separated list with
	// whitespace trim; single-model `CLARIFICATION_MODELS=foo/bar` is the
	// common case (yields a 1-element slice).
	var clarificationModels []string
	if raw := os.Getenv("CLARIFICATION_MODELS"); raw != "" {
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				clarificationModels = append(clarificationModels, m)
			}
		}
	}

	// Stage 0 arbiter. Empty string when unset — runner resolves the
	// fall-through to the council type's ChairmanModel. Trim whitespace so
	// an accidentally-spaced value (e.g. "   ") is treated as unset rather
	// than as a non-empty model ID that would skip the fall-back.
	clarificationArbiterModel := strings.TrimSpace(os.Getenv("CLARIFICATION_ARBITER_MODEL"))

	// Majority strategy generator pool. Empty slice when unset — registration
	// in cmd/server/main.go uses non-empty as the opt-in signal.
	var majorityModels []string
	if raw := os.Getenv("MAJORITY_MODELS"); raw != "" {
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				majorityModels = append(majorityModels, m)
			}
		}
	}

	// Majority strategy chairman (optional — for tiebreak/polish). Trim like
	// CLARIFICATION_ARBITER_MODEL so accidental whitespace doesn't bypass the
	// "no chairman" loud-error path on ties.
	majorityChairmanModel := strings.TrimSpace(os.Getenv("MAJORITY_CHAIRMAN_MODEL"))

	// GenerateRankRefine generator pool. Empty slice when unset — registration
	// in cmd/server/main.go also requires the chairman var, otherwise the
	// strategy can't run (no no-LLM path).
	var generateRankRefineModels []string
	if raw := os.Getenv("GENERATE_RANK_REFINE_MODELS"); raw != "" {
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				generateRankRefineModels = append(generateRankRefineModels, m)
			}
		}
	}

	// GenerateRankRefine chairman (required when models are set; whitespace-trim
	// to match CLARIFICATION_ARBITER_MODEL / MAJORITY_CHAIRMAN_MODEL).
	generateRankRefineChairmanModel := strings.TrimSpace(os.Getenv("GENERATE_RANK_REFINE_CHAIRMAN_MODEL"))

	// MultiAgentDebate generator pool. Empty slice when unset — registration
	// requires both this and the chairman var (no no-LLM path; Stage 3 chairman
	// always runs).
	var debateModels []string
	if raw := os.Getenv("DEBATE_MODELS"); raw != "" {
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				debateModels = append(debateModels, m)
			}
		}
	}

	// MultiAgentDebate chairman (required when models are set; whitespace-trim).
	debateChairmanModel := strings.TrimSpace(os.Getenv("DEBATE_CHAIRMAN_MODEL"))

	// MultiAgentDebate round budget. 0 = let the runner use its default of 2.
	// Invalid values warn + use 0 (mirrors how CLARIFICATION_MAX_* handle bad input).
	debateMaxRounds := 0
	if raw := os.Getenv("DEBATE_MAX_ROUNDS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			debateMaxRounds = v
		} else {
			slog.Warn("DEBATE_MAX_ROUNDS is invalid; using runner default",
				"value", raw, "error", err)
		}
	}

	// MixtureOfAgents proposer pool. Empty slice when unset — registration
	// requires all three MoA env vars (no no-LLM path; every layer needs models).
	var moaProposerModels []string
	if raw := os.Getenv("MOA_PROPOSER_MODELS"); raw != "" {
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				moaProposerModels = append(moaProposerModels, m)
			}
		}
	}

	// MixtureOfAgents aggregator pool. Same opt-in semantics as proposers.
	var moaAggregatorModels []string
	if raw := os.Getenv("MOA_AGGREGATOR_MODELS"); raw != "" {
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				moaAggregatorModels = append(moaAggregatorModels, m)
			}
		}
	}

	// MixtureOfAgents refiner (Layer 3, single model). Whitespace-trimmed so an
	// accidentally-spaced value doesn't bypass the registration gate.
	moaRefinerModel := strings.TrimSpace(os.Getenv("MOA_REFINER_MODEL"))

	// Delphi rater pool. Empty when unset — registration requires both this
	// AND the chairman var (Stage 3 chairman always runs).
	var delphiModels []string
	if raw := os.Getenv("DELPHI_MODELS"); raw != "" {
		for _, m := range strings.Split(raw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				delphiModels = append(delphiModels, m)
			}
		}
	}

	// Delphi chairman (required when models are set; whitespace-trim).
	delphiChairmanModel := strings.TrimSpace(os.Getenv("DELPHI_CHAIRMAN_MODEL"))

	// Delphi round budget. 0 = let the runner use its default of 3.
	// Invalid values warn + use 0 sentinel. Must be ≥1 to be valid.
	delphiMaxRounds := 0
	if raw := os.Getenv("DELPHI_MAX_ROUNDS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			delphiMaxRounds = v
		} else {
			slog.Warn("DELPHI_MAX_ROUNDS is invalid; using runner default",
				"value", raw, "error", err)
		}
	}

	// Delphi convergence threshold. 0 = let the runner use its default of 0.1.
	// Valid range is (0.0, 1.0). Outside that range or non-numeric → warn + 0.
	delphiConvergenceThreshold := 0.0
	if raw := strings.TrimSpace(os.Getenv("DELPHI_CONVERGENCE_THRESHOLD")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0.0 && v < 1.0 {
			delphiConvergenceThreshold = v
		} else {
			slog.Warn("DELPHI_CONVERGENCE_THRESHOLD is invalid (must be in (0.0, 1.0)); using runner default",
				"value", raw, "error", err)
		}
	}

	llmAPIMaxRetries := 2
	if raw := os.Getenv("LLM_API_MAX_RETRIES"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			llmAPIMaxRetries = v
		} else {
			slog.Warn("LLM_API_MAX_RETRIES is invalid; using fallback value",
				"value", raw, "error", err, "fallback", llmAPIMaxRetries)
		}
	}

	return &Config{
		OpenRouterAPIKey:            apiKey,
		LLMBaseURL:                  llmBaseURL,
		DataDir:                     dataDir,
		DefaultCouncilType:          councilType,
		Port:                        port,
		DefaultCouncilModels:        models,
		DefaultCouncilChairmanModel: chairmanModel,
		DefaultCouncilTemperature:   temperature,

		ClarificationMaxRounds:            clarificationMaxRounds,
		ClarificationMaxTotalQuestions:    clarificationMaxTotalQuestions,
		ClarificationMaxQuestionsPerRound: clarificationMaxQuestionsPerRound,
		ClarificationModels:               clarificationModels,
		ClarificationArbiterModel:         clarificationArbiterModel,

		MajorityModels:        majorityModels,
		MajorityChairmanModel: majorityChairmanModel,

		GenerateRankRefineModels:        generateRankRefineModels,
		GenerateRankRefineChairmanModel: generateRankRefineChairmanModel,

		DebateModels:        debateModels,
		DebateChairmanModel: debateChairmanModel,
		DebateMaxRounds:     debateMaxRounds,

		MoaProposerModels:   moaProposerModels,
		MoaAggregatorModels: moaAggregatorModels,
		MoaRefinerModel:     moaRefinerModel,

		DelphiModels:               delphiModels,
		DelphiChairmanModel:        delphiChairmanModel,
		DelphiMaxRounds:            delphiMaxRounds,
		DelphiConvergenceThreshold: delphiConvergenceThreshold,

		LLMAPIMaxRetries: llmAPIMaxRetries,
	}, nil
}
