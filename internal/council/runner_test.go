package council

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	complete func(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

func (m *mockLLMClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	return m.complete(ctx, req)
}

func makeResponse(content string) CompletionResponse {
	return CompletionResponse{
		Choices: []struct {
			Message ChatMessage `json:"message"`
		}{
			{Message: ChatMessage{Role: "assistant", Content: content}},
		},
	}
}

// ── runStage1 ─────────────────────────────────────────────────────────────────

func TestRunStage1_AllSucceed(t *testing.T) {
	models := []string{"model-a", "model-b", "model-c"}
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			return makeResponse("answer from " + req.Model), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	results := c.runStage1(context.Background(), "test query", models, 0.7)

	if len(results) != len(models) {
		t.Fatalf("len: got %d, want %d", len(results), len(models))
	}
	for i, r := range results {
		if r.Error != nil {
			t.Errorf("results[%d].Error: unexpected %v", i, r.Error)
		}
		if r.Content == "" {
			t.Errorf("results[%d].Content: empty", i)
		}
		if r.Model != models[i] {
			t.Errorf("results[%d].Model: got %q, want %q", i, r.Model, models[i])
		}
		if r.DurationMs < 0 {
			t.Errorf("results[%d].DurationMs: negative %d", i, r.DurationMs)
		}
	}
}

func TestRunStage1_PartialFailure_ReturnsAll(t *testing.T) {
	errBoom := errors.New("model failed")
	models := []string{"model-a", "model-b", "model-c"}
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			if req.Model == "model-b" {
				return CompletionResponse{}, errBoom
			}
			return makeResponse("ok"), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	results := c.runStage1(context.Background(), "test query", models, 0.7)

	if len(results) != 3 {
		t.Fatalf("len: got %d, want 3", len(results))
	}
	if results[0].Error != nil {
		t.Errorf("results[0]: unexpected error %v", results[0].Error)
	}
	if !errors.Is(results[1].Error, errBoom) {
		t.Errorf("results[1].Error: got %v, want errBoom", results[1].Error)
	}
	if results[1].Content != "" {
		t.Errorf("results[1].Content: want empty on error, got %q", results[1].Content)
	}
	if results[2].Error != nil {
		t.Errorf("results[2]: unexpected error %v", results[2].Error)
	}
}

func TestRunStage1_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before any call

	client := &mockLLMClient{
		complete: func(ctx context.Context, _ CompletionRequest) (CompletionResponse, error) {
			return CompletionResponse{}, ctx.Err()
		},
	}
	c := NewCouncil(client, nil, nil)
	results := c.runStage1(ctx, "test query", []string{"model-a", "model-b"}, 0.7)

	if len(results) != 2 {
		t.Fatalf("len: got %d, want 2", len(results))
	}
	for i, r := range results {
		if !errors.Is(r.Error, context.Canceled) {
			t.Errorf("results[%d].Error: got %v, want context.Canceled", i, r.Error)
		}
	}
}

func TestRunStage1_EmptyChoices_IsError(t *testing.T) {
	client := &mockLLMClient{
		complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
			return CompletionResponse{}, nil // no choices
		},
	}
	c := NewCouncil(client, nil, nil)
	results := c.runStage1(context.Background(), "q", []string{"model-a"}, 0.7)

	if results[0].Error == nil {
		t.Error("expected error for empty choices, got nil")
	}
	if results[0].Content != "" {
		t.Errorf("Content: want empty on error, got %q", results[0].Content)
	}
}

// ── runStage2 ─────────────────────────────────────────────────────────────────

// stage1Fixture returns labeled stage1 results for use in stage2 tests.
func stage1Fixture() []StageOneResult {
	return []StageOneResult{
		{Label: "Response A", Model: "model-a", Content: "answer A"},
		{Label: "Response B", Model: "model-b", Content: "answer B"},
		{Label: "Response C", Model: "model-c", Content: "answer C"},
	}
}

func TestRunStage2_AllSucceed(t *testing.T) {
	stage1 := stage1Fixture()
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			// Each reviewer ranks A > B > C regardless of who they are.
			return makeResponse(`{"rankings":["Response A","Response B","Response C"]}`), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	results := c.runStage2(context.Background(), "q", stage1, 0.7)

	if len(results) != 3 {
		t.Fatalf("len: got %d, want 3", len(results))
	}
	for i, r := range results {
		if r.Error != nil {
			t.Errorf("results[%d].Error: unexpected %v", i, r.Error)
		}
		if len(r.Rankings) != 3 {
			t.Errorf("results[%d].Rankings len: got %d, want 3", i, len(r.Rankings))
		}
		if r.ReviewerLabel != stage1[i].Label {
			t.Errorf("results[%d].ReviewerLabel: got %q, want %q", i, r.ReviewerLabel, stage1[i].Label)
		}
	}
}

func TestRunStage2_ParseFailure_NilRankings_NoError(t *testing.T) {
	client := &mockLLMClient{
		complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
			return makeResponse("not valid json"), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	results := c.runStage2(context.Background(), "q", stage1Fixture(), 0.7)

	for i, r := range results {
		if r.Error != nil {
			t.Errorf("results[%d].Error: want nil on parse failure, got %v", i, r.Error)
		}
		if r.Rankings != nil {
			t.Errorf("results[%d].Rankings: want nil on parse failure, got %v", i, r.Rankings)
		}
	}
}

func TestRunStage2_UnknownLabelsDropped(t *testing.T) {
	client := &mockLLMClient{
		complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
			return makeResponse(`{"rankings":["Response A","Response Z","Response B"]}`), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	results := c.runStage2(context.Background(), "q", stage1Fixture(), 0.7)

	for i, r := range results {
		for _, label := range r.Rankings {
			if label == "Response Z" {
				t.Errorf("results[%d].Rankings: unknown label %q not dropped", i, label)
			}
		}
		// "Response A" and "Response B" should remain
		if len(r.Rankings) != 2 {
			t.Errorf("results[%d].Rankings len: got %d, want 2", i, len(r.Rankings))
		}
	}
}

func TestRunStage2_EmptyOrMissingRankings_TreatedAsMissing(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"missing rankings field", `{}`},
		{"null rankings", `{"rankings":null}`},
		{"empty rankings array", `{"rankings":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &mockLLMClient{
				complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
					return makeResponse(tc.payload), nil
				},
			}
			c := NewCouncil(client, nil, nil)
			results := c.runStage2(context.Background(), "q", stage1Fixture(), 0.7)

			for i, r := range results {
				if r.Error != nil {
					t.Errorf("results[%d].Error: want nil, got %v", i, r.Error)
				}
				if r.Rankings != nil {
					t.Errorf("results[%d].Rankings: want nil, got %v", i, r.Rankings)
				}
			}
		})
	}
}

func TestRunStage2_LLMFailure_SetsError(t *testing.T) {
	errBoom := errors.New("api error")
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			if req.Model == "model-b" {
				return CompletionResponse{}, errBoom
			}
			return makeResponse(`{"rankings":["Response A","Response B","Response C"]}`), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	results := c.runStage2(context.Background(), "q", stage1Fixture(), 0.7)

	if results[0].Error != nil {
		t.Errorf("results[0].Error: unexpected %v", results[0].Error)
	}
	if !errors.Is(results[1].Error, errBoom) {
		t.Errorf("results[1].Error: got %v, want errBoom", results[1].Error)
	}
	if results[2].Error != nil {
		t.Errorf("results[2].Error: unexpected %v", results[2].Error)
	}
}

// ── runStage3 ─────────────────────────────────────────────────────────────────

func TestRunStage3_Success(t *testing.T) {
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			return makeResponse("synthesized answer"), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	result, err := c.runStage3(context.Background(), "q",
		[]StageTwoResult{{ReviewerLabel: "Response A", Rankings: []string{"Response A", "Response B"}}},
		map[string]string{"Response A": "model-a", "Response B": "model-b"},
		0.75, "chairman-model", 0.3,
		map[string]string{"Response A": "answer A", "Response B": "answer B"},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "synthesized answer" {
		t.Errorf("Content: got %q, want %q", result.Content, "synthesized answer")
	}
	if result.Model != "chairman-model" {
		t.Errorf("Model: got %q, want %q", result.Model, "chairman-model")
	}
	if result.DurationMs < 0 {
		t.Errorf("DurationMs: negative %d", result.DurationMs)
	}
}

func TestRunStage3_ClientError_WrappedAndModelPreserved(t *testing.T) {
	errBoom := errors.New("network failure")
	client := &mockLLMClient{
		complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
			return CompletionResponse{}, errBoom
		},
	}
	c := NewCouncil(client, nil, nil)
	result, err := c.runStage3(context.Background(), "q", nil, nil, 0.5, "chairman-model", 0.3, nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("error chain: want errBoom, got %v", err)
	}
	if result.Model != "chairman-model" {
		t.Errorf("Model: got %q, want %q on error", result.Model, "chairman-model")
	}
}

func TestRunStage3_EmptyChoices_WrappedError(t *testing.T) {
	client := &mockLLMClient{
		complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
			return CompletionResponse{}, nil
		},
	}
	c := NewCouncil(client, nil, nil)
	_, err := c.runStage3(context.Background(), "q", nil, nil, 0.5, "chairman-model", 0.3, nil)

	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
	if !errors.Is(err, errNoChoices) {
		t.Errorf("error chain: want errNoChoices, got %v", err)
	}
}

func TestRunStage3_UsesChairmanModel(t *testing.T) {
	var gotModel string
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			gotModel = req.Model
			return makeResponse("ok"), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	c.runStage3(context.Background(), "q", nil, nil, 0.5, "my-chairman", 0.3, nil) //nolint:errcheck

	if gotModel != "my-chairman" {
		t.Errorf("Model: got %q, want %q", gotModel, "my-chairman")
	}
}

func TestRunStage3_Stage1ContentAndTemperatureForwarded(t *testing.T) {
	var gotReq CompletionRequest
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			gotReq = req
			return makeResponse("ok"), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	c.runStage3(context.Background(), "q", nil, nil, 0.5, "chairman", 0.3, //nolint:errcheck
		map[string]string{"Response A": "the actual answer"},
	)

	if gotReq.Temperature != 0.3 {
		t.Errorf("Temperature: got %v, want 0.3", gotReq.Temperature)
	}
	if len(gotReq.Messages) == 0 {
		t.Fatal("Messages: empty")
	}
	if !strings.Contains(gotReq.Messages[0].Content, "the actual answer") {
		t.Error("stage1 content missing from chairman prompt")
	}
}

func TestRunStage2_JsonObjectFormatRequested(t *testing.T) {
	var gotFormat *ResponseFormat
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			gotFormat = req.ResponseFormat
			return makeResponse(`{"rankings":["Response A"]}`), nil
		},
	}
	c := NewCouncil(client, nil, nil)
	c.runStage2(context.Background(), "q", stage1Fixture()[:1], 0.7)

	if gotFormat == nil {
		t.Fatal("ResponseFormat: want non-nil, got nil")
	}
	if gotFormat.Type != "json_object" {
		t.Errorf("ResponseFormat.Type: got %q, want %q", gotFormat.Type, "json_object")
	}
}

// ── RunFull ───────────────────────────────────────────────────────────────────

// councilFixture returns a registry with one council type containing 3 models.
func councilFixture() map[string]CouncilType {
	return map[string]CouncilType{
		"default": {
			Name:          "default",
			Strategy:      PeerReview,
			Models:        []string{"model-a", "model-b", "model-c"},
			ChairmanModel: "chairman",
			Temperature:   0.7,
		},
	}
}

// labelsFromPrompt extracts "Response X" labels from a stage-2 prompt by looking
// for "## Response " heading lines (format produced by BuildStage2Prompt).
func labelsFromPrompt(content string) []string {
	var labels []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## Response ") {
			labels = append(labels, strings.TrimPrefix(line, "## "))
		}
	}
	return labels
}

// fullPipelineClient returns a mock client that succeeds for all calls:
// stage1 returns prose, stage2 extracts labels from the prompt and returns them
// as rankings (so CalculateAggregateRankings produces non-nil output), stage3
// returns a synthesis string.
func fullPipelineClient(t *testing.T) *mockLLMClient {
	t.Helper()
	return &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
				// Stage 2 reviewer — derive labels from the prompt so rankings are valid.
				var labels []string
				if len(req.Messages) > 0 {
					labels = labelsFromPrompt(req.Messages[0].Content)
				}
				type rankResp struct {
					Rankings []string `json:"rankings"`
				}
				b, _ := json.Marshal(rankResp{Rankings: labels})
				return makeResponse(string(b)), nil
			}
			return makeResponse("answer from " + req.Model), nil
		},
	}
}

func TestRunFull_UnknownCouncilType_ReturnsError(t *testing.T) {
	c := NewCouncil(&mockLLMClient{}, councilFixture(), nil)
	err := c.RunFull(context.Background(), "q", "unknown-type", nil)
	if err == nil {
		t.Fatal("expected error for unknown council type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown council type") {
		t.Errorf("error message: got %q, want 'unknown council type'", err.Error())
	}
}

func TestRunFull_QuorumFailure_ReturnsQuorumError(t *testing.T) {
	// All 3 models fail → quorum not met.
	client := &mockLLMClient{
		complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
			return CompletionResponse{}, errors.New("model unavailable")
		},
	}
	c := NewCouncil(client, councilFixture(), nil)
	err := c.RunFull(context.Background(), "q", "default", nil)

	var qe *QuorumError
	if !errors.As(err, &qe) {
		t.Fatalf("expected *QuorumError, got %T: %v", err, err)
	}
}

func TestRunFull_QuorumFailure_NoStage2Or3Events(t *testing.T) {
	client := &mockLLMClient{
		complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
			return CompletionResponse{}, errors.New("model unavailable")
		},
	}
	c := NewCouncil(client, councilFixture(), nil)

	var emitted []string
	_ = c.RunFull(context.Background(), "q", "default", func(eventType string, _ any) {
		emitted = append(emitted, eventType)
	})

	for _, et := range emitted {
		if et == "stage2_complete" || et == "stage3_complete" {
			t.Errorf("unexpected event %q emitted after quorum failure", et)
		}
	}
}

func TestRunFull_HappyPath_EmitsAllThreeEvents(t *testing.T) {
	c := NewCouncil(fullPipelineClient(t), councilFixture(), nil)

	var emitted []string
	err := c.RunFull(context.Background(), "q", "default", func(eventType string, _ any) {
		emitted = append(emitted, eventType)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"stage1_complete", "stage2_complete", "stage3_complete"}
	if len(emitted) != len(want) {
		t.Fatalf("events: got %v, want %v", emitted, want)
	}
	for i, e := range emitted {
		if e != want[i] {
			t.Errorf("events[%d]: got %q, want %q", i, e, want[i])
		}
	}
}

func TestRunFull_Stage2CompletePayload_IsStage2CompleteData(t *testing.T) {
	c := NewCouncil(fullPipelineClient(t), councilFixture(), nil)

	var stage2Data any
	_ = c.RunFull(context.Background(), "q", "default", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			stage2Data = data
		}
	})

	d, ok := stage2Data.(Stage2CompleteData)
	if !ok {
		t.Fatalf("stage2_complete payload: got %T, want Stage2CompleteData", stage2Data)
	}
	if d.Metadata.CouncilType != "default" {
		t.Errorf("Metadata.CouncilType: got %q, want %q", d.Metadata.CouncilType, "default")
	}
	if len(d.Metadata.LabelToModel) == 0 {
		t.Error("Metadata.LabelToModel: empty")
	}
	// AggregateRankings must be non-empty and contain real model names, not labels.
	if len(d.Metadata.AggregateRankings) == 0 {
		t.Error("Metadata.AggregateRankings: empty")
	}
	expectedModels := map[string]bool{"model-a": true, "model-b": true, "model-c": true}
	for _, r := range d.Metadata.AggregateRankings {
		if strings.HasPrefix(r.Model, "Response ") {
			t.Errorf("AggregateRankings entry %q contains a label instead of a model name", r.Model)
		}
		if !expectedModels[r.Model] {
			t.Errorf("AggregateRankings entry %q is not a known model", r.Model)
		}
	}
}

func TestRunFull_UnimplementedStrategy_ReturnsError(t *testing.T) {
	unimplemented := []Strategy{
		Majority, GenerateRankRefine, MultiAgentDebate, MixtureOfAgents, Delphi,
	}
	for _, s := range unimplemented {
		s := s
		t.Run(fmt.Sprintf("strategy_%d", s), func(t *testing.T) {
			registry := map[string]CouncilType{
				"test": {Name: "test", Strategy: s, Models: []string{"m1"}},
			}
			c := NewCouncil(&mockLLMClient{}, registry, nil)
			err := c.RunFull(context.Background(), "q", "test", nil)
			if err == nil || !strings.Contains(err.Error(), "not implemented") {
				t.Fatalf("expected 'not implemented' error, got %v", err)
			}
		})
	}
}
