package council

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// defaultMaxDebateRounds is used when CouncilType.MaxDebateRounds is zero-valued.
const defaultMaxDebateRounds = 2

// Dropout reason codes recorded on DebaterDropout.Reason.
const (
	dropReasonError          = "error"
	dropReasonJSONParse      = "json_parse"
	dropReasonEmptyRevision  = "empty_revision"
)

// runMultiAgentDebate executes the multi-round debate pipeline:
//
//   - Round 0 (Stage 1): parallel generation across ct.Models. Anonymous
//     labels assigned once and persisted across all rounds. Emits
//     stage1_complete.
//   - Rounds 1..R: each surviving debater sees all OTHER debaters'
//     previous-round answers (anonymised — labels only) and produces a
//     JSON {critique, revision}. Per-round event:
//     stage2_round_complete with kind="debate_round" and round=r.
//   - Stage 2 terminal event: stage2_complete with kind="debate_round"
//     carrying the full transcript via Metadata.Debate.
//   - Stage 3: chairman synthesises across the whole transcript.
//
// Quorum is checked after every round; if survivors drop below the
// threshold, runMultiAgentDebate returns a loud error rather than
// continuing with sub-quorum rounds. Per-debater failures within a round
// (call error / JSON parse failure / empty revision) drop that debater
// from subsequent rounds and append a DebaterDropout marker that the
// chairman + DebateView can reason about.
func (c *Council) runMultiAgentDebate(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
	if len(ct.Models) == 0 {
		return fmt.Errorf("council type %q has no models configured", ct.Name)
	}
	if ct.ChairmanModel == "" {
		return fmt.Errorf("council type %q has no chairman model configured (MultiAgentDebate Stage 3 always runs)", ct.Name)
	}

	rounds := ct.MaxDebateRounds
	if rounds <= 0 {
		rounds = defaultMaxDebateRounds
	}

	// Round 0 — parallel generation, same shape as PeerReview.
	allStage1 := c.runStage1(ctx, query, ct.Models, ct.Temperature)

	need := ct.QuorumMin
	if need == 0 {
		n := len(allStage1)
		need = max(2, (n+1)/2+1)
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

	// previousByLabel tracks each debater's most recent successful output.
	// In round 1 the "previous" output is the round-0 answer; in subsequent
	// rounds it's the prior revision. Keyed by Label.
	previousByLabel := make(map[string]DebaterRevision, len(successful))
	for _, s1 := range successful {
		previousByLabel[s1.Label] = DebaterRevision{
			Label:   s1.Label,
			Content: s1.Content,
			Model:   s1.Model,
		}
	}

	transcript := make([]DebateRound, 0, rounds)
	dropouts := make([]DebaterDropout, 0)
	// alive lists labels that are still in the debate. Sorted ascending so
	// per-round prompts and emitted revisions are deterministic for tests.
	alive := make([]string, 0, len(successful))
	for _, s1 := range successful {
		alive = append(alive, s1.Label)
	}
	sort.Strings(alive)

	for r := 1; r <= rounds; r++ {
		roundResult, roundDropouts := c.runDebateRound(ctx, query, alive, previousByLabel, r, rounds, ct.Temperature)

		// Update previousByLabel with successful revisions; record dropouts.
		newAlive := make([]string, 0, len(alive))
		for _, rev := range roundResult.Revisions {
			previousByLabel[rev.Label] = rev
			newAlive = append(newAlive, rev.Label)
		}
		dropouts = append(dropouts, roundDropouts...)
		sort.Strings(newAlive)
		alive = newAlive

		// Sort revisions deterministically before emitting.
		sort.SliceStable(roundResult.Revisions, func(i, j int) bool {
			return roundResult.Revisions[i].Label < roundResult.Revisions[j].Label
		})

		transcript = append(transcript, roundResult)

		if onEvent != nil {
			onEvent("stage2_round_complete", Stage2CompleteData{
				Kind:    "debate_round",
				Round:   r,
				Results: []StageTwoResult{},
				Metadata: Metadata{
					CouncilType:       ct.Name,
					LabelToModel:      labelToModel,
					AggregateRankings: []RankedModel{},
					ConsensusW:        0.0,
					Debate: &Debate{
						Rounds:     []DebateRound{roundResult}, // this round only
						FinalRound: r,
					},
				},
			})
		}

		// Quorum re-check: survivors must still meet `need` to continue.
		if len(alive) < need {
			return fmt.Errorf("multi-agent debate: quorum failed after round %d (%d survivors, need %d)", r, len(alive), need)
		}
	}

	// Sort dropouts by Label for stable output.
	sort.SliceStable(dropouts, func(i, j int) bool {
		return dropouts[i].Label < dropouts[j].Label
	})

	debate := &Debate{
		Rounds:     transcript,
		FinalRound: len(transcript),
		Dropouts:   dropouts,
	}

	metadata := Metadata{
		CouncilType:       ct.Name,
		LabelToModel:      labelToModel,
		AggregateRankings: []RankedModel{},
		ConsensusW:        0.0,
		Debate:            debate,
	}

	if onEvent != nil {
		onEvent("stage2_complete", Stage2CompleteData{
			Kind:     "debate_round",
			Results:  []StageTwoResult{},
			Metadata: metadata,
		})
	}

	// Stage 3 — chairman synthesises across the whole transcript.
	stage3, err := c.runDebateStage3(ctx, query, successful, debate, labelToModel, ct.ChairmanModel, ct.Temperature)
	if err != nil {
		return err
	}
	if onEvent != nil {
		onEvent("stage3_complete", stage3)
	}
	return nil
}

// runDebateRound runs one round of the debate in parallel across all surviving
// debaters. Returns the round's revisions plus any dropouts that occurred in
// THIS round (so the caller can append them to the cumulative dropouts list).
//
// Each debater's prompt shows the OTHER debaters' previous-round outputs (with
// labels only — never model names) and the debater's OWN previous-round
// answer (so they can revise it rather than start from scratch).
func (c *Council) runDebateRound(ctx context.Context, query string, alive []string, previousByLabel map[string]DebaterRevision, round, totalRounds int, temperature float64) (DebateRound, []DebaterDropout) {
	results := make([]DebaterRevision, len(alive))
	dropoutSlots := make([]*DebaterDropout, len(alive))

	var wg sync.WaitGroup
	for i, lbl := range alive {
		wg.Add(1)
		go func(idx int, selfLabel string) {
			defer wg.Done()

			// Build the OTHERS list — every alive debater except self, in
			// label order for determinism.
			others := make([]DebaterRevision, 0, len(alive)-1)
			for _, other := range alive {
				if other == selfLabel {
					continue
				}
				if rev, ok := previousByLabel[other]; ok {
					others = append(others, rev)
				}
			}
			sort.SliceStable(others, func(a, b int) bool {
				return others[a].Label < others[b].Label
			})

			selfPrev := previousByLabel[selfLabel]
			prompt := BuildDebateRoundPrompt(query, selfPrev, others, round, totalRounds)

			start := time.Now()
			resp, err := c.client.Complete(ctx, CompletionRequest{
				Model:          selfPrev.Model,
				Messages:       prompt,
				Temperature:    temperature,
				ResponseFormat: &ResponseFormat{Type: "json_object"},
			})
			elapsed := time.Since(start).Milliseconds()

			rev := DebaterRevision{
				Label:      selfLabel,
				Model:      selfPrev.Model,
				DurationMs: elapsed,
			}

			if err != nil {
				rev.Error = err
				dropoutSlots[idx] = &DebaterDropout{
					Label:     selfLabel,
					LastRound: round - 1,
					Reason:    dropReasonError,
				}
				if c.logger != nil {
					c.logger.Warn("debate: debater errored", slog.String("label", selfLabel), slog.Int("round", round), slog.Any("error", err))
				}
				return
			}
			if len(resp.Choices) == 0 {
				rev.Error = errNoChoices
				dropoutSlots[idx] = &DebaterDropout{
					Label:     selfLabel,
					LastRound: round - 1,
					Reason:    dropReasonError,
				}
				return
			}

			body := StripCodeFence(resp.Choices[0].Message.Content)
			var parsed struct {
				Critique string `json:"critique"`
				Revision string `json:"revision"`
			}
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				rev.Error = err
				dropoutSlots[idx] = &DebaterDropout{
					Label:     selfLabel,
					LastRound: round - 1,
					Reason:    dropReasonJSONParse,
				}
				if c.logger != nil {
					c.logger.Warn("debate: parse failure", slog.String("label", selfLabel), slog.Int("round", round), slog.Any("error", err))
				}
				return
			}
			if strings.TrimSpace(parsed.Revision) == "" {
				rev.Error = fmt.Errorf("empty revision")
				dropoutSlots[idx] = &DebaterDropout{
					Label:     selfLabel,
					LastRound: round - 1,
					Reason:    dropReasonEmptyRevision,
				}
				if c.logger != nil {
					c.logger.Warn("debate: empty revision", slog.String("label", selfLabel), slog.Int("round", round))
				}
				return
			}

			rev.Critique = parsed.Critique
			rev.Content = parsed.Revision
			results[idx] = rev
		}(i, lbl)
	}
	wg.Wait()

	// Filter results to successful revisions only (failed slots have
	// non-nil Error and were marked in dropoutSlots).
	successful := make([]DebaterRevision, 0, len(results))
	dropouts := make([]DebaterDropout, 0)
	for i, rev := range results {
		if rev.Error == nil && rev.Content != "" {
			successful = append(successful, rev)
		}
		if dropoutSlots[i] != nil {
			dropouts = append(dropouts, *dropoutSlots[i])
		}
	}

	return DebateRound{
		Round:     round,
		Revisions: successful,
	}, dropouts
}

// runDebateStage3 calls the chairman with the full transcript and Stage 1
// results to produce a refined final answer. Failure path matches runStage3
// and runRoleBasedStage3 (returns StageThreeResult{Model, DurationMs, Error}
// even when the call errors).
func (c *Council) runDebateStage3(ctx context.Context, query string, stage1 []StageOneResult, debate *Debate, labelToModel map[string]string, chairmanModel string, temperature float64) (StageThreeResult, error) {
	start := time.Now()
	msgs := BuildDebateChairmanPrompt(query, stage1, debate, labelToModel)
	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:       chairmanModel,
		Messages:    msgs,
		Temperature: temperature,
	})
	elapsed := time.Since(start).Milliseconds()
	result := StageThreeResult{Model: chairmanModel, DurationMs: elapsed}
	if err != nil {
		result.Error = err
		return result, fmt.Errorf("debate chairman (%s): %w", chairmanModel, err)
	}
	if len(resp.Choices) == 0 {
		result.Error = fmt.Errorf("empty response from chairman %s", chairmanModel)
		return result, result.Error
	}
	result.Content = resp.Choices[0].Message.Content
	return result, nil
}
