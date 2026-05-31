// Command eval runs the VMM Rada evaluation harness — a manual,
// pre-merge regression detector that compares the council pipeline against
// a single-model baseline using an LLM-as-judge.
//
// Modes:
//
//	Single question:
//	  go run ./cmd/eval -question "What is 2+2?" -baseline-model gpt-4o-mini
//
//	Batch benchmark (YAML):
//	  go run ./cmd/eval -benchmark eval/benchmarks/baseline.yaml \
//	      -baseline-model gpt-4o-mini
//
//	Legacy JSON suite:
//	  go run ./cmd/eval -input internal/eval/testdata/golden.json \
//	      -out eval-results.json -baseline-model gpt-4o-mini
//
// Costs real money (~$1–2 per pass with the balanced model preset). Run
// before any prompt-template, strategy, or default-models change.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/valpere/vmm-rada/internal/config"
	"github.com/valpere/vmm-rada/internal/council"
	"github.com/valpere/vmm-rada/internal/eval"
	"github.com/valpere/vmm-rada/internal/openrouter"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		inputPath     = flag.String("input", "", "path to golden case JSON file (legacy mode)")
		outPath       = flag.String("out", "eval-results.json", "path to write results JSON (legacy mode)")
		baselineModel = flag.String("baseline-model", "", "single-model baseline (required)")
		judgeModel    = flag.String("judge-model", "", "judge model — defaults to chairman; MUST differ from baseline")
		councilType   = flag.String("council-type", "default", "council type registered in the server registry")
		seed          = flag.Int64("seed", 0, "RNG seed for A/B order (0 = time-based, captured into output meta)")
		question      = flag.String("question", "", "run a single question ad-hoc")
		benchmarkFile = flag.String("benchmark", "", "path to YAML benchmark file for batch mode")
	)
	flag.Parse()

	if *baselineModel == "" {
		return fmt.Errorf("-baseline-model is required")
	}

	// Load .env so AI_PROVIDER_API_KEY etc. resolve in dev. Production
	// environments without a .env file are expected.
	_ = godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger.Info("LLM provider", "name", cfg.ProviderName, "base_url", cfg.LLMBaseURL)

	// Default judge to chairman if -judge-model was not provided.
	jModel := *judgeModel
	if jModel == "" {
		jModel = cfg.DefaultCouncilChairmanModel
	}
	if jModel == *baselineModel {
		return fmt.Errorf("judge model %q must differ from baseline model to avoid self-preference bias; pass -judge-model explicitly", jModel)
	}

	chosenSeed := *seed
	if chosenSeed == 0 {
		chosenSeed = time.Now().UnixNano()
	}

	// Honour SIGINT / SIGTERM mid-run — the harness is sequential, so
	// cancelling between cases is enough; no goroutine cleanup is needed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Mode dispatch ────────────────────────────────────────────────────────
	// Priority: --benchmark > --question > --input (legacy JSON)
	if *benchmarkFile != "" {
		return runBatch(ctx, *benchmarkFile, *baselineModel, jModel, *councilType, chosenSeed, cfg, logger)
	}

	var (
		suite    eval.Suite
		inputSHA string
	)
	if *question != "" {
		suite = eval.Suite{Cases: []eval.Case{{ID: "adhoc-001", Prompt: *question, Category: "adhoc"}}}
		inputSHA = eval.SuiteSHA256([]byte(*question))
	} else {
		// Legacy JSON suite mode.
		if *inputPath == "" {
			return fmt.Errorf("one of -question, -benchmark, or -input is required")
		}
		data, err := os.ReadFile(*inputPath)
		if err != nil {
			return fmt.Errorf("read input: %w", err)
		}
		suite, err = eval.LoadSuite(data)
		if err != nil {
			return err
		}
		if len(suite.Cases) == 0 {
			return fmt.Errorf("input %q contains zero cases", *inputPath)
		}
		inputSHA = eval.SuiteSHA256(data)
	}

	registry := buildRegistry(cfg)
	if _, ok := registry[*councilType]; !ok {
		return fmt.Errorf("unknown council type %q (known: %v)", *councilType, knownTypes(registry))
	}

	cb := openrouter.NewCircuitBreaker(openrouter.CircuitBreakerConfig{
		FailureThreshold: cfg.CBFailureThreshold,
		WindowDuration:   time.Duration(cfg.CBWindowDurationSecs) * time.Second,
		ResetTimeout:     time.Duration(cfg.CBResetTimeoutSecs) * time.Second,
	})
	client := openrouter.NewClient(cfg.ProviderAPIKey, cfg.LLMBaseURL, 120*time.Second, cfg.LLMAPIMaxRetries, logger, cb)
	runner := council.NewCouncil(client, registry, logger)

	results, err := eval.Run(ctx, eval.Options{
		Suite:         suite,
		Runner:        runner,
		Client:        client,
		BaselineModel: *baselineModel,
		JudgeModel:    jModel,
		CouncilType:   *councilType,
		Temperature:   cfg.DefaultCouncilTemperature,
		Seed:          chosenSeed,
		Logger:        logger,
	})
	if err != nil {
		return fmt.Errorf("run suite: %w", err)
	}

	meta := eval.Meta{
		Seed:          chosenSeed,
		InputSHA256:   inputSHA,
		BaselineModel: *baselineModel,
		JudgeModel:    jModel,
		CouncilType:   *councilType,
	}

	out, err := os.Create(*outPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer out.Close()
	if err := eval.WriteOutput(out, meta, results); err != nil {
		return err
	}

	summary := eval.Aggregate(results)
	if _, err := fmt.Fprintf(os.Stdout, "%s\n", summary.Format(len(results))); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	if _, err := fmt.Fprintf(os.Stderr, "wrote %s (seed=%d, sha256=%s)\n", *outPath, chosenSeed, meta.InputSHA256); err != nil {
		return fmt.Errorf("write stderr: %w", err)
	}
	return nil
}

// runBatch loads a YAML benchmark file and runs all items sequentially,
// writing a BatchReport to runs/<timestamp>/report.json. Progress is
// printed to stderr after each item. Aborts if EVAL_MAX_COST_USD is exceeded.
func runBatch(
	ctx context.Context,
	benchmarkPath, baselineModel, judgeModel, councilType string,
	seed int64,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	data, err := os.ReadFile(benchmarkPath)
	if err != nil {
		return fmt.Errorf("read benchmark: %w", err)
	}
	items, err := eval.LoadBenchmark(data)
	if err != nil {
		return fmt.Errorf("parse benchmark: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("benchmark %q contains zero items", benchmarkPath)
	}

	maxCost := 1.0
	if raw := os.Getenv("EVAL_MAX_COST_USD"); raw != "" {
		if v, fErr := fmt.Sscanf(raw, "%f", &maxCost); v != 1 || fErr != nil {
			logger.Warn("EVAL_MAX_COST_USD is invalid; using fallback value", "value", raw, "fallback", maxCost)
		}
	}

	registry := buildRegistry(cfg)
	if _, ok := registry[councilType]; !ok {
		return fmt.Errorf("unknown council type %q (known: %v)", councilType, knownTypes(registry))
	}

	cb := openrouter.NewCircuitBreaker(openrouter.CircuitBreakerConfig{
		FailureThreshold: cfg.CBFailureThreshold,
		WindowDuration:   time.Duration(cfg.CBWindowDurationSecs) * time.Second,
		ResetTimeout:     time.Duration(cfg.CBResetTimeoutSecs) * time.Second,
	})
	baseClient := openrouter.NewClient(cfg.ProviderAPIKey, cfg.LLMBaseURL, 120*time.Second, cfg.LLMAPIMaxRetries, logger, cb)
	meter := eval.NewMeteringClient(baseClient)
	runner := council.NewCouncil(meter, registry, logger)

	startedAt := time.Now()
	runID := startedAt.Format("20060102-150405")

	outDir := filepath.Join("runs", runID)
	if err := os.MkdirAll(outDir, 0750); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Convert benchmark items to eval cases.
	cases := make([]eval.Case, len(items))
	for i, it := range items {
		cases[i] = eval.Case{ID: it.ID, Prompt: it.Question, Category: it.Category}
	}
	suite := eval.Suite{Cases: cases}

	results := make([]eval.Result, 0, len(items))
	for i, c := range suite.Cases {
		if err := ctx.Err(); err != nil {
			return err
		}
		itemStart := time.Now()
		r := eval.RunSingleCase(ctx, c, eval.Options{
			Suite:         eval.Suite{Cases: []eval.Case{c}},
			Runner:        runner,
			Client:        meter,
			BaselineModel: baselineModel,
			JudgeModel:    judgeModel,
			CouncilType:   councilType,
			Temperature:   cfg.DefaultCouncilTemperature,
			Seed:          seed,
			Logger:        logger,
		})
		results = append(results, r)
		elapsed := time.Since(itemStart).Seconds()
		_, _, costSoFar := meter.Totals()
		fmt.Fprintf(os.Stderr, "[%d/%d] %s %s (%.1fs)\n",
			i+1, len(items), c.ID, r.JudgeVerdict, elapsed)

		if costSoFar > maxCost {
			logger.Warn("EVAL_MAX_COST_USD exceeded — aborting",
				"cost_usd", costSoFar, "limit_usd", maxCost, "completed", i+1)
			break
		}

		if i < len(items)-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}

	pt, ct, totalCost := meter.Totals()
	report := eval.BatchReport{
		RunID:         runID,
		BenchmarkFile: benchmarkPath,
		StartedAt:     startedAt,
		FinishedAt:    time.Now(),
		Meta: eval.Meta{
			Seed:          seed,
			InputSHA256:   eval.SuiteSHA256(data),
			BaselineModel: baselineModel,
			JudgeModel:    judgeModel,
			CouncilType:   councilType,
		},
		Results:      results,
		Summary:      eval.Aggregate(results),
		TotalTokens:  pt + ct,
		TotalCostUSD: totalCost,
	}

	outPath := filepath.Join(outDir, "report.json")
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	defer f.Close()
	if err := eval.WriteBatchReport(f, report); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "%s\n", report.Summary.Format(len(results)))
	fmt.Fprintf(os.Stderr, "wrote %s (tokens=%d cost=$%.4f)\n", outPath, report.TotalTokens, totalCost)
	return nil
}

// buildRegistry constructs the council type registry from cfg, mirroring
// cmd/server/main.go. Extracted here so runBatch can use it without
// duplicating the long opt-in registration block.
func buildRegistry(cfg *config.Config) map[string]council.CouncilType {
	registry := map[string]council.CouncilType{
		cfg.DefaultCouncilType: {
			Name:          cfg.DefaultCouncilType,
			Strategy:      council.PeerReview,
			Models:        cfg.DefaultCouncilModels,
			ChairmanModel: cfg.DefaultCouncilChairmanModel,
			Temperature:   cfg.DefaultCouncilTemperature,
		},
	}
	if len(cfg.MajorityModels) > 0 {
		registry["majority"] = council.CouncilType{
			Name:          "majority",
			Strategy:      council.Majority,
			Models:        cfg.MajorityModels,
			ChairmanModel: cfg.MajorityChairmanModel,
			Temperature:   cfg.DefaultCouncilTemperature,
		}
	}
	if len(cfg.GenerateRankRefineModels) > 0 && cfg.GenerateRankRefineChairmanModel != "" {
		registry["generate-rank-refine"] = council.CouncilType{
			Name:          "generate-rank-refine",
			Strategy:      council.GenerateRankRefine,
			Models:        cfg.GenerateRankRefineModels,
			ChairmanModel: cfg.GenerateRankRefineChairmanModel,
			Temperature:   cfg.DefaultCouncilTemperature,
		}
	}
	if len(cfg.DebateModels) > 0 && cfg.DebateChairmanModel != "" {
		registry["debate"] = council.CouncilType{
			Name:            "debate",
			Strategy:        council.MultiAgentDebate,
			Models:          cfg.DebateModels,
			ChairmanModel:   cfg.DebateChairmanModel,
			Temperature:     cfg.DefaultCouncilTemperature,
			MaxDebateRounds: cfg.DebateMaxRounds,
		}
	}
	if len(cfg.MoaProposerModels) > 0 && len(cfg.MoaAggregatorModels) > 0 && cfg.MoaRefinerModel != "" {
		registry["moa"] = council.CouncilType{
			Name:             "moa",
			Strategy:         council.MixtureOfAgents,
			ProposerModels:   cfg.MoaProposerModels,
			AggregatorModels: cfg.MoaAggregatorModels,
			RefinerModel:     cfg.MoaRefinerModel,
			Temperature:      cfg.DefaultCouncilTemperature,
		}
	}
	if len(cfg.DelphiModels) > 0 && cfg.DelphiChairmanModel != "" {
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
	return registry
}

// knownTypes returns the keys of registry sorted lexicographically — used
// only for diagnostic error messages, but determinism matters here so a
// failing CI run always blames the same line of "expected: [a b c]" output.
func knownTypes(registry map[string]council.CouncilType) []string {
	keys := make([]string, 0, len(registry))
	for k := range registry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
