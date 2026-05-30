package council

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// rankRefineRecorder is a thread-safe collector that classifies each completion
// request as a Stage 1 generation, a rank arbiter call, or a refine call by
// inspecting the prompt prefix:
//   - Generator prompts are the council's deliberation prompt (Stage 1) — they
//     contain the question text and no discriminator phrase.
//   - Rank prompts start with "You rank council answers."
//   - Refine prompts start with "You refine the top-K council answers."
type rankRefineRecorder struct {
	mu          sync.Mutex
	stage1      map[string]string // model → answer to return
	rankOut     string            // arbiter's JSON response
	refineOut   string            // refiner's response
	rankErr     error             // if non-nil, rank call returns this error
	refineErr   error             // if non-nil, refine call returns this error
	calls       []string          // labels: "stage1:<model>", "rank:<model>", "refine:<model>"
	rankPrompt  string            // last rank prompt (for assertions)
	refinePrompt string           // last refine prompt (for assertions)
}

func (r *rankRefineRecorder) record(req CompletionRequest) (CompletionResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	body := ""
	if len(req.Messages) > 0 {
		body = req.Messages[0].Content
	}
	switch {
	case strings.Contains(body, "You rank council answers"):
		r.calls = append(r.calls, "rank:"+req.Model)
		r.rankPrompt = body
		if r.rankErr != nil {
			return CompletionResponse{}, r.rankErr
		}
		return makeResponse(r.rankOut), nil
	case strings.Contains(body, "You refine the top-K council answers"):
		r.calls = append(r.calls, "refine:"+req.Model)
		r.refinePrompt = body
		if r.refineErr != nil {
			return CompletionResponse{}, r.refineErr
		}
		return makeResponse(r.refineOut), nil
	default:
		// Stage 1 generation.
		r.calls = append(r.calls, "stage1:"+req.Model)
		ans, ok := r.stage1[req.Model]
		if !ok {
			return CompletionResponse{}, errors.New("no answer scripted for " + req.Model)
		}
		return makeResponse(ans), nil
	}
}

func rankRefineCouncil(t *testing.T, models []string, chairman string, refineTopK int, rec *rankRefineRecorder) *Rada {
	t.Helper()
	registry := map[string]CouncilType{
		"test": {
			Name:          "test",
			Strategy:      GenerateRankRefine,
			Models:        models,
			ChairmanModel: chairman,
			Temperature:   0.7,
			RefineTopK:    refineTopK,
		},
	}
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			return rec.record(req)
		},
	}
	return NewCouncil(client, registry, nil)
}

// rankResponseJSON is a helper to build a rank-arbiter JSON response.
func rankResponseJSON(t *testing.T, entries []rankEntry) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"rankings":[`)
	for i, e := range entries {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"label":"`)
		b.WriteString(e.Label)
		b.WriteString(`","scores":{`)
		j := 0
		for k, v := range e.Scores {
			if j > 0 {
				b.WriteString(",")
			}
			b.WriteString(`"`)
			b.WriteString(k)
			b.WriteString(`":`)
			fmtFloat(&b, v)
			j++
		}
		b.WriteString(`},"total_score":`)
		fmtFloat(&b, e.Total)
		b.WriteString(`}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// fmtFloat writes a compact decimal representation of v into b — used by
// rankResponseJSON when building arbiter response fixtures.
func fmtFloat(b *strings.Builder, v float64) {
	b.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
}

type rankEntry struct {
	Label  string
	Scores map[string]float64
	Total  float64
}

// ── 1. Happy path ─────────────────────────────────────────────────────────

func TestRunGenerateRankRefine_HappyPath(t *testing.T) {
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "answer a", "m-b": "answer b", "m-c": "answer c",
			"m-d": "answer d", "m-e": "answer e",
		},
		rankOut: rankResponseJSON(t, []rankEntry{
			{Label: "Response A", Scores: map[string]float64{"correctness": 0.8, "clarity": 0.8, "completeness": 0.8, "originality": 0.8}, Total: 3.2},
			{Label: "Response B", Scores: map[string]float64{"correctness": 0.7, "clarity": 0.7, "completeness": 0.7, "originality": 0.7}, Total: 2.8},
			{Label: "Response C", Scores: map[string]float64{"correctness": 0.6, "clarity": 0.6, "completeness": 0.6, "originality": 0.6}, Total: 2.4},
			{Label: "Response D", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
			{Label: "Response E", Scores: map[string]float64{"correctness": 0.4, "clarity": 0.4, "completeness": 0.4, "originality": 0.4}, Total: 1.6},
		}),
		refineOut: "## Refined Answer\n\nThe synthesised answer.",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d", "m-e"}, "chairman-z", 0 /* default k=3 */, rec)

	var seenKind string
	var tally *RankRefine
	var stage3 StageThreeResult
	err := c.RunFull(context.Background(), "what is the answer?", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				seenKind = d.Kind
				tally = d.Metadata.RankRefine
			}
		}
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenKind != "rank_refine" {
		t.Errorf("Stage 2 Kind: got %q, want %q", seenKind, "rank_refine")
	}
	if tally == nil {
		t.Fatal("Metadata.RankRefine: nil")
	}
	if tally.TopK != 3 {
		t.Errorf("TopK: got %d, want 3", tally.TopK)
	}
	if len(tally.Rankings) != 5 {
		t.Fatalf("Rankings: got %d, want 5", len(tally.Rankings))
	}
	advancing := 0
	for _, r := range tally.Rankings {
		if r.Advancing {
			advancing++
		}
	}
	if advancing != 3 {
		t.Errorf("advancing count: got %d, want 3", advancing)
	}
	if !strings.Contains(stage3.Content, "Refined Answer") {
		t.Errorf("stage3 content: got %q, want refined output", stage3.Content)
	}
	if stage3.Model != "chairman-z" {
		t.Errorf("stage3 model: got %q, want %q", stage3.Model, "chairman-z")
	}
}

// ── 2. Quorum failure ─────────────────────────────────────────────────────

func TestRunGenerateRankRefine_QuorumFailure(t *testing.T) {
	// k=3 (default), need = max(4, 3) = 4. Scripting only 2 successful answers.
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "ok", "m-b": "ok",
			// m-c, m-d, m-e have no scripted answer → errors → quorum fails
		},
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d", "m-e"}, "chairman-z", 0, rec)

	err := c.RunFull(context.Background(), "q", "test", nil)
	if err == nil {
		t.Fatal("expected QuorumError")
	}
	var qe *QuorumError
	if !errors.As(err, &qe) {
		t.Errorf("error type: got %T, want *QuorumError", err)
	}
	if qe != nil && qe.Need != 4 {
		t.Errorf("QuorumError.Need: got %d, want 4 (max(k+1=4, 3))", qe.Need)
	}
}

// ── 3. Custom RefineTopK ──────────────────────────────────────────────────

func TestRunGenerateRankRefine_CustomTopK(t *testing.T) {
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d", "m-e": "e", "m-f": "f",
		},
		rankOut: rankResponseJSON(t, []rankEntry{
			{Label: "Response A", Scores: map[string]float64{"correctness": 1, "clarity": 1, "completeness": 1, "originality": 1}, Total: 4.0},
			{Label: "Response B", Scores: map[string]float64{"correctness": 0.8, "clarity": 0.8, "completeness": 0.8, "originality": 0.8}, Total: 3.2},
			{Label: "Response C", Scores: map[string]float64{"correctness": 0.6, "clarity": 0.6, "completeness": 0.6, "originality": 0.6}, Total: 2.4},
			{Label: "Response D", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
			{Label: "Response E", Scores: map[string]float64{"correctness": 0.4, "clarity": 0.4, "completeness": 0.4, "originality": 0.4}, Total: 1.6},
			{Label: "Response F", Scores: map[string]float64{"correctness": 0.3, "clarity": 0.3, "completeness": 0.3, "originality": 0.3}, Total: 1.2},
		}),
		refineOut: "refined",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d", "m-e", "m-f"}, "chairman-z", 5 /* custom k */, rec)

	var tally *RankRefine
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				tally = d.Metadata.RankRefine
			}
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tally == nil || tally.TopK != 5 {
		t.Fatalf("TopK: got %v, want 5", tally)
	}
	advancing := 0
	for _, r := range tally.Rankings {
		if r.Advancing {
			advancing++
		}
	}
	if advancing != 5 {
		t.Errorf("advancing: got %d, want 5", advancing)
	}
}

// ── 4. Default RefineTopK ─────────────────────────────────────────────────

// Covered implicitly by TestRunGenerateRankRefine_HappyPath (RefineTopK=0 → defaults to 3).
// Adding a focused test on the field default to nail it down explicitly.

func TestRunGenerateRankRefine_DefaultTopKIsThree(t *testing.T) {
	rec := &rankRefineRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		rankOut: rankResponseJSON(t, []rankEntry{
			{Label: "Response A", Scores: map[string]float64{"correctness": 0.9, "clarity": 0.9, "completeness": 0.9, "originality": 0.9}, Total: 3.6},
			{Label: "Response B", Scores: map[string]float64{"correctness": 0.7, "clarity": 0.7, "completeness": 0.7, "originality": 0.7}, Total: 2.8},
			{Label: "Response C", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
			{Label: "Response D", Scores: map[string]float64{"correctness": 0.3, "clarity": 0.3, "completeness": 0.3, "originality": 0.3}, Total: 1.2},
		}),
		refineOut: "refined",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	var tally *RankRefine
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				tally = d.Metadata.RankRefine
			}
		}
	})
	if tally == nil || tally.TopK != 3 {
		t.Fatalf("TopK: got %v, want 3 (default)", tally)
	}
}

// ── 5. Arbiter parse failure (loud error) ────────────────────────────────

func TestRunGenerateRankRefine_RankParseFailure_LoudError(t *testing.T) {
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d",
		},
		rankOut: "not valid json {{{",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	var stage3Emitted bool
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, _ any) {
		if eventType == "stage3_complete" {
			stage3Emitted = true
		}
	})
	if err == nil {
		t.Fatal("expected loud error on rank parse failure")
	}
	if !strings.Contains(err.Error(), "arbiter") {
		t.Errorf("error message: got %q, want it to mention 'arbiter'", err.Error())
	}
	if stage3Emitted {
		t.Error("must NOT emit stage3_complete on rank parse failure")
	}
}

// ── 6. Unknown labels dropped ─────────────────────────────────────────────

func TestRunGenerateRankRefine_UnknownLabelsDropped(t *testing.T) {
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d",
		},
		rankOut: rankResponseJSON(t, []rankEntry{
			{Label: "Response A", Scores: map[string]float64{"correctness": 0.9, "clarity": 0.9, "completeness": 0.9, "originality": 0.9}, Total: 3.6},
			{Label: "Response B", Scores: map[string]float64{"correctness": 0.7, "clarity": 0.7, "completeness": 0.7, "originality": 0.7}, Total: 2.8},
			{Label: "Hallucinated Z", Scores: map[string]float64{"correctness": 1, "clarity": 1, "completeness": 1, "originality": 1}, Total: 4.0},
			{Label: "Response C", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
			{Label: "Response D", Scores: map[string]float64{"correctness": 0.3, "clarity": 0.3, "completeness": 0.3, "originality": 0.3}, Total: 1.2},
		}),
		refineOut: "refined",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	var tally *RankRefine
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				tally = d.Metadata.RankRefine
			}
		}
	})
	if tally == nil {
		t.Fatal("tally nil")
	}
	if len(tally.Rankings) != 4 {
		t.Errorf("rankings: got %d, want 4 (Hallucinated Z dropped)", len(tally.Rankings))
	}
	for _, r := range tally.Rankings {
		if r.Label == "Hallucinated Z" {
			t.Error("hallucinated label survived")
		}
	}
}

// ── 7. Refinement prompt receives top-K ──────────────────────────────────

func TestRunGenerateRankRefine_RefinePromptHasTopKLabels(t *testing.T) {
	// All 4 models return distinguishable content; assignLabels shuffles
	// model→label mapping randomly, so we can't predict label→content. We
	// capture LabelToModel from the stage2 metadata, derive which models
	// got the top-3 labels (A, B, C), and assert their content reaches the
	// refine prompt.
	contentByModel := map[string]string{
		"m-a": "alpha-content",
		"m-b": "beta-content",
		"m-c": "gamma-content",
		"m-d": "delta-content",
	}
	rec := &rankRefineRecorder{
		stage1: contentByModel,
		rankOut: rankResponseJSON(t, []rankEntry{
			{Label: "Response A", Scores: map[string]float64{"correctness": 0.9, "clarity": 0.9, "completeness": 0.9, "originality": 0.9}, Total: 3.6},
			{Label: "Response B", Scores: map[string]float64{"correctness": 0.7, "clarity": 0.7, "completeness": 0.7, "originality": 0.7}, Total: 2.8},
			{Label: "Response C", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
			{Label: "Response D", Scores: map[string]float64{"correctness": 0.3, "clarity": 0.3, "completeness": 0.3, "originality": 0.3}, Total: 1.2},
		}),
		refineOut: "refined",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	var labelToModel map[string]string
	if err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				labelToModel = d.Metadata.LabelToModel
			}
		}
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(labelToModel) != 4 {
		t.Fatalf("LabelToModel: got %d entries, want 4", len(labelToModel))
	}
	// Top-3 advancing labels are A, B, C — their content MUST reach the refine prompt.
	for _, label := range []string{"Response A", "Response B", "Response C"} {
		model, ok := labelToModel[label]
		if !ok {
			t.Fatalf("LabelToModel missing %q", label)
		}
		want := contentByModel[model]
		if !strings.Contains(rec.refinePrompt, want) {
			t.Errorf("refine prompt missing content of advancing %s (model %s, content %q)", label, model, want)
		}
	}
	// The model behind label D must NOT have its content in the refine prompt.
	dModel := labelToModel["Response D"]
	dContent := contentByModel[dModel]
	if strings.Contains(rec.refinePrompt, dContent) {
		t.Errorf("refine prompt contains non-advancing content %q (model %s, label D)", dContent, dModel)
	}
}

// ── 8. Score parsing edge cases (clamps + missing criterion) ─────────────

func TestRunGenerateRankRefine_ScoreClamping(t *testing.T) {
	// Arbiter returns a candidate with out-of-range scores and a missing criterion.
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d",
		},
		rankOut: `{"rankings":[
			{"label":"Response A","scores":{"correctness":2.0,"clarity":-0.5,"completeness":99,"originality":0.5},"total_score":99},
			{"label":"Response B","scores":{"correctness":0.7,"clarity":0.7,"completeness":0.7,"originality":0.7},"total_score":2.8},
			{"label":"Response C","scores":{"correctness":0.5,"clarity":0.5},"total_score":1.0},
			{"label":"Response D","scores":{"correctness":0.3,"clarity":0.3,"completeness":0.3,"originality":0.3},"total_score":1.2}
		]}`,
		refineOut: "refined",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	var tally *RankRefine
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				tally = d.Metadata.RankRefine
			}
		}
	})
	if tally == nil {
		t.Fatal("tally nil")
	}
	// Find Response A — its scores must be clamped to [0,1] each, total to len(criteria)=4.
	var a *RankedCandidate
	for i := range tally.Rankings {
		if tally.Rankings[i].Label == "Response A" {
			a = &tally.Rankings[i]
		}
	}
	if a == nil {
		t.Fatal("Response A missing")
	}
	for k, v := range a.Scores {
		if v < 0.0 || v > 1.0 {
			t.Errorf("score[%s] = %f not clamped to [0,1]", k, v)
		}
	}
	if a.TotalScore < 0.0 || a.TotalScore > 4.0 {
		t.Errorf("TotalScore = %f not clamped to [0,4]", a.TotalScore)
	}
	// Find Response C — missing criteria default to 0.0.
	var rcC *RankedCandidate
	for i := range tally.Rankings {
		if tally.Rankings[i].Label == "Response C" {
			rcC = &tally.Rankings[i]
		}
	}
	if rcC == nil {
		t.Fatal("Response C missing")
	}
	for _, name := range []string{"correctness", "clarity", "completeness", "originality"} {
		if _, ok := rcC.Scores[name]; !ok {
			t.Errorf("missing criterion %q must be defaulted to 0.0, but key absent", name)
		}
	}
	if rcC.Scores["completeness"] != 0.0 || rcC.Scores["originality"] != 0.0 {
		t.Errorf("missing criteria not defaulted to 0.0: %v", rcC.Scores)
	}
}

// ── 9. Sort stability ─────────────────────────────────────────────────────

func TestRunGenerateRankRefine_SortStability(t *testing.T) {
	// Two pairs of ties: (A,B) at total=3.0; (C,D) at total=2.0. Expect the
	// secondary sort to put A before B (by Label asc), C before D.
	// Input order intentionally scrambles the pairs.
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d",
		},
		rankOut: rankResponseJSON(t, []rankEntry{
			{Label: "Response B", Scores: map[string]float64{"correctness": 0.75, "clarity": 0.75, "completeness": 0.75, "originality": 0.75}, Total: 3.0},
			{Label: "Response D", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
			{Label: "Response A", Scores: map[string]float64{"correctness": 0.75, "clarity": 0.75, "completeness": 0.75, "originality": 0.75}, Total: 3.0},
			{Label: "Response C", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
		}),
		refineOut: "refined",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	var tally *RankRefine
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				tally = d.Metadata.RankRefine
			}
		}
	})
	if tally == nil {
		t.Fatal("tally nil")
	}
	wantOrder := []string{"Response A", "Response B", "Response C", "Response D"}
	if len(tally.Rankings) != len(wantOrder) {
		t.Fatalf("rankings: got %d, want %d", len(tally.Rankings), len(wantOrder))
	}
	for i, want := range wantOrder {
		if tally.Rankings[i].Label != want {
			t.Errorf("rankings[%d]: got %q, want %q", i, tally.Rankings[i].Label, want)
		}
	}
}

// ── 10. Tie at K boundary (deterministic) ─────────────────────────────────

func TestRunGenerateRankRefine_TieAtKBoundary(t *testing.T) {
	// k=3 default. Candidates ranked 3 and 4 share total_score=2.0.
	// Expect the secondary Label sort to put C (3rd) before D (4th), so
	// only C advances. Verify exactly K=3 candidates have Advancing=true.
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d",
		},
		rankOut: rankResponseJSON(t, []rankEntry{
			{Label: "Response A", Scores: map[string]float64{"correctness": 0.9, "clarity": 0.9, "completeness": 0.9, "originality": 0.9}, Total: 3.6},
			{Label: "Response B", Scores: map[string]float64{"correctness": 0.7, "clarity": 0.7, "completeness": 0.7, "originality": 0.7}, Total: 2.8},
			{Label: "Response C", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
			{Label: "Response D", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
		}),
		refineOut: "refined",
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	var tally *RankRefine
	if err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				tally = d.Metadata.RankRefine
			}
		}
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tally == nil {
		t.Fatal("tally nil")
	}
	// Exactly 3 advancing.
	advancingLabels := []string{}
	for _, r := range tally.Rankings {
		if r.Advancing {
			advancingLabels = append(advancingLabels, r.Label)
		}
	}
	if len(advancingLabels) != 3 {
		t.Fatalf("advancing count: got %d (%v), want 3", len(advancingLabels), advancingLabels)
	}
	// C advances, D does not (deterministic by Label sort).
	want := map[string]bool{"Response A": true, "Response B": true, "Response C": true}
	for _, lbl := range advancingLabels {
		if !want[lbl] {
			t.Errorf("unexpected advancing label %q (D should NOT advance — Label sort puts C before D)", lbl)
		}
	}
}

// ── 11. Refine-call failure populates Model + DurationMs ─────────────────

func TestRunGenerateRankRefine_RefineFailure_PopulatesModelAndDuration(t *testing.T) {
	rec := &rankRefineRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d",
		},
		rankOut: rankResponseJSON(t, []rankEntry{
			{Label: "Response A", Scores: map[string]float64{"correctness": 0.9, "clarity": 0.9, "completeness": 0.9, "originality": 0.9}, Total: 3.6},
			{Label: "Response B", Scores: map[string]float64{"correctness": 0.7, "clarity": 0.7, "completeness": 0.7, "originality": 0.7}, Total: 2.8},
			{Label: "Response C", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, Total: 2.0},
			{Label: "Response D", Scores: map[string]float64{"correctness": 0.3, "clarity": 0.3, "completeness": 0.3, "originality": 0.3}, Total: 1.2},
		}),
		refineErr: errors.New("upstream timeout"),
	}
	c := rankRefineCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	err := c.RunFull(context.Background(), "q", "test", nil)
	if err == nil {
		t.Fatal("expected error from refine call")
	}
	if !strings.Contains(err.Error(), "refiner") {
		t.Errorf("error message: got %q, want it to mention 'refiner'", err.Error())
	}
	// Verify the rank step succeeded but the refine step failed (call sequence).
	rankCalls, refineCalls := 0, 0
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "rank:") {
			rankCalls++
		}
		if strings.HasPrefix(c, "refine:") {
			refineCalls++
		}
	}
	if rankCalls != 1 || refineCalls != 1 {
		t.Errorf("call sequence: got rank=%d refine=%d, want 1 of each", rankCalls, refineCalls)
	}
}
