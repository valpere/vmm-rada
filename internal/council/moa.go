package council

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"sync"
	"time"
)

// runMixtureOfAgents executes the 3-layer Mixture-of-Agents pipeline:
//
//   - Layer 1 (Stage 1): parallel generation across ct.ProposerModels. Anonymous
//     "Response A/B/…" labels assigned via assignLabels. Emits stage1_complete.
//   - Layer 2 (Stage 2): parallel call per aggregator (ct.AggregatorModels) with
//     all-to-all fan-out — every aggregator sees every Layer 1 proposer draft.
//     Aggregators get distinct "Aggregator A/B/…" labels (assignAggregatorLabels)
//     so they don't collide with proposer labels in Metadata.LabelToModel. Emits
//     stage2_complete with kind="moa_aggregator".
//   - Layer 3 (Stage 3): single refiner call (ct.RefinerModel) that synthesises
//     the aggregator drafts into the final answer. The refiner sees aggregator
//     outputs WITH model attribution but does NOT see raw proposer outputs.
//
// Two-tier quorum:
//   - Layer 1: max(2, ⌈N_proposers/2⌉+1) when ct.QuorumMin == 0; standard
//     *QuorumError if Layer 1 fails.
//   - Layer 2: at least 1 successful aggregator; loud error otherwise (no
//     stage2_complete emitted, no Stage 3 run).
//
// Models and ChairmanModel are UNUSED for MoA registrations — the runner reads
// ProposerModels / AggregatorModels / RefinerModel directly. See CouncilType's
// field-usage matrix in types.go.
func (c *Rada) runMixtureOfAgents(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
	if len(ct.ProposerModels) == 0 {
		return fmt.Errorf("council type %q has no proposer models configured (set ProposerModels for MixtureOfAgents)", ct.Name)
	}
	if len(ct.AggregatorModels) == 0 {
		return fmt.Errorf("council type %q has no aggregator models configured (set AggregatorModels for MixtureOfAgents)", ct.Name)
	}
	if ct.RefinerModel == "" {
		return fmt.Errorf("council type %q has no refiner model configured (set RefinerModel for MixtureOfAgents)", ct.Name)
	}

	// Layer 1 — parallel generation across the proposer pool.
	allStage1 := c.runStage1(ctx, query, ct.ProposerModels, ct.Temperature)

	// Layer 1 quorum check.
	successful, err := checkQuorum(allStage1, ct.QuorumMin)
	if err != nil {
		return err
	}

	// Anonymous proposer labels.
	successfulModels := make([]string, len(successful))
	for i, r := range successful {
		successfulModels[i] = r.Model
	}
	proposerLabelToModel, proposerModelToLabel := assignLabels(successfulModels)
	for i := range successful {
		successful[i].Label = proposerModelToLabel[successful[i].Model]
	}

	if onEvent != nil {
		onEvent("stage1_complete", successful)
	}

	// Aggregator labels (distinct prefix family).
	aggregatorLabelToModel, _ := assignAggregatorLabels(ct.AggregatorModels)

	// Merge proposer + aggregator label maps into one flat LabelToModel — key
	// collisions are impossible because the prefixes differ.
	labelToModel := make(map[string]string, len(proposerLabelToModel)+len(aggregatorLabelToModel))
	maps.Copy(labelToModel, proposerLabelToModel)
	maps.Copy(labelToModel, aggregatorLabelToModel)

	// Layer 2 — parallel aggregators, all-to-all fan-out over Layer 1 proposers.
	aggregatorOutputs := c.runMoaLayer2(ctx, query, successful, aggregatorLabelToModel, ct.Temperature)

	// Sort aggregators by Label for stable test output and deterministic prompts.
	sort.SliceStable(aggregatorOutputs, func(i, j int) bool {
		return aggregatorOutputs[i].Label < aggregatorOutputs[j].Label
	})

	// Layer 2 quorum: at least one successful aggregator.
	successfulAggregators := make([]AggregatorOutput, 0, len(aggregatorOutputs))
	for _, a := range aggregatorOutputs {
		if a.Error == nil && a.Content != "" {
			successfulAggregators = append(successfulAggregators, a)
		}
	}
	if len(successfulAggregators) == 0 {
		return fmt.Errorf("mixture-of-agents: all aggregators failed (%d configured); refiner has no input", len(ct.AggregatorModels))
	}

	moa := &MoaAggregator{Aggregators: aggregatorOutputs}

	metadata := Metadata{
		CouncilType:       ct.Name,
		LabelToModel:      labelToModel,
		AggregateRankings: []RankedModel{},
		ConsensusW:        0.0,
		MoaAggregator:     moa,
	}

	if onEvent != nil {
		onEvent("stage2_complete", Stage2CompleteData{
			Kind:     "moa_aggregator",
			Results:  []StageTwoResult{},
			Metadata: metadata,
		})
	}

	// Layer 3 — single refiner call over the successful aggregators.
	stage3, err := c.runMoaRefine(ctx, query, successfulAggregators, labelToModel, ct.RefinerModel, ct.Temperature)
	if err != nil {
		return err
	}
	if onEvent != nil {
		onEvent("stage3_complete", stage3)
	}
	return nil
}

// runMoaLayer2 runs the aggregator layer in parallel. Each aggregator receives
// the SAME prompt — all proposer drafts (with labels only) — so the prompt is
// built once and reused.
//
// The Sources slice on every emitted AggregatorOutput is the labels of all
// successful Layer 1 proposers fed into the prompt — deterministic (sorted by
// Label) so transcript-agreement tests can verify Sources matches the prompt
// body byte-for-byte.
func (c *Rada) runMoaLayer2(ctx context.Context, query string, proposers []StageOneResult, aggregatorLabelToModel map[string]string, temperature float64) []AggregatorOutput {
	// Stable order of sources mirrors the prompt builder's internal sort.
	sources := make([]string, len(proposers))
	for i, p := range proposers {
		sources[i] = p.Label
	}
	sort.Strings(sources)

	prompt := BuildMoaAggregatorPrompt(query, proposers)

	// Iterate aggregator labels in a deterministic order.
	labels := make([]string, 0, len(aggregatorLabelToModel))
	for l := range aggregatorLabelToModel {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	results := make([]AggregatorOutput, len(labels))
	var wg sync.WaitGroup
	for i, lbl := range labels {
		wg.Add(1)
		go func(idx int, label string) {
			defer wg.Done()
			model := aggregatorLabelToModel[label]
			start := time.Now()
			resp, err := c.client.Complete(ctx, CompletionRequest{
				Model:       model,
				Messages:    prompt,
				Temperature: temperature,
			})
			elapsed := time.Since(start).Milliseconds()
			out := AggregatorOutput{
				Label:      label,
				Model:      model,
				Sources:    append([]string(nil), sources...),
				DurationMs: elapsed,
			}
			if err != nil {
				out.Error = err
				if c.logger != nil {
					c.logger.Warn("moa: aggregator errored", slog.String("label", label), slog.String("model", model), slog.Any("error", err))
				}
				results[idx] = out
				return
			}
			if len(resp.Choices) == 0 {
				out.Error = errNoChoices
				if c.logger != nil {
					c.logger.Warn("moa: aggregator returned no choices", slog.String("label", label), slog.String("model", model))
				}
				results[idx] = out
				return
			}
			out.Content = resp.Choices[0].Message.Content
			results[idx] = out
		}(i, lbl)
	}
	wg.Wait()
	return results
}

// runMoaRefine calls the Layer 3 refiner once with the successful aggregator
// outputs and returns the synthesised final answer. Failure path matches
// runStage3 — Model + DurationMs are populated even on error.
func (c *Rada) runMoaRefine(ctx context.Context, query string, aggregators []AggregatorOutput, labelToModel map[string]string, refinerModel string, temperature float64) (StageThreeResult, error) {
	start := time.Now()
	msgs := BuildMoaRefinerPrompt(query, aggregators, labelToModel)
	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:       refinerModel,
		Messages:    msgs,
		Temperature: temperature,
	})
	elapsed := time.Since(start).Milliseconds()
	result := StageThreeResult{Model: refinerModel, DurationMs: elapsed}
	if err != nil {
		result.Error = err
		return result, fmt.Errorf("moa refiner (%s): %w", refinerModel, err)
	}
	if len(resp.Choices) == 0 {
		result.Error = fmt.Errorf("empty response from refiner %s", refinerModel)
		return result, result.Error
	}
	result.Content = resp.Choices[0].Message.Content
	return result, nil
}
