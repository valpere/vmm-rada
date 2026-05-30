// Package eval implements a manually-invoked evaluation harness that compares
// the output of the VMM Rada pipeline against a single-model baseline.
//
// The harness runs each prompt through both pipelines, asks a third model
// (acting as judge) to pick the better answer in a blinded pairwise
// comparison, and emits a JSON envelope of per-case results plus a summary
// counter.
//
// Scope: smallest possible regression detector for prompt/strategy changes.
// Not a research-grade evaluation framework. Costs real money — never wired
// into CI; trigger manually before any prompt-template, strategy, or
// default-models change.
package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"

	"github.com/valpere/vmm-rada/internal/council"
)

// Case is a single evaluation prompt loaded from the golden input file.
type Case struct {
	ID       string `json:"id"`
	Prompt   string `json:"prompt"`
	Category string `json:"category"`
}

// Suite is the set of cases the harness will run.
type Suite struct {
	Cases []Case `json:"-"`
}

// Verdict is the four-state judge outcome surfaced in Result.JudgeVerdict.
type Verdict string

const (
	VerdictCouncil  Verdict = "council"
	VerdictBaseline Verdict = "baseline"
	VerdictTie      Verdict = "tie"
	VerdictError    Verdict = "error"
)

// Result is the per-case record written to the output JSON.
type Result struct {
	ID                 string  `json:"id"`
	Prompt             string  `json:"prompt"`
	CouncilAnswer      string  `json:"council_answer"`
	BaselineAnswer     string  `json:"baseline_answer"`
	CouncilModel       string  `json:"council_model"`
	BaselineModel      string  `json:"baseline_model"`
	JudgeVerdict       Verdict `json:"judge_verdict"`
	JudgeExplanation   string  `json:"judge_explanation"`
	CouncilConsensusW  float64 `json:"council_consensus_w"`
	CouncilDurationMs  int64   `json:"council_duration_ms"`
	BaselineDurationMs int64   `json:"baseline_duration_ms"`
	Error              string  `json:"error,omitempty"`
}

// Meta is the run-level header echoed into the output JSON envelope. The
// seed and input_sha256 fields make a flipped result replayable.
type Meta struct {
	Seed          int64  `json:"seed"`
	InputSHA256   string `json:"input_sha256"`
	BaselineModel string `json:"baseline_model"`
	JudgeModel    string `json:"judge_model"`
	CouncilType   string `json:"council_type"`
}

// Output is the envelope serialised to disk.
type Output struct {
	Meta    Meta     `json:"meta"`
	Results []Result `json:"results"`
}

// Options bundles the dependencies and parameters for a single Run.
type Options struct {
	Suite         Suite
	Runner        council.Runner
	Client        council.LLMClient
	BaselineModel string
	JudgeModel    string
	CouncilType   string
	Temperature   float64
	Seed          int64
	Logger        *slog.Logger
}

// Run executes the suite sequentially against the configured pipelines.
// Per-case errors (council failure, baseline failure, judge parse failure)
// are recorded as VerdictError and do not abort the run; only a configuration
// error (missing dependency) returns a top-level error.
func Run(ctx context.Context, opts Options) ([]Result, error) {
	if opts.Runner == nil {
		return nil, errors.New("eval: Options.Runner is required")
	}
	if opts.Client == nil {
		return nil, errors.New("eval: Options.Client is required")
	}
	if opts.BaselineModel == "" {
		return nil, errors.New("eval: Options.BaselineModel is required")
	}
	if opts.JudgeModel == "" {
		return nil, errors.New("eval: Options.JudgeModel is required")
	}
	if opts.JudgeModel == opts.BaselineModel {
		return nil, fmt.Errorf("eval: judge model %q must differ from baseline model to avoid self-preference bias", opts.JudgeModel)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// rand/v2 PCG, seeded deterministically. The same seed + same input file
	// produces the same A/B-order assignments, which is what the meta header
	// promises.
	rng := rand.New(rand.NewPCG(uint64(opts.Seed), 0))
	judge := &Judge{Client: opts.Client, Model: opts.JudgeModel, Temperature: opts.Temperature}

	results := make([]Result, 0, len(opts.Suite.Cases))
	for _, c := range opts.Suite.Cases {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		results = append(results, runCase(ctx, c, opts, judge, rng, logger))
	}
	return results, nil
}

// runCase executes a single case end-to-end (council, baseline, judge) and
// returns the populated Result. It never returns an error — every failure
// path is captured in Result.Error / Result.JudgeVerdict so the orchestrator
// loop can continue to the next case.
func runCase(
	ctx context.Context,
	c Case,
	opts Options,
	judge *Judge,
	rng *rand.Rand,
	logger *slog.Logger,
) Result {
	r := Result{
		ID:            c.ID,
		Prompt:        c.Prompt,
		CouncilModel:  opts.CouncilType + ":synthesis",
		BaselineModel: opts.BaselineModel,
	}

	// Run both pipelines. If either errors, we record it and skip the judge —
	// there's nothing meaningful to compare.
	cAnswer, cConsensusW, cDur, cErr := runCouncil(ctx, opts.Runner, c.Prompt, opts.CouncilType)
	r.CouncilAnswer = cAnswer
	r.CouncilConsensusW = cConsensusW
	r.CouncilDurationMs = cDur
	if cErr != nil {
		r.JudgeVerdict = VerdictError
		r.Error = fmt.Sprintf("council error: %v", cErr)
		logger.Warn("eval: council error", "id", c.ID, "error", cErr)
		return r
	}

	bAnswer, bDur, bErr := runBaseline(ctx, opts.Client, c.Prompt, opts.BaselineModel, opts.Temperature)
	r.BaselineAnswer = bAnswer
	r.BaselineDurationMs = bDur
	if bErr != nil {
		r.JudgeVerdict = VerdictError
		r.Error = fmt.Sprintf("baseline error: %v", bErr)
		logger.Warn("eval: baseline error", "id", c.ID, "error", bErr)
		return r
	}

	// Decide A/B order per case via the seeded RNG. councilFirst==true means
	// Answer A is the council's answer; the judge's verdict is then remapped
	// back to council/baseline before being written into Result.
	councilFirst := rng.IntN(2) == 0
	answerA, answerB := cAnswer, bAnswer
	if !councilFirst {
		answerA, answerB = bAnswer, cAnswer
	}

	rawVerdict, explanation, err := judge.Compare(ctx, c.Prompt, answerA, answerB)
	r.JudgeExplanation = explanation
	if err != nil {
		r.JudgeVerdict = VerdictError
		r.Error = fmt.Sprintf("judge error: %v", err)
		logger.Warn("eval: judge error", "id", c.ID, "error", err)
		return r
	}

	r.JudgeVerdict = remapVerdict(rawVerdict, councilFirst)
	return r
}

// remapVerdict translates the judge's "A"/"B"/"tie" output back to the
// pipeline-named verdict, accounting for the per-case order randomisation.
func remapVerdict(raw string, councilFirst bool) Verdict {
	switch raw {
	case "tie":
		return VerdictTie
	case "A":
		if councilFirst {
			return VerdictCouncil
		}
		return VerdictBaseline
	case "B":
		if councilFirst {
			return VerdictBaseline
		}
		return VerdictCouncil
	default:
		return VerdictError
	}
}

// LoadSuite is a convenience helper for the cmd/eval CLI: it parses a JSON
// array of cases. It lives here rather than in cmd/ so tests can use it.
func LoadSuite(data []byte) (Suite, error) {
	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		return Suite{}, fmt.Errorf("parse cases: %w", err)
	}
	return Suite{Cases: cases}, nil
}

// SuiteSHA256 returns the lowercase hex SHA-256 of the raw input file bytes.
// Echoing this into the output meta lets a flipped run be replayed against
// the exact same input.
func SuiteSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
