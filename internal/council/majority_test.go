package council

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// majorityFixture builds a Rada whose registry's "test" entry uses the
// Majority strategy with the given chairman (empty for the no-chairman path)
// and a 4-model pool. The mock client routes by request-shape: prompt prefix
// classifies the call as Stage 1 vs polish vs tiebreak.
type majorityRecorder struct {
	mu        sync.Mutex
	stage1    map[string]string // model → answer the mock should return
	polishOut string            // chairman's response on polish calls
	tieOut    string            // chairman's response on tiebreak calls
	calls     []string          // labels: "stage1:<model>", "polish:<model>", "tiebreak:<model>"
	chairmanErr error            // if set, chairman calls return this error
}

func (r *majorityRecorder) record(req CompletionRequest) (CompletionResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	body := ""
	if len(req.Messages) > 0 {
		body = req.Messages[0].Content
	}
	switch {
	case strings.Contains(body, "You polish the council's winning answer"):
		r.calls = append(r.calls, "polish:"+req.Model)
		if r.chairmanErr != nil {
			return CompletionResponse{}, r.chairmanErr
		}
		return makeResponse(r.polishOut), nil
	case strings.Contains(body, "You arbitrate a tie"):
		r.calls = append(r.calls, "tiebreak:"+req.Model)
		if r.chairmanErr != nil {
			return CompletionResponse{}, r.chairmanErr
		}
		return makeResponse(r.tieOut), nil
	default:
		// Stage 1 — the prompt is the council's deliberation prompt.
		r.calls = append(r.calls, "stage1:"+req.Model)
		ans, ok := r.stage1[req.Model]
		if !ok {
			return CompletionResponse{}, errors.New("no answer scripted for " + req.Model)
		}
		return makeResponse(ans), nil
	}
}

func majorityCouncil(t *testing.T, models []string, chairman string, rec *majorityRecorder) *Rada {
	t.Helper()
	registry := map[string]CouncilType{
		"test": {
			Name:          "test",
			Strategy:      Majority,
			Models:        models,
			ChairmanModel: chairman,
			Temperature:   0.7,
		},
	}
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			return rec.record(req)
		},
	}
	return NewCouncil(client, registry, nil)
}

func TestRunMajority_Unanimous(t *testing.T) {
	rec := &majorityRecorder{stage1: map[string]string{
		"m-a": "42",
		"m-b": "42",
		"m-c": "42",
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c"}, "", rec)

	var stage3 StageThreeResult
	var tally *VoteTally
	err := c.RunFull(context.Background(), "what is the answer?", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				tally = d.Metadata.VoteTally
			}
		}
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tally == nil || len(tally.Clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %v", tally)
	}
	if tally.Clusters[0].Votes != 3 {
		t.Errorf("winner votes: got %d, want 3", tally.Clusters[0].Votes)
	}
	if stage3.Content != "42" {
		t.Errorf("stage3 content: got %q, want %q", stage3.Content, "42")
	}
	if stage3.Model != "" {
		t.Errorf("stage3 model: got %q, want empty (no chairman, no LLM call)", stage3.Model)
	}
}

func TestRunMajority_Plurality_NoChairman(t *testing.T) {
	rec := &majorityRecorder{stage1: map[string]string{
		"m-a": "yes",
		"m-b": "yes",
		"m-c": "no",
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c"}, "", rec)

	var stage3 StageThreeResult
	err := c.RunFull(context.Background(), "is it true?", "test", func(eventType string, data any) {
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stage3.Content != "yes" {
		t.Errorf("stage3 content: got %q, want %q", stage3.Content, "yes")
	}
	if stage3.Model != "" || stage3.DurationMs != 0 {
		t.Errorf("stage3 (no LLM): got Model=%q DurationMs=%d, want empty/0", stage3.Model, stage3.DurationMs)
	}
	for _, call := range rec.calls {
		if strings.HasPrefix(call, "polish:") || strings.HasPrefix(call, "tiebreak:") {
			t.Errorf("unexpected chairman call %q in no-chairman run", call)
		}
	}
}

func TestRunMajority_Plurality_ChairmanPolish(t *testing.T) {
	rec := &majorityRecorder{
		stage1: map[string]string{
			"m-a": "yes",
			"m-b": "yes",
			"m-c": "no",
		},
		polishOut: "Yes — confirmed.",
	}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", rec)

	var stage3 StageThreeResult
	err := c.RunFull(context.Background(), "is it true?", "test", func(eventType string, data any) {
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stage3.Content != "Yes — confirmed." {
		t.Errorf("stage3 content: got %q, want polished output", stage3.Content)
	}
	if stage3.Model != "chairman-z" {
		t.Errorf("stage3 model: got %q, want %q", stage3.Model, "chairman-z")
	}
	polishCount := 0
	for _, call := range rec.calls {
		if strings.HasPrefix(call, "polish:") {
			polishCount++
		}
		if strings.HasPrefix(call, "tiebreak:") {
			t.Errorf("unexpected tiebreak call in plurality run: %q", call)
		}
	}
	if polishCount != 1 {
		t.Errorf("polish calls: got %d, want 1", polishCount)
	}
}

func TestRunMajority_Tie_ChairmanTiebreak(t *testing.T) {
	// 2-2 split between "yes" and "no".
	rec := &majorityRecorder{
		stage1: map[string]string{
			"m-a": "yes",
			"m-b": "yes",
			"m-c": "no",
			"m-d": "no",
		},
		tieOut: "Yes — chairman picks the affirmative.",
	}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", rec)

	var stage3 StageThreeResult
	err := c.RunFull(context.Background(), "is it true?", "test", func(eventType string, data any) {
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stage3.Content, "chairman picks") {
		t.Errorf("stage3 content: got %q, want chairman tiebreak output", stage3.Content)
	}
	tiebreakCount := 0
	for _, call := range rec.calls {
		if strings.HasPrefix(call, "tiebreak:") {
			tiebreakCount++
		}
		if strings.HasPrefix(call, "polish:") {
			t.Errorf("unexpected polish call in tiebreak run: %q", call)
		}
	}
	if tiebreakCount != 1 {
		t.Errorf("tiebreak calls: got %d, want 1", tiebreakCount)
	}
}

func TestRunMajority_Tie_NoChairman_LoudError(t *testing.T) {
	rec := &majorityRecorder{stage1: map[string]string{
		"m-a": "yes",
		"m-b": "yes",
		"m-c": "no",
		"m-d": "no",
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "" /* no chairman */, rec)

	var stage3Emitted bool
	err := c.RunFull(context.Background(), "is it true?", "test", func(eventType string, _ any) {
		if eventType == "stage3_complete" {
			stage3Emitted = true
		}
	})
	if err == nil {
		t.Fatal("expected error on tie with no chairman")
	}
	if !strings.Contains(err.Error(), "tie") {
		t.Errorf("error message: got %q, want it to mention 'tie'", err.Error())
	}
	if stage3Emitted {
		t.Error("must NOT emit stage3_complete on the tie-no-chairman error path")
	}
}

func TestRunMajority_QuorumFailure(t *testing.T) {
	// All models error → quorum can't be met. With QuorumMin=0, the formula
	// is max(3, ⌈N/2⌉+1); N=4 gives need=3, so 0 successful answers fails it.
	rec := &majorityRecorder{stage1: map[string]string{
		// no scripted answers — every Stage 1 call returns "no answer scripted"
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "", rec)

	err := c.RunFull(context.Background(), "q", "test", nil)
	if err == nil {
		t.Fatal("expected QuorumError when all Stage 1 calls fail")
	}
	var qe *QuorumError
	if !errors.As(err, &qe) {
		t.Errorf("error type: got %T, want *QuorumError", err)
	}
}

func TestRunMajority_NormalisationCollapses(t *testing.T) {
	// Three answers that differ only in case + whitespace must collapse to
	// one cluster.
	rec := &majorityRecorder{stage1: map[string]string{
		"m-a": "42",
		"m-b": " 42 ",
		"m-c": "42\n",
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c"}, "", rec)

	var tally *VoteTally
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				tally = d.Metadata.VoteTally
			}
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tally == nil || len(tally.Clusters) != 1 {
		t.Fatalf("expected 1 cluster after normalisation, got %v", tally)
	}
	if tally.Clusters[0].Votes != 3 {
		t.Errorf("votes: got %d, want 3", tally.Clusters[0].Votes)
	}
}

func TestRunMajority_Stage2Kind_IsVoteTally(t *testing.T) {
	rec := &majorityRecorder{stage1: map[string]string{
		"m-a": "42",
		"m-b": "42",
		"m-c": "42",
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c"}, "", rec)

	var seenKind string
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				seenKind = d.Kind
			}
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenKind != "vote_tally" {
		t.Errorf("Stage 2 Kind: got %q, want %q", seenKind, "vote_tally")
	}
}

func TestRunMajority_ConsensusW_IsRatio(t *testing.T) {
	// 2-of-3 plurality → consensus_w should be 2/3 ≈ 0.667, NOT 1.0.
	rec := &majorityRecorder{stage1: map[string]string{
		"m-a": "yes",
		"m-b": "yes",
		"m-c": "no",
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c"}, "", rec)

	var consensusW float64
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				consensusW = d.Metadata.ConsensusW
			}
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := 2.0 / 3.0
	if consensusW < want-0.01 || consensusW > want+0.01 {
		t.Errorf("ConsensusW: got %.3f, want ~%.3f (winnerVotes/totalVotes for 2-of-3 plurality)", consensusW, want)
	}
}

func TestRunMajority_ConsensusW_UnanimousIsOne(t *testing.T) {
	// Unanimous (all 3 agree) → consensus_w should be 1.0.
	rec := &majorityRecorder{stage1: map[string]string{
		"m-a": "yes", "m-b": "yes", "m-c": "yes",
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c"}, "", rec)

	var consensusW float64
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				consensusW = d.Metadata.ConsensusW
			}
		}
	})
	if consensusW != 1.0 {
		t.Errorf("ConsensusW: got %.3f, want 1.0 (unanimous)", consensusW)
	}
}

func TestRunMajority_NoLLMStage3_Contract(t *testing.T) {
	// Plurality, no chairman, no tie → Model="" and DurationMs=0.
	// This is the documented "no LLM call produced this result" contract.
	rec := &majorityRecorder{stage1: map[string]string{
		"m-a": "yes",
		"m-b": "yes",
		"m-c": "no",
	}}
	c := majorityCouncil(t, []string{"m-a", "m-b", "m-c"}, "", rec)

	var stage3 StageThreeResult
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stage3.Model != "" {
		t.Errorf("Model: got %q, want empty (no LLM call)", stage3.Model)
	}
	if stage3.DurationMs != 0 {
		t.Errorf("DurationMs: got %d, want 0 (no LLM call)", stage3.DurationMs)
	}
	if stage3.Content != "yes" {
		t.Errorf("Content: got %q, want %q", stage3.Content, "yes")
	}
}
