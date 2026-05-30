package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/valpere/vmm-rada/internal/council"
)

// fakeLLMClient routes Complete() calls to the per-model `responses` map.
// Tests pre-populate the map with the answer they want returned for a given
// model, so the orchestrator's path through council baseline + baseline
// fallback + judge can be exercised without any real LLM calls.
type fakeLLMClient struct {
	responses map[string]string
	errs      map[string]error
	calls     []council.CompletionRequest
}

func (f *fakeLLMClient) Complete(_ context.Context, req council.CompletionRequest) (council.CompletionResponse, error) {
	f.calls = append(f.calls, req)
	if err, ok := f.errs[req.Model]; ok {
		return council.CompletionResponse{}, err
	}
	body, ok := f.responses[req.Model]
	if !ok {
		return council.CompletionResponse{}, fmt.Errorf("fakeLLMClient: no response for model %q", req.Model)
	}
	return council.CompletionResponse{
		Choices: []struct {
			Message council.ChatMessage `json:"message"`
		}{{Message: council.ChatMessage{Role: "assistant", Content: body}}},
	}, nil
}

// fakeRunner implements council.Runner. RunFull fires a fixed event sequence
// (stage2_complete with metadata, then stage3_complete with the configured
// answer) so the orchestrator's onEvent capture is exercised. errOnRun lets
// individual cases inject a council failure.
type fakeRunner struct {
	answer     string
	consensusW float64
	errOnRun   error
}

func (r *fakeRunner) RunFull(_ context.Context, _ string, _ string, onEvent council.EventFunc) error {
	if r.errOnRun != nil {
		return r.errOnRun
	}
	if onEvent != nil {
		onEvent("stage2_complete", council.Stage2CompleteData{
			Metadata: council.Metadata{ConsensusW: r.consensusW},
		})
		onEvent("stage3_complete", council.StageThreeResult{Content: r.answer})
	}
	return nil
}

// judgeReply marshals a judge response payload into the JSON shape the
// real Judge will parse.
func judgeReply(verdict, explanation string) string {
	b, _ := json.Marshal(judgeResponse{Verdict: verdict, Explanation: explanation})
	return string(b)
}

// ── Run: success paths ─────────────────────────────────────────────────────

func TestRun_CapturesCouncilAnswerAndConsensusW(t *testing.T) {
	const (
		baselineModel = "baseline-x"
		judgeModelID  = "judge-y"
	)
	runner := &fakeRunner{answer: "council says foo", consensusW: 0.73}
	client := &fakeLLMClient{
		responses: map[string]string{
			baselineModel: "baseline says bar",
			judgeModelID:  judgeReply("A", "council answer is more focused"),
		},
	}

	results, err := Run(context.Background(), Options{
		Suite:         Suite{Cases: []Case{{ID: "c1", Prompt: "what is foo?", Category: "factual"}}},
		Runner:        runner,
		Client:        client,
		BaselineModel: baselineModel,
		JudgeModel:    judgeModelID,
		CouncilType:   "default",
		Temperature:   0.7,
		Seed:          42, // deterministic
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.CouncilAnswer != "council says foo" {
		t.Errorf("CouncilAnswer: got %q", r.CouncilAnswer)
	}
	if r.BaselineAnswer != "baseline says bar" {
		t.Errorf("BaselineAnswer: got %q", r.BaselineAnswer)
	}
	if r.CouncilConsensusW != 0.73 {
		t.Errorf("CouncilConsensusW: got %v, want 0.73", r.CouncilConsensusW)
	}
	// Verdict can be either VerdictCouncil or VerdictBaseline depending on the
	// per-case A/B order, but it MUST be one of the pipeline names — never
	// the raw "A" or "B" — and never VerdictError on this happy path.
	if r.JudgeVerdict != VerdictCouncil && r.JudgeVerdict != VerdictBaseline {
		t.Errorf("JudgeVerdict: got %q, want council or baseline", r.JudgeVerdict)
	}
	if r.Error != "" {
		t.Errorf("Error should be empty, got %q", r.Error)
	}
}

// ── Run: blinding + remap ──────────────────────────────────────────────────

// Both cases below pin the seed so we can know whether councilFirst will be
// true or false, and assert that the same raw verdict ("A") remaps to the
// correct pipeline name in each ordering.
func TestRun_RemapHonoursABOrder(t *testing.T) {
	// Find one seed where councilFirst=true and one where councilFirst=false
	// for case index 0. The PCG output for IntN(2) varies per seed so we
	// just iterate small seeds until we have both.
	const (
		baselineModel = "baseline-x"
		judgeModelID  = "judge-y"
	)
	tests := []struct {
		name              string
		councilFirstSeed  int64
		judgeReturnsA     bool
		wantVerdict       Verdict
	}{
		// Verdict "A" in a councilFirst run → council won.
		{"council A wins when councilFirst", findSeedWithCouncilFirst(t, true), true, VerdictCouncil},
		// Verdict "A" in a baselineFirst run → baseline won.
		{"baseline A wins when baselineFirst", findSeedWithCouncilFirst(t, false), true, VerdictBaseline},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{answer: "C", consensusW: 1}
			client := &fakeLLMClient{responses: map[string]string{
				baselineModel: "B",
				judgeModelID:  judgeReply("A", "answer A is better"),
			}}
			results, err := Run(context.Background(), Options{
				Suite:         Suite{Cases: []Case{{ID: "x", Prompt: "?", Category: "test"}}},
				Runner:        runner,
				Client:        client,
				BaselineModel: baselineModel,
				JudgeModel:    judgeModelID,
				CouncilType:   "default",
				Seed:          tc.councilFirstSeed,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if results[0].JudgeVerdict != tc.wantVerdict {
				t.Errorf("verdict: got %q, want %q", results[0].JudgeVerdict, tc.wantVerdict)
			}
		})
	}
}

func TestRun_TieRemapsToTie(t *testing.T) {
	const (
		baselineModel = "baseline-x"
		judgeModelID  = "judge-y"
	)
	runner := &fakeRunner{answer: "C", consensusW: 1}
	client := &fakeLLMClient{responses: map[string]string{
		baselineModel: "B",
		judgeModelID:  judgeReply("tie", "equally good"),
	}}
	results, err := Run(context.Background(), Options{
		Suite:         Suite{Cases: []Case{{ID: "x", Prompt: "?", Category: "test"}}},
		Runner:        runner,
		Client:        client,
		BaselineModel: baselineModel,
		JudgeModel:    judgeModelID,
		CouncilType:   "default",
		Seed:          1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].JudgeVerdict != VerdictTie {
		t.Errorf("verdict: got %q, want tie", results[0].JudgeVerdict)
	}
}

// ── Run: error handling ────────────────────────────────────────────────────

func TestRun_CouncilErrorRecordedAsVerdictError(t *testing.T) {
	const (
		baselineModel = "baseline-x"
		judgeModelID  = "judge-y"
	)
	runner := &fakeRunner{errOnRun: errors.New("quorum not met")}
	client := &fakeLLMClient{responses: map[string]string{
		baselineModel: "B",
		judgeModelID:  judgeReply("A", "x"),
	}}

	results, err := Run(context.Background(), Options{
		Suite:         Suite{Cases: []Case{{ID: "x", Prompt: "?", Category: "test"}}},
		Runner:        runner,
		Client:        client,
		BaselineModel: baselineModel,
		JudgeModel:    judgeModelID,
		CouncilType:   "default",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0]
	if r.JudgeVerdict != VerdictError {
		t.Errorf("verdict: got %q, want error", r.JudgeVerdict)
	}
	if !strings.Contains(r.Error, "quorum not met") {
		t.Errorf("Error: got %q, want it to contain 'quorum not met'", r.Error)
	}
	// Judge MUST NOT have been called when council errored.
	for _, call := range client.calls {
		if call.Model == judgeModelID {
			t.Error("judge was called despite council error")
		}
	}
}

func TestRun_BaselineErrorRecordedAsVerdictError(t *testing.T) {
	const (
		baselineModel = "baseline-x"
		judgeModelID  = "judge-y"
	)
	runner := &fakeRunner{answer: "C", consensusW: 1}
	client := &fakeLLMClient{
		responses: map[string]string{judgeModelID: judgeReply("A", "x")},
		errs:      map[string]error{baselineModel: errors.New("rate limited")},
	}

	results, err := Run(context.Background(), Options{
		Suite:         Suite{Cases: []Case{{ID: "x", Prompt: "?", Category: "test"}}},
		Runner:        runner,
		Client:        client,
		BaselineModel: baselineModel,
		JudgeModel:    judgeModelID,
		CouncilType:   "default",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0]
	if r.JudgeVerdict != VerdictError {
		t.Errorf("verdict: got %q, want error", r.JudgeVerdict)
	}
	if !strings.Contains(r.Error, "rate limited") {
		t.Errorf("Error: got %q, want it to contain 'rate limited'", r.Error)
	}
}

func TestRun_MixedOutcomesDoNotAbort(t *testing.T) {
	// Three cases: two ordinary successes followed by a council failure.
	// After the failure the orchestrator must still process all three rows.
	const (
		baselineModel = "baseline-x"
		judgeModelID  = "judge-y"
	)
	runner := &fakeRunner{answer: "C", consensusW: 1}
	client := &fakeLLMClient{responses: map[string]string{
		baselineModel: "B",
		judgeModelID:  judgeReply("tie", "x"),
	}}
	suite := Suite{Cases: []Case{
		{ID: "ok-1", Prompt: "?", Category: "test"},
		{ID: "ok-2", Prompt: "?", Category: "test"},
		{ID: "fail-1", Prompt: "?", Category: "test"},
	}}

	// Swap to a failing runner just for the third case by wrapping.
	failing := &runnerFailingOnCase{base: runner, failID: "fail-1"}

	results, err := Run(context.Background(), Options{
		Suite:         suite,
		Runner:        failing,
		Client:        client,
		BaselineModel: baselineModel,
		JudgeModel:    judgeModelID,
		CouncilType:   "default",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].JudgeVerdict != VerdictTie || results[1].JudgeVerdict != VerdictTie {
		t.Errorf("first two verdicts: got %q,%q, want tie,tie", results[0].JudgeVerdict, results[1].JudgeVerdict)
	}
	if results[2].JudgeVerdict != VerdictError {
		t.Errorf("third verdict: got %q, want error", results[2].JudgeVerdict)
	}
}

// ── Run: validation ────────────────────────────────────────────────────────

func TestRun_ValidatesRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"missing runner", Options{Client: &fakeLLMClient{}, BaselineModel: "b", JudgeModel: "j"}, "Runner is required"},
		{"missing client", Options{Runner: &fakeRunner{}, BaselineModel: "b", JudgeModel: "j"}, "Client is required"},
		{"missing baseline", Options{Runner: &fakeRunner{}, Client: &fakeLLMClient{}, JudgeModel: "j"}, "BaselineModel is required"},
		{"missing judge", Options{Runner: &fakeRunner{}, Client: &fakeLLMClient{}, BaselineModel: "b"}, "JudgeModel is required"},
		{"judge equals baseline", Options{Runner: &fakeRunner{}, Client: &fakeLLMClient{}, BaselineModel: "x", JudgeModel: "x"}, "must differ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Run(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err: got %v, want substring %q", err, tc.want)
			}
		})
	}
}

// ── Run: SuiteSHA256 ──────────────────────────────────────────────────────

func TestSuiteSHA256_DeterministicAndDifferentForDifferentInputs(t *testing.T) {
	a := SuiteSHA256([]byte(`[{"id":"x","prompt":"hi","category":"c"}]`))
	b := SuiteSHA256([]byte(`[{"id":"x","prompt":"hi","category":"c"}]`))
	c := SuiteSHA256([]byte(`[{"id":"y","prompt":"hi","category":"c"}]`))
	if a != b {
		t.Error("same input should produce same hash")
	}
	if a == c {
		t.Error("different input should produce different hash")
	}
	if len(a) != 64 {
		t.Errorf("hash length: got %d, want 64 (sha256 hex)", len(a))
	}
}

// ── Run: LoadSuite ─────────────────────────────────────────────────────────

func TestLoadSuite_ParsesCases(t *testing.T) {
	suite, err := LoadSuite([]byte(`[{"id":"a","prompt":"P","category":"factual"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Cases) != 1 || suite.Cases[0].ID != "a" {
		t.Errorf("got %+v, want one case with ID=a", suite.Cases)
	}
}

func TestLoadSuite_RejectsBadJSON(t *testing.T) {
	_, err := LoadSuite([]byte(`{not: valid`))
	if err == nil {
		t.Error("expected parse error")
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// runnerFailingOnCase wraps a runner so that for one specific Case ID the
// RunFull call returns an error, but for every other ID it delegates to the
// base runner. We can't switch on case ID inside RunFull because the council
// API doesn't pass it through, so we count calls instead.
type runnerFailingOnCase struct {
	base    *fakeRunner
	failID  string
	calls   int
}

func (r *runnerFailingOnCase) RunFull(ctx context.Context, prompt string, councilType string, onEvent council.EventFunc) error {
	r.calls++
	// Fail on the third call (matches "fail-1" in the test fixture).
	if r.calls == 3 {
		return errors.New("council quorum not met")
	}
	return r.base.RunFull(ctx, prompt, councilType, onEvent)
}

// findSeedWithCouncilFirst returns a small int64 seed whose first PCG IntN(2)
// draw matches `want`. Used by remap tests so we can pin orderings without
// exporting the RNG.
func findSeedWithCouncilFirst(t *testing.T, want bool) int64 {
	t.Helper()
	for s := int64(1); s < 1000; s++ {
		if councilFirstForSeed(s) == want {
			return s
		}
	}
	t.Fatalf("could not find seed with councilFirst=%v in [1, 1000)", want)
	return 0
}

// councilFirstForSeed mirrors the exact RNG construction used by Run so the
// test's expectation matches production behaviour.
func councilFirstForSeed(seed int64) bool {
	rng := rand.New(rand.NewPCG(uint64(seed), 0))
	return rng.IntN(2) == 0
}
