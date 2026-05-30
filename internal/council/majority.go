package council

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// runMajority executes the Majority strategy: parallel generation → vote tally
// (no LLM call) → Stage 3 winner emission, with optional chairman polish.
//
// Voting is exact-match after normalisation. The plurality winner — the cluster
// with the most votes — is the final answer. Ties require a chairman model;
// without one the runner returns a loud error rather than picking arbitrarily.
func (c *Rada) runMajority(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
	if len(ct.Models) == 0 {
		return fmt.Errorf("council type %q has no models configured", ct.Name)
	}

	// Stage 1: parallel generation. Reuse runStage1 — Majority's Stage 1 has
	// no peer-review-specific shape; one answer per model is exactly what
	// runStage1 produces.
	allStage1 := c.runStage1(ctx, query, ct.Models, ct.Temperature)

	// Quorum: Majority needs ≥3 successful answers by default to break ties cleanly.
	// Per-registration QuorumMin (when non-zero) overrides.
	need := ct.QuorumMin
	if need == 0 {
		n := len(allStage1)
		need = max(3, (n+1)/2+1)
	}
	// checkQuorum honours an explicit non-zero `need`; the default formula
	// max(2, ⌈N/2⌉+1) only kicks in when need == 0. Since runMajority always
	// resolves a non-zero need above, checkQuorum applies our threshold verbatim.
	successful, err := checkQuorum(allStage1, need)
	if err != nil {
		return err
	}

	// Assign anonymous labels so the on-the-wire payload is consistent with
	// PeerReview shape; the frontend treats Stage 1 the same way.
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

	// Vote tally — pure function over Stage 1 results; no LLM call.
	tally := buildVoteTally(successful)

	// ConsensusW for Majority is the *consensus ratio*: winnerVotes / totalVotes.
	// This is NOT Kendall's W (which is rank-correlation across reviewers in
	// PeerReview); for vote_tally it's the share of voters who landed on the
	// winning cluster. 1.0 means unanimous, ~0.5 means a simple plurality with
	// strong opposition. Consumers like the eval harness can read this on the
	// Metadata directly without strategy-specific code paths.
	totalVotes := 0
	for _, cl := range tally.Clusters {
		totalVotes += cl.Votes
	}
	consensusW := 0.0
	if totalVotes > 0 && len(tally.Clusters) > 0 {
		consensusW = float64(tally.Clusters[0].Votes) / float64(totalVotes)
	}

	metadata := Metadata{
		CouncilType:       ct.Name,
		LabelToModel:      labelToModel,
		AggregateRankings: []RankedModel{}, // not used by Majority
		ConsensusW:        consensusW,
		VoteTally:         tally,
	}

	if onEvent != nil {
		onEvent("stage2_complete", Stage2CompleteData{
			Kind:     "vote_tally",
			Results:  []StageTwoResult{},
			Metadata: metadata,
		})
	}

	// Stage 3: emit the winner. Behaviour depends on tie state and chairman config.
	stage3, err := c.runMajorityStage3(ctx, query, tally, ct.ChairmanModel, ct.Temperature)
	if err != nil {
		return err
	}
	if onEvent != nil {
		onEvent("stage3_complete", stage3)
	}
	return nil
}

// normaliseAnswer prepares a Stage 1 answer for exact-match clustering:
// lowercase, trim leading/trailing whitespace, collapse internal whitespace
// runs to a single space.
func normaliseAnswer(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), " ")
}

// buildVoteTally clusters the Stage 1 answers by normalised content and
// produces a VoteTally with deterministic ordering: clusters sorted by votes
// descending, then by Representative ascending for stable output across runs.
//
// WinnerLabel is the first member of the highest-vote cluster (which itself
// is sorted by Members order, mirroring Stage 1's input order). Tie detection
// is the caller's responsibility — buildVoteTally does not flag ties.
func buildVoteTally(stage1 []StageOneResult) *VoteTally {
	if len(stage1) == 0 {
		return &VoteTally{Clusters: []VoteCluster{}}
	}

	byNorm := make(map[string]*VoteCluster)
	order := make([]string, 0)
	for _, r := range stage1 {
		key := normaliseAnswer(r.Content)
		if existing, ok := byNorm[key]; ok {
			existing.Members = append(existing.Members, r.Label)
			existing.Votes++
			continue
		}
		byNorm[key] = &VoteCluster{
			Members:        []string{r.Label},
			Representative: r.Content,
			Votes:          1,
		}
		order = append(order, key)
	}

	clusters := make([]VoteCluster, 0, len(byNorm))
	for _, key := range order {
		clusters = append(clusters, *byNorm[key])
	}
	sort.SliceStable(clusters, func(i, j int) bool {
		if clusters[i].Votes != clusters[j].Votes {
			return clusters[i].Votes > clusters[j].Votes
		}
		return clusters[i].Representative < clusters[j].Representative
	})

	winner := ""
	if len(clusters[0].Members) > 0 {
		winner = clusters[0].Members[0]
	}
	return &VoteTally{Clusters: clusters, WinnerLabel: winner}
}

// runMajorityStage3 produces the final answer from a vote tally:
//
//   - No tie + no chairman → emit the winning cluster's content verbatim;
//     Model is empty, DurationMs is 0 (no LLM call).
//   - No tie + chairman    → chairman polishes the winner; Model is the
//     chairman ID, DurationMs is the call duration.
//   - Tie    + chairman    → chairman picks among tied candidates.
//   - Tie    + no chairman → loud error.
func (c *Rada) runMajorityStage3(ctx context.Context, query string, tally *VoteTally, chairmanModel string, temperature float64) (StageThreeResult, error) {
	if tally == nil || len(tally.Clusters) == 0 {
		return StageThreeResult{}, fmt.Errorf("council: majority stage 3 received empty tally")
	}

	top := tally.Clusters[0]
	tiedCount := 0
	for _, cl := range tally.Clusters {
		if cl.Votes == top.Votes {
			tiedCount++
		}
	}

	if tiedCount > 1 {
		// Tie — chairman required.
		if chairmanModel == "" {
			return StageThreeResult{}, fmt.Errorf("council: majority strategy has %d-way tie and no chairman model configured for tiebreak", tiedCount)
		}
		tied := tally.Clusters[:tiedCount]
		return c.callMajorityChairman(ctx, BuildMajorityTiebreakPrompt(query, tied), chairmanModel, temperature)
	}

	// No tie.
	if chairmanModel == "" {
		// No-LLM path: emit winner verbatim. Model="" and DurationMs=0 are
		// the documented contract for "no LLM call produced this result".
		return StageThreeResult{Content: top.Representative}, nil
	}
	return c.callMajorityChairman(ctx, BuildMajorityPolishPrompt(query, top.Representative), chairmanModel, temperature)
}

// callMajorityChairman is the shared LLM-call shape for both the polish and
// tiebreak paths. The caller supplies the messages built from the appropriate
// prompt builder.
func (c *Rada) callMajorityChairman(ctx context.Context, msgs []ChatMessage, chairmanModel string, temperature float64) (StageThreeResult, error) {
	start := time.Now()
	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:       chairmanModel,
		Messages:    msgs,
		Temperature: temperature,
	})
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return StageThreeResult{Model: chairmanModel, DurationMs: elapsed}, fmt.Errorf("majority chairman (%s): %w", chairmanModel, err)
	}
	if len(resp.Choices) == 0 {
		return StageThreeResult{Model: chairmanModel, DurationMs: elapsed}, fmt.Errorf("empty response from chairman %s", chairmanModel)
	}
	return StageThreeResult{
		Content:    resp.Choices[0].Message.Content,
		Model:      chairmanModel,
		DurationMs: elapsed,
	}, nil
}
