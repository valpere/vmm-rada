// Command eval runs the LLM Council evaluation harness — a manual,
// pre-merge regression detector that compares the council pipeline against
// a single-model baseline using an LLM-as-judge.
//
// Usage:
//
//	go run ./cmd/eval \
//	    -input internal/eval/testdata/golden.json \
//	    -out eval-results.json \
//	    -baseline-model openai/gpt-4o-mini \
//	    -council-type default
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
	"sort"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/valpere/llm-council/internal/config"
	"github.com/valpere/llm-council/internal/council"
	"github.com/valpere/llm-council/internal/eval"
	"github.com/valpere/llm-council/internal/openrouter"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		inputPath     = flag.String("input", "internal/eval/testdata/golden.json", "path to golden case JSON file")
		outPath       = flag.String("out", "eval-results.json", "path to write the results JSON envelope")
		baselineModel = flag.String("baseline-model", "", "single-model baseline (required)")
		judgeModel    = flag.String("judge-model", "", "judge model — defaults to chairman; MUST differ from baseline")
		councilType   = flag.String("council-type", "default", "council type registered in the server registry")
		seed          = flag.Int64("seed", 0, "RNG seed for A/B order (0 = time-based, captured into output meta)")
	)
	flag.Parse()

	if *baselineModel == "" {
		return fmt.Errorf("-baseline-model is required")
	}

	// Load .env so OPENROUTER_API_KEY etc. resolve in dev. Production
	// environments without a .env file are expected.
	_ = godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Default judge to chairman if -judge-model was not provided.
	jModel := *judgeModel
	if jModel == "" {
		jModel = cfg.DefaultCouncilChairmanModel
	}
	if jModel == *baselineModel {
		return fmt.Errorf("judge model %q must differ from baseline model to avoid self-preference bias; pass -judge-model explicitly", jModel)
	}

	// Read input file twice's worth of work in one pass: parse + hash. The
	// hash is echoed into the output meta so a flipped result can be replayed
	// against the exact same prompt set.
	data, err := os.ReadFile(*inputPath)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	suite, err := eval.LoadSuite(data)
	if err != nil {
		return err
	}
	if len(suite.Cases) == 0 {
		return fmt.Errorf("input %q contains zero cases", *inputPath)
	}

	chosenSeed := *seed
	if chosenSeed == 0 {
		chosenSeed = time.Now().UnixNano()
	}

	// Build the council pipeline using the same wiring shape as cmd/server.
	registry := map[string]council.CouncilType{
		cfg.DefaultCouncilType: {
			Name:          cfg.DefaultCouncilType,
			Strategy:      council.PeerReview,
			Models:        cfg.DefaultCouncilModels,
			ChairmanModel: cfg.DefaultCouncilChairmanModel,
			Temperature:   cfg.DefaultCouncilTemperature,
		},
		"code-review": council.NewCodeReviewCouncilType(
			cfg.CodeReviewModels,
			cfg.CodeReviewChairmanModel,
			cfg.DefaultCouncilTemperature,
		),
	}
	if _, ok := registry[*councilType]; !ok {
		return fmt.Errorf("unknown council type %q (known: %v)", *councilType, knownTypes(registry))
	}

	client := openrouter.NewClient(cfg.OpenRouterAPIKey, cfg.LLMBaseURL, 120*time.Second, cfg.LLMAPIMaxRetries, logger)
	runner := council.NewCouncil(client, registry, logger)

	// Honour SIGINT / SIGTERM mid-run — the harness is sequential, so
	// cancelling between cases is enough; no goroutine cleanup is needed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		InputSHA256:   eval.SuiteSHA256(data),
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
