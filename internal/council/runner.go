package council

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

var errNoChoices = errors.New("council: completion response contained no choices")

// StripCodeFence removes markdown code fences that some models wrap around JSON.
// Handles ```json\n...\n``` and ```\n...\n``` patterns.
//
// Exported for use by the eval package, which talks to the same family of
// fence-happy models when running its judge.
func StripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"```json", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
			if idx := strings.LastIndex(s, "```"); idx >= 0 {
				s = strings.TrimSpace(s[:idx])
			}
			break
		}
	}
	return s
}

// Council orchestrates the full multi-stage deliberation pipeline.
// Full implementation is provided in a later milestone.
type Council struct {
	client   LLMClient
	registry map[string]CouncilType
	logger   *slog.Logger
}

// NewCouncil creates a Council that uses client for LLM calls and resolves
// named council types from registry.
func NewCouncil(client LLMClient, registry map[string]CouncilType, logger *slog.Logger) *Council {
	return &Council{
		client:   client,
		registry: registry,
		logger:   logger,
	}
}

// Compile-time assertions: Council implements Runner and Stage0Runner.
var _ Runner = (*Council)(nil)
var _ Stage0Runner = (*Council)(nil)

// RunFull dispatches to the pipeline implementation for the council type's strategy.
func (c *Council) RunFull(ctx context.Context, query string, councilTypeName string, onEvent EventFunc) error {
	ct, ok := c.registry[councilTypeName]
	if !ok {
		return fmt.Errorf("council: unknown council type %q", councilTypeName)
	}
	switch ct.Strategy {
	case PeerReview:
		return c.runPeerReview(ctx, query, ct, onEvent)
	case RoleBased:
		return c.runRoleBased(ctx, query, ct, onEvent)
	case Majority:
		return c.runMajority(ctx, query, ct, onEvent)
	case GenerateRankRefine:
		return c.runGenerateRankRefine(ctx, query, ct, onEvent)
	case MultiAgentDebate:
		return c.runMultiAgentDebate(ctx, query, ct, onEvent)
	default:
		return fmt.Errorf("council: strategy %d not implemented", ct.Strategy)
	}
}

// runPeerReview runs the Karpathy-style 3-stage peer review pipeline.
func (c *Council) runPeerReview(ctx context.Context, query string, ct CouncilType, onEvent EventFunc) error {
	// Stage 1 — parallel generation across all configured models.
	allStage1 := c.runStage1(ctx, query, ct.Models, ct.Temperature)

	// Quorum check — returns *QuorumError if not enough models succeeded.
	successful, err := checkQuorum(allStage1, ct.QuorumMin)
	if err != nil {
		return err
	}

	// Assign anonymous labels so peer reviewers cannot identify each other.
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

	// Stage 2 — parallel peer review.
	stage2Results := c.runStage2(ctx, query, successful, ct.Temperature)

	// Compute aggregate rankings and Kendall's W consensus coefficient.
	allLabels := make([]string, 0, len(labelToModel))
	for label := range labelToModel {
		allLabels = append(allLabels, label)
	}
	sort.Strings(allLabels)
	aggregateRankings, consensusW := CalculateAggregateRankings(stage2Results, allLabels)

	// Translate label-keyed RankedModel entries to real model names for persistence/API.
	rankedByModel := make([]RankedModel, len(aggregateRankings))
	for i, r := range aggregateRankings {
		if model, ok := labelToModel[r.Model]; ok {
			r.Model = model
		}
		rankedByModel[i] = r
	}

	metadata := Metadata{
		CouncilType:       ct.Name,
		LabelToModel:      labelToModel,
		AggregateRankings: rankedByModel,
		ConsensusW:        consensusW,
	}

	if onEvent != nil {
		onEvent("stage2_complete", Stage2CompleteData{Kind: "peer_ranking", Results: stage2Results, Metadata: metadata})
	}

	// Stage 3 — Chairman synthesis.
	labeledResponses := make(map[string]string, len(successful))
	for _, r := range successful {
		labeledResponses[r.Label] = r.Content
	}
	stage3Result, err := c.runStage3(ctx, query, stage2Results, labelToModel, consensusW, ct.ChairmanModel, ct.Temperature, labeledResponses)
	if err != nil {
		return err
	}

	if onEvent != nil {
		onEvent("stage3_complete", stage3Result)
	}

	return nil
}

// runStage1 sends query to all models concurrently and returns all results.
// Each goroutine writes to its own pre-allocated results[i] slot — no mutex needed.
// Context cancellation is propagated to every Complete call.
// Quorum is NOT checked here — that is the caller's responsibility.
func (c *Council) runStage1(ctx context.Context, query string, models []string, temperature float64) []StageOneResult {
	results := make([]StageOneResult, len(models))
	var wg sync.WaitGroup
	for i, model := range models {
		wg.Add(1)
		go func(i int, model string) {
			defer wg.Done()
			start := time.Now()
			resp, err := c.client.Complete(ctx, CompletionRequest{
				Model:       model,
				Messages:    BuildStage1Prompt(query),
				Temperature: temperature,
			})
			if err == nil && len(resp.Choices) == 0 {
				err = errNoChoices
			}
			results[i] = StageOneResult{
				Model:      model,
				DurationMs: time.Since(start).Milliseconds(),
				Error:      err,
			}
			if err == nil {
				results[i].Content = resp.Choices[0].Message.Content
			}
		}(i, model)
	}
	wg.Wait()
	return results
}

// runStage2 sends peer-review requests to all stage1 models concurrently.
// Each reviewer receives the full set of anonymised stage1 responses and returns
// a ranked ordering as JSON. Parse failures are logged and treated as missing
// rankings so midrank imputation in CalculateAggregateRankings handles them.
// Unknown labels are logged and dropped from the ranking.
// LLM call failures are stored in StageTwoResult.Error; parse failures are not.
func (c *Council) runStage2(ctx context.Context, query string, stage1 []StageOneResult, temperature float64) []StageTwoResult {
	// Build the prompt and label maps once — shared across all reviewer goroutines.
	labeledResponses := make(map[string]string, len(stage1))
	knownLabels := make(map[string]bool, len(stage1))
	for _, r := range stage1 {
		labeledResponses[r.Label] = r.Content
		knownLabels[r.Label] = true
	}
	prompt := BuildStage2Prompt(query, labeledResponses)

	results := make([]StageTwoResult, len(stage1))
	var wg sync.WaitGroup
	for i, s1 := range stage1 {
		wg.Add(1)
		go func(i int, s1 StageOneResult) {
			defer wg.Done()
			resp, err := c.client.Complete(ctx, CompletionRequest{
				Model:          s1.Model,
				Messages:       prompt,
				Temperature:    temperature,
				ResponseFormat: &ResponseFormat{Type: "json_object"},
			})
			if err != nil {
				results[i] = StageTwoResult{ReviewerLabel: s1.Label, Error: err}
				return
			}
			if len(resp.Choices) == 0 {
				results[i] = StageTwoResult{ReviewerLabel: s1.Label, Error: errNoChoices}
				return
			}

			var parsed struct {
				Rankings []string `json:"rankings"`
			}
			if err := json.Unmarshal([]byte(StripCodeFence(resp.Choices[0].Message.Content)), &parsed); err != nil {
				if c.logger != nil {
					c.logger.Warn("stage2: parse failure", slog.String("reviewer", s1.Label), slog.Any("error", err))
				}
				results[i] = StageTwoResult{ReviewerLabel: s1.Label}
				return
			}
			if len(parsed.Rankings) == 0 {
				if c.logger != nil {
					c.logger.Warn("stage2: empty rankings", slog.String("reviewer", s1.Label))
				}
				results[i] = StageTwoResult{ReviewerLabel: s1.Label}
				return
			}

			valid := make([]string, 0, len(parsed.Rankings))
			for _, label := range parsed.Rankings {
				if knownLabels[label] {
					valid = append(valid, label)
				} else if c.logger != nil {
					c.logger.Warn("stage2: unknown label dropped", slog.String("reviewer", s1.Label), slog.String("label", label))
				}
			}
			results[i] = StageTwoResult{ReviewerLabel: s1.Label, Rankings: valid}
		}(i, s1)
	}
	wg.Wait()
	return results
}

// RunClarificationRound runs one Stage 0 clarification round.
// It emits either "stage0_round_complete" (with questions) or "stage0_done" (proceed to Stage 1).
func (c *Council) RunClarificationRound(
	ctx context.Context,
	query string,
	history []ClarificationRound,
	cfg ClarificationConfig,
	councilType string,
	onEvent EventFunc,
) error {
	round := len(history) + 1

	// Count accumulated questions from history.
	accumulated := 0
	for _, h := range history {
		accumulated += len(h.Questions)
	}

	// If last round's answers are ALL empty, the user wants to skip clarification.
	if len(history) > 0 {
		last := history[len(history)-1]
		allEmpty := true
		for _, a := range last.Answers {
			if a.Text != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			if onEvent != nil {
				onEvent("stage0_done", nil)
			}
			return nil
		}
	}

	// Limit checks.
	if round > cfg.MaxRounds || accumulated >= cfg.MaxTotalQuestions {
		if onEvent != nil {
			onEvent("stage0_done", nil)
		}
		return nil
	}

	ct, ok := c.registry[councilType]
	if !ok {
		return fmt.Errorf("council: unknown council type %q", councilType)
	}

	// Resolve the Stage 0 model chain: env override → per-council-type → error.
	// Both fields on cfg are optional. The runner does the resolution (not the
	// config loader) so the per-council-type fall-back hop survives.
	generatorModels := cfg.Models
	if len(generatorModels) == 0 {
		generatorModels = ct.Models
	}
	if len(generatorModels) == 0 {
		return fmt.Errorf("council: stage 0 has no generator models — set CLARIFICATION_MODELS or configure the council type's Models")
	}
	arbiterModel := cfg.ArbiterModel
	if arbiterModel == "" {
		arbiterModel = ct.ChairmanModel
	}
	if arbiterModel == "" {
		return fmt.Errorf("council: stage 0 has no arbiter model — set CLARIFICATION_ARBITER_MODEL or configure the council type's ChairmanModel")
	}

	// Run generators in parallel.
	prompt := BuildStage0GeneratorPrompt(query, history)
	candidates := c.runStage0Generators(ctx, prompt, generatorModels, ct.Temperature)

	// Collect all candidate question texts.
	var allCandidates []string
	for _, qs := range candidates {
		for _, q := range qs {
			allCandidates = append(allCandidates, q.Text)
		}
	}

	// Chairman decision.
	questions, enough, err := c.runStage0Chairman(ctx, query, allCandidates, round, cfg, accumulated, arbiterModel, ct.Temperature)
	if err != nil {
		// Log and fail-open.
		if c.logger != nil {
			c.logger.Warn("stage0: chairman error, falling through to stage 1", "error", err)
		}
		if onEvent != nil {
			onEvent("stage0_done", nil)
		}
		return nil
	}

	if enough || len(questions) == 0 {
		if onEvent != nil {
			onEvent("stage0_done", nil)
		}
		return nil
	}

	// Emit questions to client.
	type roundCompleteData struct {
		Round     int                     `json:"round"`
		Questions []ClarificationQuestion `json:"questions"`
	}
	if onEvent != nil {
		onEvent("stage0_round_complete", roundCompleteData{Round: round, Questions: questions})
	}
	return nil
}

// runStage0Generators sends Stage 0 prompts to all models concurrently and collects
// their proposed clarification questions.
func (c *Council) runStage0Generators(ctx context.Context, prompt []ChatMessage, models []string, temperature float64) [][]ClarificationQuestion {
	results := make([][]ClarificationQuestion, len(models))
	var wg sync.WaitGroup
	for i, model := range models {
		wg.Add(1)
		go func(i int, model string) {
			defer wg.Done()
			resp, err := c.client.Complete(ctx, CompletionRequest{
				Model:          model,
				Messages:       prompt,
				Temperature:    temperature,
				ResponseFormat: &ResponseFormat{Type: "json_object"},
			})
			if err != nil || len(resp.Choices) == 0 {
				results[i] = nil
				return
			}
			var parsed struct {
				Questions []struct {
					Text string `json:"text"`
				} `json:"questions"`
			}
			if err := json.Unmarshal([]byte(StripCodeFence(resp.Choices[0].Message.Content)), &parsed); err != nil {
				if c.logger != nil {
					c.logger.Warn("stage0: generator parse failure", "model", model, "error", err)
				}
				results[i] = nil
				return
			}
			qs := make([]ClarificationQuestion, 0, len(parsed.Questions))
			for _, q := range parsed.Questions {
				if q.Text != "" {
					qs = append(qs, ClarificationQuestion{Text: q.Text})
				}
			}
			results[i] = qs
		}(i, model)
	}
	wg.Wait()
	return results
}

// runStage0Chairman calls the chairman model to dedupe, prioritise, and decide
// whether to ask the user for clarification.
func (c *Council) runStage0Chairman(ctx context.Context, query string, candidates []string, round int, cfg ClarificationConfig, accumulated int, chairmanModel string, temperature float64) ([]ClarificationQuestion, bool, error) {
	prompt := BuildStage0ChairmanPrompt(query, candidates, round, cfg.MaxRounds, cfg.MaxQuestionsPerRound, accumulated, cfg.MaxTotalQuestions)
	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:          chairmanModel,
		Messages:       prompt,
		Temperature:    temperature,
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	})
	if err != nil || len(resp.Choices) == 0 {
		if err == nil {
			err = errNoChoices
		}
		return nil, true, fmt.Errorf("stage0 chairman: %w", err)
	}

	content := StripCodeFence(resp.Choices[0].Message.Content)
	var parsed struct {
		Questions []ClarificationQuestion `json:"questions"`
		Enough    bool                    `json:"enough"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		if c.logger != nil {
			prefix := content
			if len(prefix) > 200 {
				prefix = prefix[:200]
			}
			c.logger.Warn("stage0: chairman parse failure, falling through to stage 1",
				"error", err,
				"response_prefix", prefix,
			)
		}
		return nil, true, nil // fail-open
	}

	// Cap to MaxQuestionsPerRound and remaining budget.
	remaining := cfg.MaxTotalQuestions - accumulated
	limit := cfg.MaxQuestionsPerRound
	if remaining < limit {
		limit = remaining
	}
	if len(parsed.Questions) > limit {
		parsed.Questions = parsed.Questions[:limit]
	}

	return parsed.Questions, parsed.Enough, nil
}

// RunFullWithClarifications augments the original query with clarification history
// then delegates to RunFull.
func (c *Council) RunFullWithClarifications(
	ctx context.Context,
	originalQuery string,
	history []ClarificationRound,
	councilType string,
	onEvent EventFunc,
) error {
	query := BuildAugmentedQuery(originalQuery, history)
	return c.RunFull(ctx, query, councilType, onEvent)
}

// runStage3 calls the Chairman model to synthesize a final answer from the
// Stage 1 responses and Stage 2 peer-review rankings. Sequential — single LLM call.
func (c *Council) runStage3(ctx context.Context, query string, stage2 []StageTwoResult, labelToModel map[string]string, consensusW float64, chairmanModel string, temperature float64, labeledResponses map[string]string) (StageThreeResult, error) {
	start := time.Now()
	resp, err := c.client.Complete(ctx, CompletionRequest{
		Model:       chairmanModel,
		Messages:    BuildStage3Prompt(query, stage2, labelToModel, consensusW, labeledResponses),
		Temperature: temperature,
	})
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return StageThreeResult{Model: chairmanModel, DurationMs: elapsed}, fmt.Errorf("stage3: %w", err)
	}
	if len(resp.Choices) == 0 {
		return StageThreeResult{Model: chairmanModel, DurationMs: elapsed}, fmt.Errorf("stage3: %w", errNoChoices)
	}
	return StageThreeResult{
		Content:    resp.Choices[0].Message.Content,
		Model:      chairmanModel,
		DurationMs: elapsed,
	}, nil
}
