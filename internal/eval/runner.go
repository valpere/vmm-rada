package eval

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/valpere/vmm-rada/internal/council"
)

// runCouncil drives the full council pipeline for a single prompt and pulls
// out the chairman's final answer plus the consensus W computed in Stage 2.
//
// The Runner contract delivers stage results via onEvent — there is no
// "return value" channel — so this helper closes over local variables that
// the EventFunc populates as the events fire. After RunFull returns we have
// either a captured answer (success) or a captured error.
func runCouncil(
	ctx context.Context,
	runner council.Runner,
	prompt, councilType string,
) (answer string, consensusW float64, durationMs int64, err error) {
	var (
		stage3Captured bool
	)
	onEvent := func(eventType string, data any) {
		switch eventType {
		case "stage2_complete":
			if d, ok := data.(council.Stage2CompleteData); ok {
				consensusW = d.Metadata.ConsensusW
			}
		case "stage3_complete":
			if d, ok := data.(council.StageThreeResult); ok {
				answer = d.Content
				stage3Captured = true
			}
		}
	}

	start := time.Now()
	runErr := runner.RunFull(ctx, prompt, councilType, onEvent)
	durationMs = time.Since(start).Milliseconds()

	if runErr != nil {
		return "", consensusW, durationMs, fmt.Errorf("council run: %w", runErr)
	}
	if !stage3Captured {
		return "", consensusW, durationMs, errors.New("council run completed but no stage3_complete event was observed")
	}
	return answer, consensusW, durationMs, nil
}

// runBaseline calls the LLM gateway directly with a single user message —
// this is the "what would a single model produce on its own" comparison
// point. Temperature matches the council's so we're comparing apples to
// apples for sampling behaviour.
func runBaseline(
	ctx context.Context,
	client council.LLMClient,
	prompt, model string,
	temperature float64,
) (answer string, durationMs int64, err error) {
	start := time.Now()
	resp, callErr := client.Complete(ctx, council.CompletionRequest{
		Model:       model,
		Messages:    []council.ChatMessage{{Role: "user", Content: prompt}},
		Temperature: temperature,
	})
	durationMs = time.Since(start).Milliseconds()
	if callErr != nil {
		return "", durationMs, fmt.Errorf("baseline llm call: %w", callErr)
	}
	if len(resp.Choices) == 0 {
		return "", durationMs, errors.New("baseline response had no choices")
	}
	return resp.Choices[0].Message.Content, durationMs, nil
}
