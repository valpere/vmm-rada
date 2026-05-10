package council

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"
)

// defaultDelphiCriteria is the v1 fixed set of rating dimensions. A subset of
// GenerateRankRefine's criteria — `originality` is dropped because it's a
// generation-time concern, not a rating-time one. Per-registration overrides
// are deferred to a future variant.
var defaultDelphiCriteria = []string{"correctness", "clarity", "completeness"}

// Delphi defaults used when CouncilType fields are zero-valued.
const (
	defaultDelphiMaxRounds            = 3
	defaultDelphiConvergenceThreshold = 0.1
)

// runDelphi executes the Delphi method's multi-round anonymous blind rating
// pipeline:
//
//   - Stage 1: parallel generation across ct.Models (reuses runStage1).
//     Anonymous "Response A/B/…" labels assigned via assignLabels. Quorum:
//     max(3, ⌈N/2⌉+1) — higher floor than PeerReview/Debate because
//     statistical averaging needs ≥3 raters.
//   - Rounds 1..R: each surviving rater concurrently rates ALL Stage 1
//     candidates against defaultDelphiCriteria and produces a 1–2 sentence
//     summary. Per-rater LLM/parse/missing-criteria failure drops that rater
//     for the rest of the run; per-round quorum re-check fires loud error
//     if survivors fall below need. Per-round event:
//     stage2_round_complete with kind="delphi_round" and round=r.
//   - Convergence: after every round R≥2, if max(DeltaMean[crit] across
//     criteria present in both rounds) < ct.DelphiConvergenceThreshold,
//     exit early with Converged=true. Conservative — every criterion must
//     converge.
//   - Stage 2 terminal event: stage2_complete with kind="delphi_round"
//     carrying the full transcript via Metadata.DelphiPanel.
//   - Stage 3: chairman synthesises across the final-round ratings + per-
//     rater summaries + converged stats.
//
// NO DelphiDropout type is added. Dropped raters are simply absent from
// subsequent rounds' Ratings slices; chairman and frontend infer dropout
// by label-set diff between rounds.
func (c *Council) runDelphi(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
	if len(ct.Models) == 0 {
		return fmt.Errorf("council type %q has no models configured", ct.Name)
	}
	if ct.ChairmanModel == "" {
		return fmt.Errorf("council type %q has no chairman model configured (Delphi Stage 3 always runs)", ct.Name)
	}

	rounds := ct.MaxDelphiRounds
	if rounds <= 0 {
		rounds = defaultDelphiMaxRounds
	}
	threshold := ct.DelphiConvergenceThreshold
	if threshold <= 0 {
		threshold = defaultDelphiConvergenceThreshold
	}

	// Stage 1 — parallel generation.
	allStage1 := c.runStage1(ctx, query, ct.Models, ct.Temperature)

	// Higher quorum floor for Delphi (3 vs 2 for PeerReview/Debate).
	need := ct.QuorumMin
	if need == 0 {
		n := len(allStage1)
		need = max(3, (n+1)/2+1)
	}
	successful, err := checkQuorum(allStage1, need)
	if err != nil {
		return err
	}

	successfulModels := make([]string, len(successful))
	for i, r := range successful {
		successfulModels[i] = r.Model
	}
	labelToModel, modelToLabel := assignLabels(successfulModels)
	for i := range successful {
		successful[i].Label = modelToLabel[successful[i].Model]
	}

	if onEvent != nil {
		onEvent("stage1_complete", successful)
	}

	// previousRatings holds each rater's most recent successful rating, keyed
	// by Label. Used to feed the rater its OWN previous ratings + summary in
	// rounds 2+ (so it can revise rather than start from scratch).
	previousRatings := make(map[string]DelphiRating, len(successful))

	// alive lists labels that are still in the panel. Sorted ascending for
	// determinism.
	alive := make([]string, 0, len(successful))
	for _, s1 := range successful {
		alive = append(alive, s1.Label)
	}
	sort.Strings(alive)

	// labelToS1 indexes Stage 1 successes for fast model lookup per label.
	labelToS1 := make(map[string]StageOneResult, len(successful))
	for _, s1 := range successful {
		labelToS1[s1.Label] = s1
	}

	transcript := make([]DelphiRound, 0, rounds)
	priorStats := make([]DelphiStats, 0, rounds)
	converged := false

	for r := 1; r <= rounds; r++ {
		ratings := c.runDelphiRound(ctx, query, successful, defaultDelphiCriteria, r, rounds, priorStats, previousRatings, labelToS1, alive, ct.Temperature)

		// Filter to successful ratings; update previousRatings + alive.
		newAlive := make([]string, 0, len(alive))
		successfulRatings := make([]DelphiRating, 0, len(ratings))
		for _, rt := range ratings {
			if rt.Error != nil || len(rt.Scores) == 0 {
				continue
			}
			successfulRatings = append(successfulRatings, rt)
			previousRatings[rt.Label] = rt
			newAlive = append(newAlive, rt.Label)
		}
		sort.Strings(newAlive)
		alive = newAlive

		// Sort ratings by Label for stable output.
		sort.SliceStable(successfulRatings, func(i, j int) bool {
			return successfulRatings[i].Label < successfulRatings[j].Label
		})

		// Compute aggregate stats for this round, keyed off prior-round mean
		// for DeltaMean (round 1 has no prev → DeltaMean stays nil/empty).
		var prevMean map[string]float64
		if len(priorStats) > 0 {
			prevMean = priorStats[len(priorStats)-1].Mean
		}
		stats := computeDelphiStats(successfulRatings, defaultDelphiCriteria, prevMean)

		round := DelphiRound{
			Round:   r,
			Ratings: successfulRatings,
			Stats:   stats,
		}
		transcript = append(transcript, round)
		priorStats = append(priorStats, stats)

		// Per-round event — carries this round's data only.
		if onEvent != nil {
			onEvent("stage2_round_complete", Stage2CompleteData{
				Kind:    "delphi_round",
				Round:   r,
				Results: []StageTwoResult{},
				Metadata: Metadata{
					CouncilType:       ct.Name,
					LabelToModel:      labelToModel,
					AggregateRankings: []RankedModel{},
					ConsensusW:        0.0,
					DelphiPanel: &DelphiPanel{
						Rounds:     []DelphiRound{round},
						FinalRound: r,
						Criteria:   defaultDelphiCriteria,
					},
				},
			})
		}

		// Per-round quorum re-check: survivors must still meet need.
		if len(alive) < need {
			return fmt.Errorf("delphi: quorum failed after round %d (%d survivors, need %d)", r, len(alive), need)
		}

		// Convergence check (round R≥2 only): max(DeltaMean) < threshold for
		// every criterion present in DeltaMean. If DeltaMean is empty (round 1
		// or every criterion missing in either round), skip — no convergence
		// declared.
		if r >= 2 && len(stats.DeltaMean) > 0 {
			maxDelta := 0.0
			for _, d := range stats.DeltaMean {
				if d > maxDelta {
					maxDelta = d
				}
			}
			if maxDelta < threshold {
				converged = true
				break
			}
		}
	}

	panel := &DelphiPanel{
		Rounds:     transcript,
		FinalRound: len(transcript),
		Converged:  converged,
		Criteria:   defaultDelphiCriteria,
	}

	metadata := Metadata{
		CouncilType:       ct.Name,
		LabelToModel:      labelToModel,
		AggregateRankings: []RankedModel{},
		ConsensusW:        0.0,
		DelphiPanel:       panel,
	}

	if onEvent != nil {
		onEvent("stage2_complete", Stage2CompleteData{
			Kind:     "delphi_round",
			Results:  []StageTwoResult{},
			Metadata: metadata,
		})
	}

	// Stage 3 — chairman synthesises across the final-round ratings.
	finalRatings := transcript[len(transcript)-1].Ratings
	finalStats := transcript[len(transcript)-1].Stats
	stage3, err := c.runDelphiStage3(ctx, query, successful, finalRatings, finalStats, converged, defaultDelphiCriteria, labelToModel, ct.ChairmanModel, ct.Temperature)
	if err != nil {
		return err
	}
	if onEvent != nil {
		onEvent("stage3_complete", stage3)
	}
	return nil
}

// runDelphiRound runs one round of rating in parallel across all surviving
// raters. Each rater gets the same prompt template (with its own previous-
// round ratings injected for rounds 2+) and produces JSON
// {ratings: {...}, summary: "..."}.
//
// Per-rater LLM error / JSON parse failure / empty scores result in a
// rating with Error set; the caller drops those raters from the run.
func (c *Council) runDelphiRound(
	ctx context.Context,
	query string,
	candidates []StageOneResult,
	criteria []string,
	round, totalRounds int,
	priorStats []DelphiStats,
	previousRatings map[string]DelphiRating,
	labelToS1 map[string]StageOneResult,
	alive []string,
	temperature float64,
) []DelphiRating {
	results := make([]DelphiRating, len(alive))
	var wg sync.WaitGroup
	for i, lbl := range alive {
		wg.Add(1)
		go func(idx int, selfLabel string) {
			defer wg.Done()
			s1 := labelToS1[selfLabel]
			model := s1.Model

			var selfPrevPtr *DelphiRating
			if prev, ok := previousRatings[selfLabel]; ok {
				selfPrev := prev // copy so we don't alias the map's value
				selfPrevPtr = &selfPrev
			}

			prompt := BuildDelphiRoundPrompt(query, candidates, criteria, round, totalRounds, priorStats, selfPrevPtr)

			start := time.Now()
			resp, err := c.client.Complete(ctx, CompletionRequest{
				Model:          model,
				Messages:       prompt,
				Temperature:    temperature,
				ResponseFormat: &ResponseFormat{Type: "json_object"},
			})
			elapsed := time.Since(start).Milliseconds()

			rating := DelphiRating{
				Label:      selfLabel,
				Model:      model,
				DurationMs: elapsed,
			}

			if err != nil {
				rating.Error = err
				if c.logger != nil {
					c.logger.Warn("delphi: rater errored", slog.String("label", selfLabel), slog.Int("round", round), slog.Any("error", err))
				}
				results[idx] = rating
				return
			}
			if len(resp.Choices) == 0 {
				rating.Error = errNoChoices
				results[idx] = rating
				return
			}

			body := StripCodeFence(resp.Choices[0].Message.Content)
			var parsed struct {
				Ratings map[string]float64 `json:"ratings"`
				Summary string             `json:"summary"`
			}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				rating.Error = err
				if c.logger != nil {
					c.logger.Warn("delphi: parse failure", slog.String("label", selfLabel), slog.Int("round", round), slog.Any("error", err))
				}
				results[idx] = rating
				return
			}
			if len(parsed.Ratings) == 0 {
				rating.Error = fmt.Errorf("empty ratings")
				if c.logger != nil {
					c.logger.Warn("delphi: empty ratings", slog.String("label", selfLabel), slog.Int("round", round))
				}
				results[idx] = rating
				return
			}

			// Per-criterion clamp + missing-criterion default.
			scores := make(map[string]float64, len(criteria))
			for _, name := range criteria {
				v, ok := parsed.Ratings[name]
				if !ok {
					if c.logger != nil {
						c.logger.Warn("delphi: missing criterion", slog.String("label", selfLabel), slog.String("criterion", name))
					}
					v = 0.0
				}
				scores[name] = clamp(v, 0.0, 1.0)
			}
			rating.Scores = scores
			rating.Summary = parsed.Summary
			results[idx] = rating
		}(i, lbl)
	}
	wg.Wait()
	return results
}

// computeDelphiStats is a pure function that computes per-criterion mean and
// stddev across all successful ratings in a single round, plus DeltaMean
// against the prior round's mean.
//
// Semantics on missing criteria:
//   - Mean[crit] and StdDev[crit] are populated only for criteria with ≥1
//     rating in this round.
//   - DeltaMean[crit] is populated only for criteria present in BOTH the
//     current round and the prior round; criteria missing from either side
//     are excluded from DeltaMean and from the convergence check.
//   - If prevMean is nil or empty (round 1), DeltaMean is nil (omitempty
//     keeps it off the wire).
func computeDelphiStats(ratings []DelphiRating, criteria []string, prevMean map[string]float64) DelphiStats {
	mean := make(map[string]float64, len(criteria))
	stdDev := make(map[string]float64, len(criteria))

	// Per-criterion: collect all rating values (each rater contributes one
	// value per criterion since runDelphiRound clamps + defaults missing
	// criteria to 0.0; but tests can also surface true absence by omitting
	// the criterion from rating.Scores).
	for _, name := range criteria {
		var values []float64
		for _, r := range ratings {
			if v, ok := r.Scores[name]; ok {
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			continue // criterion not populated this round
		}
		var sum float64
		for _, v := range values {
			sum += v
		}
		m := sum / float64(len(values))
		mean[name] = m

		var sqSum float64
		for _, v := range values {
			d := v - m
			sqSum += d * d
		}
		stdDev[name] = math.Sqrt(sqSum / float64(len(values))) // population stddev
	}

	var deltaMean map[string]float64
	if len(prevMean) > 0 {
		for _, name := range criteria {
			curr, hasCurr := mean[name]
			prev, hasPrev := prevMean[name]
			if !hasCurr || !hasPrev {
				continue // criterion missing in either round
			}
			if deltaMean == nil {
				deltaMean = make(map[string]float64)
			}
			deltaMean[name] = math.Abs(curr - prev)
		}
	}

	return DelphiStats{
		Mean:      mean,
		StdDev:    stdDev,
		DeltaMean: deltaMean,
	}
}

// runDelphiStage3 calls the chairman with the Stage 1 candidates + final-
// round per-rater ratings + summaries + converged stats. Failure path
// matches runStage3 / runDebateStage3 / runMoaRefine — Model + DurationMs
// populated even on error.
func (c *Council) runDelphiStage3(
	ctx context.Context,
	query string,
	candidates []StageOneResult,
	finalRatings []DelphiRating,
	finalStats DelphiStats,
	converged bool,
	criteria []string,
	labelToModel map[string]string,
	chairmanModel string,
	temperature float64,
) (StageThreeResult, error) {
	start := time.Now()
	msgs := BuildDelphiChairmanPrompt(query, candidates, finalRatings, finalStats, converged, criteria, labelToModel)
	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:       chairmanModel,
		Messages:    msgs,
		Temperature: temperature,
	})
	elapsed := time.Since(start).Milliseconds()
	result := StageThreeResult{Model: chairmanModel, DurationMs: elapsed}
	if err != nil {
		result.Error = err
		return result, fmt.Errorf("delphi chairman (%s): %w", chairmanModel, err)
	}
	if len(resp.Choices) == 0 {
		result.Error = fmt.Errorf("empty response from chairman %s", chairmanModel)
		return result, result.Error
	}
	result.Content = resp.Choices[0].Message.Content
	return result, nil
}
