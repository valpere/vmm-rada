package council

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// defaultRankRefineCriteria is the v1 fixed set of ranking criteria. Per-
// registration and per-request overrides are deferred — the load-bearing risk
// for this strategy lives in prompt engineering, not config schema.
var defaultRankRefineCriteria = []string{"correctness", "clarity", "completeness", "originality"}

// defaultRefineTopK is used when CouncilType.RefineTopK is zero-valued.
const defaultRefineTopK = 3

// runGenerateRankRefine executes the 3-stage GenerateRankRefine pipeline:
//
//   - Stage 1: parallel generation across ct.Models (reuses runStage1)
//   - Stage 2: single arbiter LLM call ranking the candidates against
//     defaultRankRefineCriteria, with per-criterion scores in [0.0, 1.0]
//     and total in [0.0, len(criteria)]; emits kind="rank_refine"
//   - Stage 3: chairman refines the top-K into the final answer
//
// Both Stage 2 and Stage 3 use ct.ChairmanModel. Splitting them into separate
// RankerModel/RefinerModel fields is deferred — see docs/strategies.md.
func (c *Rada) runGenerateRankRefine(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
	if len(ct.Models) == 0 {
		return fmt.Errorf("council type %q has no models configured", ct.Name)
	}
	if ct.ChairmanModel == "" {
		return fmt.Errorf("council type %q has no chairman model configured (GenerateRankRefine requires both ranking and refinement calls)", ct.Name)
	}

	k := ct.RefineTopK
	if k == 0 {
		k = defaultRefineTopK
	}

	// Stage 1 — parallel generation.
	allStage1 := c.runStage1(ctx, query, ct.Models, ct.Temperature)

	// Quorum: max(K+1, 3) by default — the K+1 floor enforces "at least one
	// rejection." Refining all candidates defeats the rank-to-filter point.
	need := ct.QuorumMin
	if need == 0 {
		need = max(k+1, 3)
	}
	successful, err := checkQuorum(allStage1, need)
	if err != nil {
		return err
	}

	// Anonymous labels for SSE consistency with PeerReview shape.
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

	// Stage 2 — single arbiter call ranks candidates.
	rankings, err := c.runRankStage(ctx, query, successful, defaultRankRefineCriteria, k, ct.ChairmanModel, ct.Temperature)
	if err != nil {
		return err
	}

	// Mark exactly k advancing — top-k by sort order even on ties (deterministic
	// via secondary Label sort in runRankStage). If we have fewer than k
	// rankings (defensive — quorum already guarantees N >= k+1, so this is
	// only reachable if the arbiter dropped candidates as unknown labels),
	// mark all of them and proceed with what we have.
	advanceCount := k
	if advanceCount > len(rankings) {
		advanceCount = len(rankings)
	}
	for i := range rankings {
		rankings[i].Advancing = i < advanceCount
	}

	tally := &RankRefine{
		Rankings: rankings,
		TopK:     k,
		Criteria: defaultRankRefineCriteria,
	}

	metadata := Metadata{
		CouncilType:       ct.Name,
		LabelToModel:      labelToModel,
		AggregateRankings: []RankedModel{},
		ConsensusW:        averageScoreOf(rankings, defaultRankRefineCriteria),
		RankRefine:        tally,
	}

	if onEvent != nil {
		onEvent("stage2_complete", Stage2CompleteData{
			Kind:     "rank_refine",
			Results:  []StageTwoResult{},
			Metadata: metadata,
		})
	}

	// Stage 3 — refine the top-K. Build a label→content map of advancing
	// candidates from the Stage 1 successful list, then call the chairman.
	advancing := pickAdvancing(successful, rankings)
	stage3, err := c.runRefineStage(ctx, query, advancing, defaultRankRefineCriteria, ct.ChairmanModel, ct.Temperature)
	if err != nil {
		// runRefineStage already populates Model + DurationMs on the result;
		// emit nothing further and return the wrapped error.
		return err
	}
	if onEvent != nil {
		onEvent("stage3_complete", stage3)
	}
	return nil
}

// pickAdvancing returns the Stage 1 results corresponding to the advancing
// rankings, in ranking order (best first). The mapping is by Label.
func pickAdvancing(stage1 []StageOneResult, rankings []RankedCandidate) []StageOneResult {
	byLabel := make(map[string]StageOneResult, len(stage1))
	for _, r := range stage1 {
		byLabel[r.Label] = r
	}
	out := make([]StageOneResult, 0, len(rankings))
	for _, r := range rankings {
		if !r.Advancing {
			continue
		}
		if s1, ok := byLabel[r.Label]; ok {
			out = append(out, s1)
		}
	}
	return out
}

// averageScoreOf returns the mean TotalScore divided by len(criteria) so the
// resulting value is in [0.0, 1.0] — useful as a "council quality" signal
// surfaced via Metadata.ConsensusW (consistent with how Majority repurposes
// that field for its consensus ratio).
func averageScoreOf(rankings []RankedCandidate, criteria []string) float64 {
	if len(rankings) == 0 || len(criteria) == 0 {
		return 0.0
	}
	var sum float64
	for _, r := range rankings {
		sum += r.TotalScore
	}
	avg := sum / float64(len(rankings))
	return avg / float64(len(criteria))
}

// runRankStage calls the arbiter once with the full candidate set + criteria
// and returns the parsed, validated, sorted rankings. Loud error on any
// parse failure — Stage 2 IS the entire ranking, so silent fall-through
// would corrupt refinement.
func (c *Rada) runRankStage(ctx context.Context, query string, candidates []StageOneResult, criteria []string, k int, chairmanModel string, temperature float64) ([]RankedCandidate, error) {
	prompt := BuildRankPrompt(query, candidates, criteria, k)
	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:          chairmanModel,
		Messages:       prompt,
		Temperature:    temperature,
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, fmt.Errorf("rank-refine arbiter (%s): %w", chairmanModel, err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("rank-refine arbiter: %w", errNoChoices)
	}

	body := StripCodeFence(resp.Choices[0].Message.Content)
	var parsed struct {
		Rankings []struct {
			Label      string             `json:"label"`
			Scores     map[string]float64 `json:"scores"`
			TotalScore float64            `json:"total_score"`
		} `json:"rankings"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("rank-refine arbiter: %w", err)
	}

	// Build a known-labels set so we can drop hallucinated labels with a warn.
	known := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		known[c.Label] = true
	}

	out := make([]RankedCandidate, 0, len(parsed.Rankings))
	maxTotal := float64(len(criteria)) // upper bound for TotalScore clamp
	for _, r := range parsed.Rankings {
		if !known[r.Label] {
			if c.logger != nil {
				c.logger.Warn("rank-refine: unknown label dropped", slog.String("label", r.Label))
			}
			continue
		}
		// Per-criterion clamp + missing-criterion default.
		scores := make(map[string]float64, len(criteria))
		for _, name := range criteria {
			v, ok := r.Scores[name]
			if !ok {
				if c.logger != nil {
					c.logger.Warn("rank-refine: missing criterion in score", slog.String("label", r.Label), slog.String("criterion", name))
				}
				v = 0.0
			}
			scores[name] = clamp(v, 0.0, 1.0)
		}
		// Recompute TotalScore from clamped values rather than trusting the
		// arbiter's number — avoids the case where an arbiter returns
		// internally-inconsistent scores + total.
		var total float64
		for _, name := range criteria {
			total += scores[name]
		}
		total = clamp(total, 0.0, maxTotal)

		out = append(out, RankedCandidate{
			Label:      r.Label,
			Scores:     scores,
			TotalScore: total,
		})
	}

	// Sort descending by TotalScore, ascending by Label as deterministic
	// tiebreak. Tie at the K boundary: top-K by sort order, no rebalancing,
	// no chairman tiebreak.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TotalScore != out[j].TotalScore {
			return out[i].TotalScore > out[j].TotalScore
		}
		return out[i].Label < out[j].Label
	})

	return out, nil
}

// runRefineStage calls the chairman with the top-K candidates and asks for a
// refined synthesis. Both success and error paths populate Model + DurationMs
// so error-path observability matches the success path (consistent with
// runStage3 and runRoleBasedStage3).
func (c *Rada) runRefineStage(ctx context.Context, query string, advancing []StageOneResult, criteria []string, chairmanModel string, temperature float64) (StageThreeResult, error) {
	start := time.Now()
	msgs := BuildRankRefinePrompt(query, advancing, criteria)
	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:       chairmanModel,
		Messages:    msgs,
		Temperature: temperature,
	})
	elapsed := time.Since(start).Milliseconds()
	result := StageThreeResult{Model: chairmanModel, DurationMs: elapsed}
	if err != nil {
		result.Error = err
		return result, fmt.Errorf("rank-refine refiner (%s): %w", chairmanModel, err)
	}
	if len(resp.Choices) == 0 {
		result.Error = fmt.Errorf("empty response from refiner %s", chairmanModel)
		return result, result.Error
	}
	result.Content = resp.Choices[0].Message.Content
	return result, nil
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
