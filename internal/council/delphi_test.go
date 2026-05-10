package council

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// delphiRecorder is a thread-safe collector that classifies each completion
// request as a Stage 1 generation, a Delphi rating call, or a Stage 3
// chairman call by inspecting the prompt prefix.
type delphiRecorder struct {
	mu sync.Mutex

	stage1 map[string]string

	// roundOutByModel[round][model] → JSON {"ratings":{...},"summary":"..."}
	roundOutByModel map[int]map[string]string
	// roundErrByModel[round][model] → error
	roundErrByModel map[int]map[string]error
	// roundDefaults is the default rating JSON if no per-(round,model) entry exists.
	roundDefaults string

	chairmanOut string
	chairmanErr error

	calls          []string
	ratingPrompts  []string
	ratingPromptsByRound map[int][]string
	ratingPromptByRoundModel map[int]map[string]string
	chairmanPrompt string
}

func (r *delphiRecorder) record(req CompletionRequest) (CompletionResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	body := ""
	if len(req.Messages) > 0 {
		body = req.Messages[0].Content
	}
	switch {
	case strings.Contains(body, "You rate council answers"):
		// Determine round from the prompt header "round N of M".
		round := 0
		fmt.Sscanf(strings.SplitN(body[strings.Index(body, "This is round "):], "\n", 2)[0], "This is round %d", &round)
		r.calls = append(r.calls, fmt.Sprintf("rate:%s:r%d", req.Model, round))
		r.ratingPrompts = append(r.ratingPrompts, body)
		if r.ratingPromptsByRound == nil {
			r.ratingPromptsByRound = map[int][]string{}
		}
		r.ratingPromptsByRound[round] = append(r.ratingPromptsByRound[round], body)
		if r.ratingPromptByRoundModel == nil {
			r.ratingPromptByRoundModel = map[int]map[string]string{}
		}
		if r.ratingPromptByRoundModel[round] == nil {
			r.ratingPromptByRoundModel[round] = map[string]string{}
		}
		r.ratingPromptByRoundModel[round][req.Model] = body

		if errMap, ok := r.roundErrByModel[round]; ok {
			if e, ok := errMap[req.Model]; ok {
				return CompletionResponse{}, e
			}
		}
		if outMap, ok := r.roundOutByModel[round]; ok {
			if out, ok := outMap[req.Model]; ok {
				return makeResponse(out), nil
			}
		}
		out := r.roundDefaults
		if out == "" {
			out = `{"ratings":{"correctness":0.5,"clarity":0.5,"completeness":0.5},"summary":"default"}`
		}
		return makeResponse(out), nil
	case strings.Contains(body, "You synthesise the final answer from a Delphi rating panel"):
		r.calls = append(r.calls, "chairman:"+req.Model)
		r.chairmanPrompt = body
		if r.chairmanErr != nil {
			return CompletionResponse{}, r.chairmanErr
		}
		return makeResponse(r.chairmanOut), nil
	default:
		r.calls = append(r.calls, "stage1:"+req.Model)
		ans, ok := r.stage1[req.Model]
		if !ok {
			return CompletionResponse{}, errors.New("no answer scripted for " + req.Model)
		}
		return makeResponse(ans), nil
	}
}

func delphiCouncil(t *testing.T, models []string, chairman string, maxRounds int, threshold float64, rec *delphiRecorder) *Council {
	t.Helper()
	registry := map[string]CouncilType{
		"test": {
			Name:                       "test",
			Strategy:                   Delphi,
			Models:                     models,
			ChairmanModel:              chairman,
			Temperature:                0.7,
			MaxDelphiRounds:            maxRounds,
			DelphiConvergenceThreshold: threshold,
		},
	}
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			return rec.record(req)
		},
	}
	return NewCouncil(client, registry, nil)
}

// ratingJSON is a fixture helper for scripting per-round per-model ratings.
func ratingJSON(correctness, clarity, completeness float64, summary string) string {
	return fmt.Sprintf(`{"ratings":{"correctness":%.2f,"clarity":%.2f,"completeness":%.2f},"summary":%q}`,
		correctness, clarity, completeness, summary)
}

// ── 1. Happy path (no convergence) ────────────────────────────────────────

func TestRunDelphi_HappyPath_NoConvergence(t *testing.T) {
	// Threshold tight enough that DeltaMean stays above it across all 3 rounds —
	// strategy runs to MaxRounds and exits with Converged: false.
	rec := &delphiRecorder{
		stage1: map[string]string{
			"m-a": "ans-a", "m-b": "ans-b", "m-c": "ans-c", "m-d": "ans-d",
		},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": ratingJSON(0.9, 0.9, 0.9, "r1 a"),
				"m-b": ratingJSON(0.6, 0.6, 0.6, "r1 b"),
				"m-c": ratingJSON(0.3, 0.3, 0.3, "r1 c"),
				"m-d": ratingJSON(0.1, 0.1, 0.1, "r1 d"),
			},
			2: {
				"m-a": ratingJSON(0.5, 0.5, 0.5, "r2 a"), // big shift from 0.9
				"m-b": ratingJSON(0.5, 0.5, 0.5, "r2 b"),
				"m-c": ratingJSON(0.5, 0.5, 0.5, "r2 c"),
				"m-d": ratingJSON(0.9, 0.9, 0.9, "r2 d"), // big shift from 0.1
			},
			3: {
				"m-a": ratingJSON(0.0, 0.0, 0.0, "r3 a"), // big shift from 0.5
				"m-b": ratingJSON(0.0, 0.0, 0.0, "r3 b"),
				"m-c": ratingJSON(1.0, 1.0, 1.0, "r3 c"), // big shift
				"m-d": ratingJSON(1.0, 1.0, 1.0, "r3 d"),
			},
		},
		chairmanOut: "FINAL",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 3, 0.05, rec)

	var events []string
	var rounds []int
	var seenKind string
	var panel *DelphiPanel
	var stage3 StageThreeResult

	err := c.RunFull(context.Background(), "the question?", "test", func(eventType string, data any) {
		events = append(events, eventType)
		if eventType == "stage2_round_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				rounds = append(rounds, d.Round)
			}
		}
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				seenKind = d.Kind
				panel = d.Metadata.DelphiPanel
			}
		}
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSeq := []string{"stage1_complete",
		"stage2_round_complete", "stage2_round_complete", "stage2_round_complete",
		"stage2_complete", "stage3_complete"}
	if !reflect.DeepEqual(events, wantSeq) {
		t.Errorf("event sequence: got %v, want %v", events, wantSeq)
	}
	if !reflect.DeepEqual(rounds, []int{1, 2, 3}) {
		t.Errorf("round events: got %v, want [1 2 3]", rounds)
	}
	if seenKind != "delphi_round" {
		t.Errorf("Stage 2 Kind: got %q, want %q", seenKind, "delphi_round")
	}
	if panel == nil || panel.FinalRound != 3 || panel.Converged {
		t.Errorf("panel: FinalRound=3 Converged=false expected, got %+v", panel)
	}
	if stage3.Content != "FINAL" {
		t.Errorf("stage3 content: got %q", stage3.Content)
	}
}

// ── 2. Early convergence ──────────────────────────────────────────────────

func TestRunDelphi_EarlyConvergence(t *testing.T) {
	// Round 1 ratings spread; round 2 ratings move close to round 1 means
	// → DeltaMean < threshold → exit after round 2 with Converged: true.
	rec := &delphiRecorder{
		stage1: map[string]string{
			"m-a": "ans-a", "m-b": "ans-b", "m-c": "ans-c", "m-d": "ans-d",
		},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": ratingJSON(0.5, 0.5, 0.5, "r1 a"),
				"m-b": ratingJSON(0.5, 0.5, 0.5, "r1 b"),
				"m-c": ratingJSON(0.5, 0.5, 0.5, "r1 c"),
				"m-d": ratingJSON(0.5, 0.5, 0.5, "r1 d"),
			},
			2: {
				// All shift by 0.02 — well below threshold 0.1.
				"m-a": ratingJSON(0.52, 0.52, 0.52, "r2 a"),
				"m-b": ratingJSON(0.52, 0.52, 0.52, "r2 b"),
				"m-c": ratingJSON(0.52, 0.52, 0.52, "r2 c"),
				"m-d": ratingJSON(0.52, 0.52, 0.52, "r2 d"),
			},
		},
		chairmanOut: "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 5, 0.1, rec)

	var roundCount int
	var panel *DelphiPanel
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_round_complete" {
			roundCount++
		}
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				panel = d.Metadata.DelphiPanel
			}
		}
	})
	if roundCount != 2 {
		t.Errorf("round events: got %d, want 2 (early convergence)", roundCount)
	}
	if panel == nil || panel.FinalRound != 2 || !panel.Converged {
		t.Errorf("panel: FinalRound=2 Converged=true expected, got %+v", panel)
	}
}

// ── 3. Stage 1 quorum failure ────────────────────────────────────────────

func TestRunDelphi_Stage1QuorumFailure(t *testing.T) {
	rec := &delphiRecorder{stage1: map[string]string{}} // no scripted answers
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 3, 0.1, rec)

	err := c.RunFull(context.Background(), "q", "test", nil)
	var qe *QuorumError
	if !errors.As(err, &qe) {
		t.Fatalf("expected *QuorumError, got %T: %v", err, err)
	}
}

// ── 4. Per-round quorum failure ──────────────────────────────────────────

func TestRunDelphi_PerRoundQuorumFailure(t *testing.T) {
	// Need = max(3, ⌈4/2⌉+1) = 3 with 4 raters. Two fail in round 1 → 2
	// survivors → quorum re-check fires loud error.
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": ratingJSON(0.5, 0.5, 0.5, "ok"),
				"m-b": ratingJSON(0.5, 0.5, 0.5, "ok"),
			},
		},
		roundErrByModel: map[int]map[string]error{
			1: {
				"m-c": errors.New("fail c"),
				"m-d": errors.New("fail d"),
			},
		},
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 3, 0.1, rec)

	var stage2CompleteEmitted bool
	var stage3Emitted bool
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, _ any) {
		if eventType == "stage2_complete" {
			stage2CompleteEmitted = true
		}
		if eventType == "stage3_complete" {
			stage3Emitted = true
		}
	})
	if err == nil {
		t.Fatal("expected loud error on per-round quorum drop")
	}
	if !strings.Contains(err.Error(), "delphi: quorum failed after round 1") {
		t.Errorf("error: got %q, want mention of 'delphi: quorum failed after round 1'", err.Error())
	}
	if stage2CompleteEmitted {
		t.Error("must NOT emit terminal stage2_complete on quorum failure")
	}
	if stage3Emitted {
		t.Error("must NOT emit stage3_complete on quorum failure")
	}
}

// ── 5. Per-rater JSON parse failure (drop) ───────────────────────────────

func TestRunDelphi_PerRaterJSONParseFailureDrops(t *testing.T) {
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": ratingJSON(0.5, 0.5, 0.5, "ok"),
				"m-b": "not valid json {{{",
				"m-c": ratingJSON(0.5, 0.5, 0.5, "ok"),
				"m-d": ratingJSON(0.5, 0.5, 0.5, "ok"),
			},
		},
		roundDefaults: ratingJSON(0.5, 0.5, 0.5, "default"),
		chairmanOut:   "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 2, 0.1, rec)

	var panel *DelphiPanel
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				panel = d.Metadata.DelphiPanel
			}
		}
	})
	if panel == nil {
		t.Fatal("panel nil")
	}
	// Round 1 should have 3 successful ratings (m-b dropped).
	if len(panel.Rounds[0].Ratings) != 3 {
		t.Errorf("round 1 ratings: got %d, want 3", len(panel.Rounds[0].Ratings))
	}
	// Round 2 should also have 3 (m-b stays out).
	if len(panel.Rounds[1].Ratings) != 3 {
		t.Errorf("round 2 ratings: got %d, want 3", len(panel.Rounds[1].Ratings))
	}
}

// ── 6. Missing-criterion handling ────────────────────────────────────────

func TestRunDelphi_MissingCriterionDefaultsToZero(t *testing.T) {
	// One rater returns ratings dict missing "completeness" → criterion
	// defaults to 0.0; rater stays in.
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": `{"ratings":{"correctness":0.8,"clarity":0.8},"summary":"missing one"}`,
				"m-b": ratingJSON(0.5, 0.5, 0.5, "ok"),
				"m-c": ratingJSON(0.5, 0.5, 0.5, "ok"),
			},
		},
		chairmanOut: "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 1, 0.1, rec)

	var panel *DelphiPanel
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				panel = d.Metadata.DelphiPanel
			}
		}
	})
	if panel == nil || len(panel.Rounds) != 1 {
		t.Fatalf("panel state unexpected: %+v", panel)
	}
	// m-a should still be present with completeness defaulted to 0.0.
	var aRating *DelphiRating
	for i := range panel.Rounds[0].Ratings {
		if panel.Rounds[0].Ratings[i].Model == "m-a" {
			aRating = &panel.Rounds[0].Ratings[i]
		}
	}
	if aRating == nil {
		t.Fatal("m-a rating missing — should have stayed in")
	}
	if aRating.Scores["completeness"] != 0.0 {
		t.Errorf("missing 'completeness' should default to 0.0, got %v", aRating.Scores["completeness"])
	}
}

// ── 7. Score clamping ────────────────────────────────────────────────────

func TestRunDelphi_ScoreClamping(t *testing.T) {
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": `{"ratings":{"correctness":1.5,"clarity":-0.3,"completeness":0.5},"summary":"out of range"}`,
				"m-b": ratingJSON(0.5, 0.5, 0.5, "ok"),
				"m-c": ratingJSON(0.5, 0.5, 0.5, "ok"),
			},
		},
		chairmanOut: "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 1, 0.1, rec)

	var panel *DelphiPanel
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				panel = d.Metadata.DelphiPanel
			}
		}
	})
	if panel == nil {
		t.Fatal("panel nil")
	}
	for _, r := range panel.Rounds[0].Ratings {
		if r.Model == "m-a" {
			if r.Scores["correctness"] != 1.0 {
				t.Errorf("correctness=1.5 should clamp to 1.0, got %v", r.Scores["correctness"])
			}
			if r.Scores["clarity"] != 0.0 {
				t.Errorf("clarity=-0.3 should clamp to 0.0, got %v", r.Scores["clarity"])
			}
		}
	}
}

// ── 8. Anonymisation (no model names in rater prompts) ───────────────────

func TestRunDelphi_AnonymisationInPrompts(t *testing.T) {
	rec := &delphiRecorder{
		stage1: map[string]string{
			"my-secret-model-a": "ans a",
			"my-secret-model-b": "ans b",
			"my-secret-model-c": "ans c",
		},
		roundDefaults: ratingJSON(0.5, 0.5, 0.5, "ok"),
		chairmanOut:   "ok",
	}
	c := delphiCouncil(t,
		[]string{"my-secret-model-a", "my-secret-model-b", "my-secret-model-c"},
		"chairman-z", 1, 0.1, rec)

	if err := c.RunFull(context.Background(), "q", "test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, prompt := range rec.ratingPrompts {
		for _, secret := range []string{"my-secret-model-a", "my-secret-model-b", "my-secret-model-c"} {
			if strings.Contains(prompt, secret) {
				t.Errorf("rater prompt leaks model name %q:\n%s", secret, prompt)
			}
		}
		// At least one Response label should appear.
		hasAny := false
		for _, lbl := range []string{"Response A", "Response B", "Response C"} {
			if strings.Contains(prompt, lbl) {
				hasAny = true
				break
			}
		}
		if !hasAny {
			t.Errorf("rater prompt has no anonymous labels:\n%s", prompt)
		}
	}
}

// ── 9. Self-prev injection only on round 2+ ──────────────────────────────

func TestRunDelphi_SelfPrevInjection(t *testing.T) {
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": `{"ratings":{"correctness":0.7,"clarity":0.7,"completeness":0.7},"summary":"UNIQUE-R1-SUMMARY-A"}`,
				"m-b": ratingJSON(0.5, 0.5, 0.5, "ok"),
				"m-c": ratingJSON(0.5, 0.5, 0.5, "ok"),
			},
		},
		roundDefaults: ratingJSON(0.5, 0.5, 0.5, "ok"),
		chairmanOut:   "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 2, 0.001, rec) // tight threshold to force round 2

	if err := c.RunFull(context.Background(), "q", "test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Round 1 prompts MUST NOT contain a self-prev section ("Your own previous-round").
	for _, p := range rec.ratingPromptsByRound[1] {
		if strings.Contains(p, "Your own previous-round ratings") {
			t.Errorf("round 1 prompt MUST NOT contain self-prev section:\n%s", p)
		}
	}
	// Round 2 prompt for m-a MUST contain its own UNIQUE round-1 summary.
	r2pa, ok := rec.ratingPromptByRoundModel[2]["m-a"]
	if !ok {
		t.Fatal("no round-2 prompt captured for m-a")
	}
	if !strings.Contains(r2pa, "Your own previous-round ratings") {
		t.Error("round-2 m-a prompt missing self-prev section")
	}
	if !strings.Contains(r2pa, "UNIQUE-R1-SUMMARY-A") {
		t.Error("round-2 m-a prompt missing its own round-1 summary")
	}
	// Round 2 prompt for m-b MUST NOT contain m-a's UNIQUE summary (no cross-rater leakage).
	r2pb, ok := rec.ratingPromptByRoundModel[2]["m-b"]
	if !ok {
		t.Fatal("no round-2 prompt captured for m-b")
	}
	if strings.Contains(r2pb, "UNIQUE-R1-SUMMARY-A") {
		t.Error("round-2 m-b prompt leaks m-a's round-1 summary (cross-rater leakage forbidden)")
	}
}

// ── 10. Other raters' raw ratings NOT exposed (aggregate-only feedback) ──

func TestRunDelphi_AggregateOnlyFeedback(t *testing.T) {
	// Each rater scripts a UNIQUE rating pattern. Round 2 prompts must NOT
	// contain other raters' unique fingerprints — only the aggregate stats.
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": `{"ratings":{"correctness":0.987,"clarity":0.987,"completeness":0.987},"summary":"r1"}`,
				"m-b": `{"ratings":{"correctness":0.012,"clarity":0.012,"completeness":0.012},"summary":"r1"}`,
				"m-c": `{"ratings":{"correctness":0.456,"clarity":0.456,"completeness":0.456},"summary":"r1"}`,
			},
		},
		roundDefaults: ratingJSON(0.5, 0.5, 0.5, "ok"),
		chairmanOut:   "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 2, 0.001, rec)

	if err := c.RunFull(context.Background(), "q", "test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Round 2 prompts must NOT contain the unique rating fingerprints from round 1.
	// Each rater's prompt should only include the aggregate mean (~0.485 = (0.987+0.012+0.456)/3)
	// and the rater's OWN previous ratings — not other raters' raw values.
	for model, prompt := range rec.ratingPromptByRoundModel[2] {
		// The prompt does include the rater's own previous ratings, so we
		// only check OTHER raters' fingerprints.
		otherFingerprints := map[string][]string{
			"m-a": {"0.012", "0.456"},
			"m-b": {"0.987", "0.456"},
			"m-c": {"0.987", "0.012"},
		}
		for _, fp := range otherFingerprints[model] {
			if strings.Contains(prompt, fp) {
				t.Errorf("rater %s round-2 prompt leaks other rater's raw rating %q:\n%s", model, fp, prompt)
			}
		}
	}
}

// ── 11. Stage 2 kind on every event ──────────────────────────────────────

func TestRunDelphi_StageTwoKind(t *testing.T) {
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundDefaults: ratingJSON(0.5, 0.5, 0.5, "ok"),
		chairmanOut:   "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 2, 0.001, rec)

	var roundEventKinds []string
	var terminalKind string
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_round_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				roundEventKinds = append(roundEventKinds, d.Kind)
			}
		}
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				terminalKind = d.Kind
			}
		}
	})
	for _, k := range roundEventKinds {
		if k != "delphi_round" {
			t.Errorf("round event kind: got %q, want %q", k, "delphi_round")
		}
	}
	if terminalKind != "delphi_round" {
		t.Errorf("terminal kind: got %q, want %q", terminalKind, "delphi_round")
	}
}

// ── 12. Sort stability ───────────────────────────────────────────────────

func TestRunDelphi_RatingSortStability(t *testing.T) {
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		roundDefaults: ratingJSON(0.5, 0.5, 0.5, "ok"),
		chairmanOut:   "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 1, 0.1, rec)

	var panel *DelphiPanel
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				panel = d.Metadata.DelphiPanel
			}
		}
	})
	if panel == nil || len(panel.Rounds) != 1 {
		t.Fatalf("panel state unexpected: %+v", panel)
	}
	rs := panel.Rounds[0].Ratings
	for i := 1; i < len(rs); i++ {
		if rs[i-1].Label > rs[i].Label {
			t.Errorf("ratings not sorted ascending by Label: %v", labelsOfRatings(rs))
			break
		}
	}
}

// ── 13. Transcript-agreement contract ────────────────────────────────────

func TestRunDelphi_TranscriptAgreement(t *testing.T) {
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		roundDefaults: ratingJSON(0.5, 0.5, 0.5, "ok"),
		chairmanOut:   "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 2, 0.001, rec)

	var cumulative []DelphiRound
	var canonical []DelphiRound
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_round_complete" {
			if d, ok := data.(Stage2CompleteData); ok && d.Metadata.DelphiPanel != nil {
				cumulative = append(cumulative, d.Metadata.DelphiPanel.Rounds...)
			}
		}
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok && d.Metadata.DelphiPanel != nil {
				canonical = d.Metadata.DelphiPanel.Rounds
			}
		}
	})
	if len(cumulative) == 0 || len(canonical) == 0 {
		t.Fatalf("missing rounds: cumulative=%d, canonical=%d", len(cumulative), len(canonical))
	}
	if !reflect.DeepEqual(cumulative, canonical) {
		t.Errorf("cumulative round events disagree with canonical Metadata.DelphiPanel.Rounds:\ncumulative=%+v\ncanonical=%+v", cumulative, canonical)
	}
}

// ── 14. Per-criterion convergence asymmetry ──────────────────────────────

func TestRunDelphi_PerCriterionConvergenceAsymmetry(t *testing.T) {
	// At round 2: correctness Δ ≈ 0.05 (below threshold 0.1),
	//             clarity Δ ≈ 0.15 (above threshold).
	// Strategy MUST NOT exit early at round 2 — max(Δ) = 0.15 > 0.1.
	// Round 3: both Δs fall below threshold → exits with Converged: true.
	rec := &delphiRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": ratingJSON(0.5, 0.5, 0.5, "r1"),
				"m-b": ratingJSON(0.5, 0.5, 0.5, "r1"),
				"m-c": ratingJSON(0.5, 0.5, 0.5, "r1"),
			},
			2: {
				// correctness mean shifts from 0.5 → 0.55 (Δ=0.05, below).
				// clarity mean shifts from 0.5 → 0.65 (Δ=0.15, above).
				// completeness shifts from 0.5 → 0.55 (Δ=0.05, below).
				"m-a": ratingJSON(0.55, 0.65, 0.55, "r2"),
				"m-b": ratingJSON(0.55, 0.65, 0.55, "r2"),
				"m-c": ratingJSON(0.55, 0.65, 0.55, "r2"),
			},
			3: {
				// All shift by 0.02 from round 2 → all below threshold.
				"m-a": ratingJSON(0.57, 0.67, 0.57, "r3"),
				"m-b": ratingJSON(0.57, 0.67, 0.57, "r3"),
				"m-c": ratingJSON(0.57, 0.67, 0.57, "r3"),
			},
		},
		chairmanOut: "ok",
	}
	c := delphiCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 5, 0.1, rec)

	var roundCount int
	var panel *DelphiPanel
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_round_complete" {
			roundCount++
		}
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				panel = d.Metadata.DelphiPanel
			}
		}
	})
	if roundCount != 3 {
		t.Errorf("round events: got %d, want 3 (round 2 must NOT trigger early exit because clarity Δ > threshold)", roundCount)
	}
	if panel == nil || panel.FinalRound != 3 || !panel.Converged {
		t.Errorf("expected FinalRound=3 Converged=true, got %+v", panel)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────

func labelsOfRatings(rs []DelphiRating) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Label
	}
	return out
}
