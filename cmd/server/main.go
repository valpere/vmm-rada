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
