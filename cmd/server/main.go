package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/valpere/llm-council/internal/api"
	"github.com/valpere/llm-council/internal/config"
	"github.com/valpere/llm-council/internal/council"
	"github.com/valpere/llm-council/internal/openrouter"
	"github.com/valpere/llm-council/internal/storage"
)

func main() {
	// Load .env if present; ignore error so production environments without a
	// .env file work normally.
	_ = godotenv.Load()

	// Initialise the JSON logger first so every subsequent slog call —
	// including those inside config.Load() — uses a consistent format.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}

	// Build the council type registry from config fields.
	registry := map[string]council.CouncilType{
		cfg.DefaultCouncilType: {
			Name:          cfg.DefaultCouncilType,
			Strategy:      council.PeerReview,
			Models:        cfg.DefaultCouncilModels,
			ChairmanModel: cfg.DefaultCouncilChairmanModel,
			Temperature:   cfg.DefaultCouncilTemperature,
		},
	}

	// Majority strategy registration is opt-in: it's only added to the
	// registry when MAJORITY_MODELS is explicitly set. Existing deployments
	// without the env var don't get the new council type silently exposed.
	//
	// The chairman model is genuinely optional. It is NOT defaulted to the
	// global CHAIRMAN_MODEL — config.Load() always populates that with a
	// dev-fallback, so falling back here would make Majority's no-chairman
	// path (verbatim winner emission, loud-error on tie) unreachable. Users
	// who want a chairman for tiebreak/polish must set MAJORITY_CHAIRMAN_MODEL.
	if len(cfg.MajorityModels) > 0 {
		registry["majority"] = council.CouncilType{
			Name:          "majority",
			Strategy:      council.Majority,
			Models:        cfg.MajorityModels,
			ChairmanModel: cfg.MajorityChairmanModel,
			Temperature:   cfg.DefaultCouncilTemperature,
		}
	}

	// GenerateRankRefine registration is opt-in AND requires both env vars.
	// Unlike Majority, this strategy has no no-LLM path — Stage 2 ranking is
	// always an arbiter call and Stage 3 refinement is always a chairman call.
	// If models are set but the chairman is missing, log a warning and skip
	// registration rather than fail at request time.
	if len(cfg.GenerateRankRefineModels) > 0 {
		if cfg.GenerateRankRefineChairmanModel == "" {
			logger.Warn("GENERATE_RANK_REFINE_MODELS set but GENERATE_RANK_REFINE_CHAIRMAN_MODEL is empty; skipping registration of \"generate-rank-refine\" council type")
		} else {
			registry["generate-rank-refine"] = council.CouncilType{
				Name:          "generate-rank-refine",
				Strategy:      council.GenerateRankRefine,
				Models:        cfg.GenerateRankRefineModels,
				ChairmanModel: cfg.GenerateRankRefineChairmanModel,
				Temperature:   cfg.DefaultCouncilTemperature,
			}
		}
	}

	// MultiAgentDebate registration is opt-in AND requires both env vars.
	// Stage 3 chairman always runs; no no-LLM path. DebateMaxRounds=0 is the
	// sentinel for "use runner default of 2".
	if len(cfg.DebateModels) > 0 {
		if cfg.DebateChairmanModel == "" {
			logger.Warn("DEBATE_MODELS set but DEBATE_CHAIRMAN_MODEL is empty; skipping registration of \"debate\" council type")
		} else {
			registry["debate"] = council.CouncilType{
				Name:            "debate",
				Strategy:        council.MultiAgentDebate,
				Models:          cfg.DebateModels,
				ChairmanModel:   cfg.DebateChairmanModel,
				Temperature:     cfg.DefaultCouncilTemperature,
				MaxDebateRounds: cfg.DebateMaxRounds,
			}
		}
	}

	// MixtureOfAgents registration is opt-in AND requires ALL THREE env vars.
	// MoA has no no-LLM path: every layer needs models. Models / ChairmanModel
	// are NOT used — the runner reads ProposerModels / AggregatorModels /
	// RefinerModel directly. Partial config is logged and skipped (not failed
	// at request time).
	if len(cfg.MoaProposerModels) > 0 || len(cfg.MoaAggregatorModels) > 0 || cfg.MoaRefinerModel != "" {
		switch {
		case len(cfg.MoaProposerModels) == 0:
			logger.Warn("MOA_AGGREGATOR_MODELS or MOA_REFINER_MODEL set but MOA_PROPOSER_MODELS is empty; skipping registration of \"moa\" council type")
		case len(cfg.MoaAggregatorModels) == 0:
			logger.Warn("MOA_PROPOSER_MODELS set but MOA_AGGREGATOR_MODELS is empty; skipping registration of \"moa\" council type")
		case cfg.MoaRefinerModel == "":
			logger.Warn("MOA_PROPOSER_MODELS / MOA_AGGREGATOR_MODELS set but MOA_REFINER_MODEL is empty; skipping registration of \"moa\" council type")
		default:
			registry["moa"] = council.CouncilType{
				Name:             "moa",
				Strategy:         council.MixtureOfAgents,
				ProposerModels:   cfg.MoaProposerModels,
				AggregatorModels: cfg.MoaAggregatorModels,
				RefinerModel:     cfg.MoaRefinerModel,
				Temperature:      cfg.DefaultCouncilTemperature,
			}
		}
	}

	// Delphi registration is opt-in AND requires both env vars. Stage 3
	// chairman always runs; no no-LLM path. DelphiMaxRounds=0 and
	// DelphiConvergenceThreshold=0 are sentinels for "use runner defaults"
	// (3 rounds, 0.1 threshold).
	if len(cfg.DelphiModels) > 0 {
		if cfg.DelphiChairmanModel == "" {
			logger.Warn("DELPHI_MODELS set but DELPHI_CHAIRMAN_MODEL is empty; skipping registration of \"delphi\" council type")
		} else {
			registry["delphi"] = council.CouncilType{
				Name:                       "delphi",
				Strategy:                   council.Delphi,
				Models:                     cfg.DelphiModels,
				ChairmanModel:              cfg.DelphiChairmanModel,
				Temperature:                cfg.DefaultCouncilTemperature,
				MaxDelphiRounds:            cfg.DelphiMaxRounds,
				DelphiConvergenceThreshold: cfg.DelphiConvergenceThreshold,
			}
		}
	}

	client := openrouter.NewClient(cfg.OpenRouterAPIKey, cfg.LLMBaseURL, 120*time.Second, cfg.LLMAPIMaxRetries, logger)
	runner := council.NewCouncil(client, registry, logger)

	store, err := storage.NewStore(cfg.DataDir, logger)
	if err != nil {
		logger.Error("storage init failed", "error", err)
		os.Exit(1)
	}

	clarificationCfg := council.ClarificationConfig{
		MaxRounds:            cfg.ClarificationMaxRounds,
		MaxTotalQuestions:    cfg.ClarificationMaxTotalQuestions,
		MaxQuestionsPerRound: cfg.ClarificationMaxQuestionsPerRound,
		Models:               cfg.ClarificationModels,
		ArbiterModel:         cfg.ClarificationArbiterModel,
	}
	handler := api.NewHandler(runner, runner, store, logger, cfg.DefaultCouncilType, clarificationCfg)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown: cancel context on SIGINT or SIGTERM, then drain
	// in-flight requests with a 10 s deadline.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// errCh receives the first non-ErrServerClosed error from ListenAndServe,
	// allowing the main goroutine to log and exit without skipping deferred cleanup.
	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}
