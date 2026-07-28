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
	"github.com/valpere/vmm-rada/internal/api"
	"github.com/valpere/vmm-rada/internal/config"
	"github.com/valpere/vmm-rada/internal/council"
	"github.com/valpere/vmm-rada/internal/openrouter"
	"github.com/valpere/vmm-rada/internal/storage"
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
	logger.Info("LLM provider", "name", cfg.ProviderName, "base_url", cfg.LLMBaseURL)

	registry, err := config.BuildRegistry(cfg, logger)
	if err != nil {
		logger.Error("council registry error", "error", err)
		os.Exit(1)
	}

	cb := openrouter.NewCircuitBreaker(openrouter.CircuitBreakerConfig{
		FailureThreshold: cfg.CBFailureThreshold,
		WindowDuration:   time.Duration(cfg.CBWindowDurationSecs) * time.Second,
		ResetTimeout:     time.Duration(cfg.CBResetTimeoutSecs) * time.Second,
	})
	client := openrouter.NewClient(cfg.ProviderAPIKey, cfg.LLMBaseURL, 120*time.Second, cfg.LLMAPIMaxRetries, logger, cb)
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
