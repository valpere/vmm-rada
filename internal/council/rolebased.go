package council

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// runRoleBased executes the role-based 2-stage pipeline (Stage 1 + Stage 3).
// Stage 2 is skipped; a minimal Stage2CompleteData event is emitted for SSE compatibility.
func (c *Rada) runRoleBased(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
	if len(ct.Roles) == 0 {
		return fmt.Errorf("council type %q has no roles configured", ct.Name)
	}
	if len(ct.Models) == 0 {
		return fmt.Errorf("council type %q has no models configured", ct.Name)
	}

	// Stage 1: parallel role execution.
	stage1 := c.runRoleBasedStage1(ctx, query, ct)

	successful, err := checkQuorum(stage1, ct.QuorumMin)
	if err != nil {
		return err
	}

	// Build labelToModel map (role name → model used).
	labelToModel := make(map[string]string, len(successful))
	for _, r := range successful {
		labelToModel[r.Label] = r.Model
	}

	if onEvent != nil {
		onEvent("stage1_complete", successful)
	}

	// Stage 2: skipped for role-based strategies.
	// Emit a minimal Stage2CompleteData so SSE clients receive the expected event.
	meta := Metadata{
		CouncilType:       ct.Name,
		LabelToModel:      labelToModel,
		AggregateRankings: []RankedModel{},
		ConsensusW:        1.0, // roles are complementary, not competing
	}
	if onEvent != nil {
		onEvent("stage2_complete", Stage2CompleteData{Kind: "role_stub", Results: []StageTwoResult{}, Metadata: meta})
	}

	// Stage 3: chairman synthesis.
	stage3, err := c.runRoleBasedStage3(ctx, query, successful, ct.ChairmanModel, ct.Temperature)
	if err != nil {
		return err
	}
	if onEvent != nil {
		onEvent("stage3_complete", stage3)
	}
	return nil
}

// runRoleBasedStage1 executes all roles concurrently.
// Model assignment: ct.Models[i % len(ct.Models)].
func (c *Rada) runRoleBasedStage1(ctx context.Context, query string, ct CouncilType) []StageOneResult {
	results := make([]StageOneResult, len(ct.Roles))
	var wg sync.WaitGroup

	for i, role := range ct.Roles {
		wg.Add(1)
		go func(idx int, r Role) {
			defer wg.Done()
			model := ct.Models[idx%len(ct.Models)]
			start := time.Now()

			msgs := BuildRoleStage1Prompt(r, query)
			resp, err := c.client.Complete(ctx, CompletionRequest{
				Model:       model,
				Messages:    msgs,
				Temperature: ct.Temperature,
			})

			result := StageOneResult{
				Label:      r.Name,
				Model:      model,
				DurationMs: time.Since(start).Milliseconds(),
			}
			if err != nil {
				result.Error = err
			} else if len(resp.Choices) == 0 {
				result.Error = fmt.Errorf("role %q: empty response from %s", r.Name, model)
			} else {
				result.Content = resp.Choices[0].Message.Content
			}
			results[idx] = result
		}(i, role)
	}
	wg.Wait()
	return results
}

// runRoleBasedStage3 asks the chairman to synthesise all role findings.
func (c *Rada) runRoleBasedStage3(ctx context.Context, query string, roleResults []StageOneResult, chairmanModel string, temperature float64) (StageThreeResult, error) {
	start := time.Now()
	msgs := BuildRoleChairmanPrompt(query, roleResults)

	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:       chairmanModel,
		Messages:    msgs,
		Temperature: temperature,
	})

	result := StageThreeResult{
		Model:      chairmanModel,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		result.Error = err
		return result, fmt.Errorf("role-based chairman (%s): %w", chairmanModel, err)
	}
	if len(resp.Choices) == 0 {
		result.Error = fmt.Errorf("empty response from chairman %s", chairmanModel)
		return result, result.Error
	}
	result.Content = resp.Choices[0].Message.Content
	return result, nil
}
