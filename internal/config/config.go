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

		LLMAPIMaxRetries: llmAPIMaxRetries,
	}, nil
}
