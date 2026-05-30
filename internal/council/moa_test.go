package council

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
)

// moaRecorder is a thread-safe collector that classifies each completion
// request as a Stage 1 proposer call, a Layer 2 aggregator call, or a Layer 3
// refiner call by inspecting the prompt prefix produced by the prompt builders.
type moaRecorder struct {
	mu sync.Mutex
	// proposerOut maps model → answer to return for Stage 1 calls.
	proposerOut map[string]string
	// proposerErr maps model → error to return for Stage 1 calls (overrides proposerOut when set).
	proposerErr map[string]error
	// aggregatorOut maps model → answer to return for Layer 2 calls.
	aggregatorOut map[string]string
	// aggregatorErr maps model → error to return for Layer 2 calls.
	aggregatorErr map[string]error
	// refinerOut / refinerErr drive Layer 3.
	refinerOut string
	refinerErr error

	// Captured per-call data.
	calls            []string // labels: "stage1:<model>", "agg:<model>", "refiner:<model>"
	aggregatorPrompt string   // shared aggregator prompt body (one per call, same content)
	aggregatorPromptByModel map[string]string // model → captured prompt (all aggregators get the same one, but per-call captured)
	refinerPrompt    string   // captured refiner prompt body
}

func (r *moaRecorder) record(req CompletionRequest) (CompletionResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	body := ""
	if len(req.Messages) > 0 {
		body = req.Messages[0].Content
	}
	switch {
	case strings.Contains(body, "You aggregate council proposals"):
		r.calls = append(r.calls, "agg:"+req.Model)
		r.aggregatorPrompt = body
		if r.aggregatorPromptByModel == nil {
			r.aggregatorPromptByModel = map[string]string{}
		}
		r.aggregatorPromptByModel[req.Model] = body
		if e, ok := r.aggregatorErr[req.Model]; ok {
			return CompletionResponse{}, e
		}
		out, ok := r.aggregatorOut[req.Model]
		if !ok {
			out = "default aggregator draft from " + req.Model
		}
		return makeResponse(out), nil
	case strings.Contains(body, "You synthesise the final answer from MoA aggregator drafts"):
		r.calls = append(r.calls, "refiner:"+req.Model)
		r.refinerPrompt = body
		if r.refinerErr != nil {
			return CompletionResponse{}, r.refinerErr
		}
		return makeResponse(r.refinerOut), nil
	default:
		r.calls = append(r.calls, "stage1:"+req.Model)
		if e, ok := r.proposerErr[req.Model]; ok {
			return CompletionResponse{}, e
		}
		ans, ok := r.proposerOut[req.Model]
		if !ok {
			return CompletionResponse{}, errors.New("no answer scripted for " + req.Model)
		}
		return makeResponse(ans), nil
	}
}

func moaCouncil(t *testing.T, proposers, aggregators []string, refiner string, rec *moaRecorder) *Rada {
	t.Helper()
	registry := map[string]CouncilType{
		"test": {
			Name:             "test",
			Strategy:         MixtureOfAgents,
			ProposerModels:   proposers,
			AggregatorModels: aggregators,
			RefinerModel:     refiner,
			Temperature:      0.7,
		},
	}
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			return rec.record(req)
		},
	}
	return NewCouncil(client, registry, nil)
}

// ── 1. Happy path ─────────────────────────────────────────────────────────

func TestRunMixtureOfAgents_HappyPath(t *testing.T) {
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorOut: map[string]string{
			"agg-1": "improved draft from agg-1",
			"agg-2": "improved draft from agg-2",
		},
		refinerOut: "FINAL ANSWER",
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	var events []string
	var seenKind string
	var moa *MoaAggregator
	var stage3 StageThreeResult

	err := c.RunFull(context.Background(), "the question?", "test", func(eventType string, data any) {
		events = append(events, eventType)
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				seenKind = d.Kind
				moa = d.Metadata.MoaAggregator
			}
		}
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSeq := []string{"stage1_complete", "stage2_complete", "stage3_complete"}
	if !equalStringSlice(events, wantSeq) {
		t.Errorf("event sequence: got %v, want %v", events, wantSeq)
	}
	if seenKind != "moa_aggregator" {
		t.Errorf("Stage 2 Kind: got %q, want %q", seenKind, "moa_aggregator")
	}
	if moa == nil {
		t.Fatal("Metadata.MoaAggregator: nil")
	}
	if len(moa.Aggregators) != 2 {
		t.Fatalf("Aggregators: got %d, want 2", len(moa.Aggregators))
	}
	for _, a := range moa.Aggregators {
		if len(a.Sources) != 4 {
			t.Errorf("aggregator %q sources: got %d, want 4 (all-to-all)", a.Label, len(a.Sources))
		}
	}
	if stage3.Content != "FINAL ANSWER" {
		t.Errorf("stage3 content: got %q", stage3.Content)
	}
	if stage3.Model != "refiner-z" {
		t.Errorf("stage3 model: got %q, want %q", stage3.Model, "refiner-z")
	}
}

// ── 2. Layer 1 quorum failure ─────────────────────────────────────────────

func TestRunMixtureOfAgents_Layer1QuorumFailure(t *testing.T) {
	// All proposers fail → quorum not met. No Stage 2 / 3 events.
	rec := &moaRecorder{
		proposerErr: map[string]error{
			"prop-a": errors.New("upstream"), "prop-b": errors.New("upstream"),
			"prop-c": errors.New("upstream"), "prop-d": errors.New("upstream"),
		},
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	var events []string
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, _ any) {
		events = append(events, eventType)
	})

	var qe *QuorumError
	if !errors.As(err, &qe) {
		t.Fatalf("expected *QuorumError, got %T: %v", err, err)
	}
	for _, e := range events {
		if e == "stage2_complete" || e == "stage3_complete" {
			t.Errorf("must NOT emit %q after Layer 1 quorum failure (events: %v)", e, events)
		}
	}
}

// ── 3. All aggregators fail (Layer 2 loud failure) ────────────────────────

func TestRunMixtureOfAgents_AllAggregatorsFail(t *testing.T) {
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorErr: map[string]error{
			"agg-1": errors.New("upstream"),
			"agg-2": errors.New("upstream"),
		},
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	var events []string
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, _ any) {
		events = append(events, eventType)
	})

	if err == nil {
		t.Fatal("expected loud error when all aggregators fail")
	}
	if !strings.Contains(err.Error(), "all aggregators failed") {
		t.Errorf("error message: got %q, want mention of 'all aggregators failed'", err.Error())
	}
	for _, e := range events {
		if e == "stage2_complete" {
			t.Errorf("must NOT emit stage2_complete when all aggregators failed (events: %v)", events)
		}
		if e == "stage3_complete" {
			t.Errorf("must NOT emit stage3_complete when all aggregators failed (events: %v)", events)
		}
	}
}

// ── 4. Partial aggregator failure ─────────────────────────────────────────

func TestRunMixtureOfAgents_PartialAggregatorFailure(t *testing.T) {
	// One aggregator fails, the other succeeds → refiner runs with the survivor.
	// Stage 2 metadata still lists BOTH aggregators (the failed one with Error
	// surfaced via the `Error` field, omitted from the wire JSON).
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorOut: map[string]string{
			"agg-good": "good draft",
		},
		aggregatorErr: map[string]error{
			"agg-bad": errors.New("upstream"),
		},
		refinerOut: "synthesis",
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-good", "agg-bad"}, "refiner-z", rec)

	var moa *MoaAggregator
	var stage3 StageThreeResult
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				moa = d.Metadata.MoaAggregator
			}
		}
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if moa == nil || len(moa.Aggregators) != 2 {
		t.Fatalf("expected 2 aggregator entries (incl. failed), got %v", moa)
	}
	successCount := 0
	for _, a := range moa.Aggregators {
		if a.Error == nil && a.Content != "" {
			successCount++
		}
	}
	if successCount != 1 {
		t.Errorf("successful aggregator count: got %d, want 1", successCount)
	}
	if stage3.Content != "synthesis" {
		t.Errorf("stage3 content: got %q, want %q", stage3.Content, "synthesis")
	}
}

// ── 5. Anonymisation (proposer model names not in aggregator prompts) ───

func TestRunMixtureOfAgents_AnonymisationInAggregatorPrompts(t *testing.T) {
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"my-secret-model-a": "draft a",
			"my-secret-model-b": "draft b",
			"my-secret-model-c": "draft c",
			"my-secret-model-d": "draft d",
		},
		aggregatorOut: map[string]string{
			"agg-1": "draft", "agg-2": "draft",
		},
		refinerOut: "ok",
	}
	c := moaCouncil(t,
		[]string{"my-secret-model-a", "my-secret-model-b", "my-secret-model-c", "my-secret-model-d"},
		[]string{"agg-1", "agg-2"}, "refiner-z", rec)

	if err := c.RunFull(context.Background(), "q", "test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for model, prompt := range rec.aggregatorPromptByModel {
		// Proposer model names MUST NOT appear in aggregator prompts.
		for _, secret := range []string{"my-secret-model-a", "my-secret-model-b", "my-secret-model-c", "my-secret-model-d"} {
			if strings.Contains(prompt, secret) {
				t.Errorf("aggregator %q prompt leaks proposer model %q:\n%s", model, secret, prompt)
			}
		}
		// At least one proposer label MUST appear.
		hasAny := false
		for _, lbl := range []string{"Response A", "Response B", "Response C", "Response D"} {
			if strings.Contains(prompt, lbl) {
				hasAny = true
				break
			}
		}
		if !hasAny {
			t.Errorf("aggregator %q prompt has no proposer labels:\n%s", model, prompt)
		}
	}
}

// ── 6. Source provenance (Sources lists ALL proposers, all-to-all) ────────

func TestRunMixtureOfAgents_SourceProvenance(t *testing.T) {
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorOut: map[string]string{
			"agg-1": "draft", "agg-2": "draft",
		},
		refinerOut: "ok",
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	var moa *MoaAggregator
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				moa = d.Metadata.MoaAggregator
			}
		}
	})
	if moa == nil {
		t.Fatal("moa nil")
	}
	for _, a := range moa.Aggregators {
		if len(a.Sources) != 4 {
			t.Errorf("aggregator %q sources: got %d (%v), want 4 (all-to-all)", a.Label, len(a.Sources), a.Sources)
		}
		// Sources must be sorted ascending for determinism.
		sorted := append([]string(nil), a.Sources...)
		sort.Strings(sorted)
		if !equalStringSlice(a.Sources, sorted) {
			t.Errorf("aggregator %q sources not sorted ascending: %v", a.Label, a.Sources)
		}
	}
}

// ── 7. Refiner input contract (aggregators only, no raw proposers) ────────

func TestRunMixtureOfAgents_RefinerInputContract(t *testing.T) {
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "PROPOSER-A-CONTENT",
			"prop-b": "PROPOSER-B-CONTENT",
			"prop-c": "PROPOSER-C-CONTENT",
			"prop-d": "PROPOSER-D-CONTENT",
		},
		aggregatorOut: map[string]string{
			"agg-1": "AGGREGATOR-1-DRAFT",
			"agg-2": "AGGREGATOR-2-DRAFT",
		},
		refinerOut: "ok",
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	if err := c.RunFull(context.Background(), "q", "test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Refiner prompt MUST contain aggregator drafts.
	for _, want := range []string{"AGGREGATOR-1-DRAFT", "AGGREGATOR-2-DRAFT"} {
		if !strings.Contains(rec.refinerPrompt, want) {
			t.Errorf("refiner prompt missing aggregator content %q", want)
		}
	}
	// Refiner prompt MUST contain aggregator model attribution.
	for _, want := range []string{"agg-1", "agg-2"} {
		if !strings.Contains(rec.refinerPrompt, want) {
			t.Errorf("refiner prompt missing aggregator model %q", want)
		}
	}
	// Refiner prompt MUST NOT contain raw proposer content.
	for _, raw := range []string{"PROPOSER-A-CONTENT", "PROPOSER-B-CONTENT", "PROPOSER-C-CONTENT", "PROPOSER-D-CONTENT"} {
		if strings.Contains(rec.refinerPrompt, raw) {
			t.Errorf("refiner prompt leaks raw proposer content %q (refiner should only see aggregator drafts):\n%s", raw, rec.refinerPrompt)
		}
	}
}

// ── 8. Stage 2 kind ─────────────────────────────────────────────────────

func TestRunMixtureOfAgents_Stage2Kind(t *testing.T) {
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorOut: map[string]string{
			"agg-1": "draft", "agg-2": "draft",
		},
		refinerOut: "ok",
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	var seenKind string
	var sawRoundEvent bool
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				seenKind = d.Kind
			}
		}
		if eventType == "stage2_round_complete" {
			sawRoundEvent = true
		}
	})
	if seenKind != "moa_aggregator" {
		t.Errorf("kind: got %q, want %q", seenKind, "moa_aggregator")
	}
	if sawRoundEvent {
		t.Error("MoA must not emit stage2_round_complete (single-pass per layer)")
	}
}

// ── 9. Missing model fields error early ────────────────────────────────

func TestRunMixtureOfAgents_MissingFieldsErrorsEarly(t *testing.T) {
	tests := []struct {
		name string
		ct   CouncilType
		want string
	}{
		{
			name: "missing proposers",
			ct: CouncilType{
				Name:             "test",
				Strategy:         MixtureOfAgents,
				AggregatorModels: []string{"agg-1"},
				RefinerModel:     "refiner-z",
			},
			want: "no proposer models",
		},
		{
			name: "missing aggregators",
			ct: CouncilType{
				Name:           "test",
				Strategy:       MixtureOfAgents,
				ProposerModels: []string{"prop-a"},
				RefinerModel:   "refiner-z",
			},
			want: "no aggregator models",
		},
		{
			name: "missing refiner",
			ct: CouncilType{
				Name:             "test",
				Strategy:         MixtureOfAgents,
				ProposerModels:   []string{"prop-a"},
				AggregatorModels: []string{"agg-1"},
			},
			want: "no refiner model",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			client := &mockLLMClient{
				complete: func(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
					calls++
					return makeResponse("should not be called"), nil
				},
			}
			c := NewCouncil(client, map[string]CouncilType{"test": tc.ct}, nil)
			err := c.RunFull(context.Background(), "q", "test", nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error message: got %q, want mention of %q", err.Error(), tc.want)
			}
			if calls != 0 {
				t.Errorf("expected 0 LLM calls before missing-field error, got %d", calls)
			}
		})
	}
}

// ── 10. Aggregator labels distinct from proposer labels ────────────────

func TestRunMixtureOfAgents_DistinctLabelFamilies(t *testing.T) {
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorOut: map[string]string{
			"agg-1": "draft", "agg-2": "draft",
		},
		refinerOut: "ok",
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	var labelToModel map[string]string
	var moa *MoaAggregator
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				labelToModel = d.Metadata.LabelToModel
				moa = d.Metadata.MoaAggregator
			}
		}
	})

	// LabelToModel must contain BOTH proposer- and aggregator-prefixed entries.
	var proposerCount, aggregatorCount int
	for label := range labelToModel {
		switch {
		case strings.HasPrefix(label, "Response "):
			proposerCount++
		case strings.HasPrefix(label, "Aggregator "):
			aggregatorCount++
		default:
			t.Errorf("unexpected label prefix in LabelToModel: %q", label)
		}
	}
	if proposerCount != 4 {
		t.Errorf("proposer-prefixed entries: got %d, want 4", proposerCount)
	}
	if aggregatorCount != 2 {
		t.Errorf("aggregator-prefixed entries: got %d, want 2", aggregatorCount)
	}
	// Aggregator labels in MoaAggregator MUST use the "Aggregator " prefix.
	for _, a := range moa.Aggregators {
		if !strings.HasPrefix(a.Label, "Aggregator ") {
			t.Errorf("aggregator label %q missing 'Aggregator ' prefix", a.Label)
		}
	}
}

// ── 11. Refiner failure populates Model + DurationMs ───────────────────

func TestRunMixtureOfAgents_RefinerFailurePopulatesModel(t *testing.T) {
	errBoom := errors.New("refiner upstream")
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorOut: map[string]string{
			"agg-1": "draft", "agg-2": "draft",
		},
		refinerErr: errBoom,
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	err := c.RunFull(context.Background(), "q", "test", nil)
	if err == nil {
		t.Fatal("expected refiner error")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("error chain: want errBoom, got %v", err)
	}
	// runMoaRefine returns StageThreeResult{Model, DurationMs, Error} on the
	// error path. The package-level test surface for that is via runMoaRefine
	// directly (RunFull discards the result on error).
	stage3, refineErr := c.runMoaRefine(context.Background(), "q",
		[]AggregatorOutput{{Label: "Aggregator A", Model: "agg-1", Content: "draft", Sources: []string{"Response A"}}},
		map[string]string{"Aggregator A": "agg-1"},
		"refiner-z", 0.7,
	)
	if refineErr == nil {
		t.Fatal("expected error from runMoaRefine")
	}
	if stage3.Model != "refiner-z" {
		t.Errorf("Model: got %q, want %q on error", stage3.Model, "refiner-z")
	}
	if stage3.DurationMs < 0 {
		t.Errorf("DurationMs: negative %d", stage3.DurationMs)
	}
	if stage3.Error == nil {
		t.Error("Error: nil; want populated on failure path")
	}
}

// ── 12. Sort stability ────────────────────────────────────────────────

func TestRunMixtureOfAgents_SortStability(t *testing.T) {
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorOut: map[string]string{
			"agg-1": "draft 1", "agg-2": "draft 2", "agg-3": "draft 3",
		},
		refinerOut: "ok",
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2", "agg-3"}, "refiner-z", rec)

	var moa *MoaAggregator
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				moa = d.Metadata.MoaAggregator
			}
		}
	})
	if moa == nil {
		t.Fatal("moa nil")
	}
	for i := 1; i < len(moa.Aggregators); i++ {
		if moa.Aggregators[i-1].Label > moa.Aggregators[i].Label {
			t.Errorf("aggregators not sorted ascending by Label: %+v", labelsOf(moa.Aggregators))
			break
		}
	}
}

// ── 13. Transcript-agreement contract ────────────────────────────────

func TestRunMixtureOfAgents_TranscriptAgreement(t *testing.T) {
	// For every emitted AggregatorOutput, every label in Sources MUST appear in
	// the captured prompt body, and the prompt body MUST contain no extras
	// (i.e., the prompt's set of Layer 1 labels equals Sources). Catches
	// runner/prompt bookkeeping divergence.
	rec := &moaRecorder{
		proposerOut: map[string]string{
			"prop-a": "draft-a", "prop-b": "draft-b", "prop-c": "draft-c", "prop-d": "draft-d",
		},
		aggregatorOut: map[string]string{
			"agg-1": "draft 1", "agg-2": "draft 2",
		},
		refinerOut: "ok",
	}
	c := moaCouncil(t, []string{"prop-a", "prop-b", "prop-c", "prop-d"}, []string{"agg-1", "agg-2"}, "refiner-z", rec)

	var moa *MoaAggregator
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				moa = d.Metadata.MoaAggregator
			}
		}
	})
	if moa == nil {
		t.Fatal("moa nil")
	}
	for _, a := range moa.Aggregators {
		prompt, ok := rec.aggregatorPromptByModel[a.Model]
		if !ok {
			t.Errorf("no captured prompt for aggregator %q (model %q)", a.Label, a.Model)
			continue
		}
		// 1. Every label in Sources MUST appear in the prompt body.
		for _, lbl := range a.Sources {
			needle := fmt.Sprintf("[%s]", lbl)
			if !strings.Contains(prompt, needle) {
				t.Errorf("aggregator %q: source %q not present in prompt body", a.Label, lbl)
			}
		}
		// 2. No extra proposer-prefixed labels beyond Sources.
		labelsInPrompt := extractProposerLabelsFromPrompt(prompt)
		got := append([]string(nil), labelsInPrompt...)
		want := append([]string(nil), a.Sources...)
		sort.Strings(got)
		sort.Strings(want)
		if !equalStringSlice(got, want) {
			t.Errorf("aggregator %q: prompt labels ≠ Sources\nprompt has: %v\nSources:    %v", a.Label, got, want)
		}
	}
}

// ── helpers ────────────────────────────────────────────────────────────

func labelsOf(aggs []AggregatorOutput) []string {
	out := make([]string, len(aggs))
	for i, a := range aggs {
		out[i] = a.Label
	}
	return out
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// extractProposerLabelsFromPrompt returns every "Response X" label that
// appears in the prompt body inside `[...]` brackets — the format used by
// BuildMoaAggregatorPrompt for proposer drafts.
func extractProposerLabelsFromPrompt(prompt string) []string {
	var out []string
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "[Response ") && strings.HasSuffix(line, "]") {
			out = append(out, strings.Trim(line, "[]"))
		}
	}
	return out
}
