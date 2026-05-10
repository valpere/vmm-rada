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

// debateRecorder is a thread-safe collector that classifies each completion
// request as a Stage 1 generation, a debate-round revision call, or a Stage 3
// chairman call by inspecting the prompt prefix.
type debateRecorder struct {
	mu sync.Mutex
	// stage1 maps model → answer to return for Stage 1 calls.
	stage1 map[string]string
	// roundOutByModel[round][model] → JSON {"critique":"...","revision":"..."} string for that model in that round.
	// If a model is missing for a round, the recorder returns roundDefaults below.
	roundOutByModel map[int]map[string]string
	// roundErrByModel[round][model] → error to return for that model in that round.
	roundErrByModel map[int]map[string]error
	// roundDefaults is the default revision JSON if no per-(round, model) entry exists.
	roundDefaults string
	chairmanOut   string
	chairmanErr   error

	calls         []string // labels: "stage1:<model>", "debate:<model>:r<round>", "chairman:<model>"
	debatePrompts []string // captured per-round debater prompts (for anonymisation tests)
	chairmanPrompt string  // captured chairman prompt
}

func (r *debateRecorder) record(req CompletionRequest) (CompletionResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	body := ""
	if len(req.Messages) > 0 {
		body = req.Messages[0].Content
	}
	switch {
	case strings.Contains(body, "You debate council answers"):
		// Determine round from the prompt header "round N of M".
		round := 0
		fmt.Sscanf(strings.SplitN(body[strings.Index(body, "This is round "):], "\n", 2)[0], "This is round %d", &round)
		r.calls = append(r.calls, fmt.Sprintf("debate:%s:r%d", req.Model, round))
		r.debatePrompts = append(r.debatePrompts, body)

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
			out = `{"critique":"default","revision":"default revised answer"}`
		}
		return makeResponse(out), nil
	case strings.Contains(body, "You synthesise the final answer from a multi-agent debate"):
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

func debateCouncil(t *testing.T, models []string, chairman string, maxRounds int, rec *debateRecorder) *Council {
	t.Helper()
	registry := map[string]CouncilType{
		"test": {
			Name:            "test",
			Strategy:        MultiAgentDebate,
			Models:          models,
			ChairmanModel:   chairman,
			Temperature:     0.7,
			MaxDebateRounds: maxRounds,
		},
	}
	client := &mockLLMClient{
		complete: func(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
			return rec.record(req)
		},
	}
	return NewCouncil(client, registry, nil)
}

// debateRevision builds a {"critique":"...","revision":"..."} string for fixtures.
func debateRevision(critique, revision string) string {
	return fmt.Sprintf(`{"critique":%q,"revision":%q}`, critique, revision)
}

// ── 1. Happy path ─────────────────────────────────────────────────────────

func TestRunMultiAgentDebate_HappyPath(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{
			"m-a": "ans-a", "m-b": "ans-b", "m-c": "ans-c", "m-d": "ans-d",
		},
		roundDefaults: debateRevision("crit", "rev"),
		chairmanOut:   "Final synthesis.",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0 /* default 2 rounds */, rec)

	var events []string
	var rounds []int
	var seenKind string
	var debate *Debate
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
				debate = d.Metadata.Debate
			}
		}
		if eventType == "stage3_complete" {
			stage3 = data.(StageThreeResult)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSeq := []string{"stage1_complete", "stage2_round_complete", "stage2_round_complete", "stage2_complete", "stage3_complete"}
	if !reflect.DeepEqual(events, wantSeq) {
		t.Errorf("event sequence: got %v, want %v", events, wantSeq)
	}
	if !reflect.DeepEqual(rounds, []int{1, 2}) {
		t.Errorf("round events: got %v, want [1 2]", rounds)
	}
	if seenKind != "debate_round" {
		t.Errorf("Stage 2 Kind: got %q, want %q", seenKind, "debate_round")
	}
	if debate == nil {
		t.Fatal("Metadata.Debate: nil")
	}
	if debate.FinalRound != 2 {
		t.Errorf("FinalRound: got %d, want 2", debate.FinalRound)
	}
	if len(debate.Rounds) != 2 {
		t.Fatalf("Rounds: got %d, want 2", len(debate.Rounds))
	}
	for i, r := range debate.Rounds {
		if len(r.Revisions) != 4 {
			t.Errorf("round %d revisions: got %d, want 4", i+1, len(r.Revisions))
		}
	}
	if stage3.Content != "Final synthesis." {
		t.Errorf("stage3 content: got %q", stage3.Content)
	}
}

// ── 2. Custom MaxDebateRounds=3 ─────────────────────────────────────────

func TestRunMultiAgentDebate_CustomMaxRounds(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c",
		},
		roundDefaults: debateRevision("c", "r"),
		chairmanOut:   "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 3, rec)

	var roundCount int
	if err := c.RunFull(context.Background(), "q", "test", func(eventType string, _ any) {
		if eventType == "stage2_round_complete" {
			roundCount++
		}
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if roundCount != 3 {
		t.Errorf("round events: got %d, want 3", roundCount)
	}
}

// ── 3. Default MaxDebateRounds=2 ────────────────────────────────────────

func TestRunMultiAgentDebate_DefaultMaxRoundsIsTwo(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{
			"m-a": "a", "m-b": "b", "m-c": "c",
		},
		roundDefaults: debateRevision("c", "r"),
		chairmanOut:   "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 0, rec)

	var roundCount int
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, _ any) {
		if eventType == "stage2_round_complete" {
			roundCount++
		}
	})
	if roundCount != 2 {
		t.Errorf("default rounds: got %d, want 2", roundCount)
	}
}

// ── 4. Quorum failure at round 0 ────────────────────────────────────────

func TestRunMultiAgentDebate_QuorumFailureAtRound0(t *testing.T) {
	// All Stage 1 calls fail (no scripted answers) → quorum fails.
	rec := &debateRecorder{stage1: map[string]string{}}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 0, rec)

	err := c.RunFull(context.Background(), "q", "test", nil)
	if err == nil {
		t.Fatal("expected QuorumError")
	}
	var qe *QuorumError
	if !errors.As(err, &qe) {
		t.Errorf("error type: got %T, want *QuorumError", err)
	}
}

// ── 5. Per-debater failure mid-debate ───────────────────────────────────

func TestRunMultiAgentDebate_PerDebaterFailureDropsOut(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": debateRevision("c", "r-a-1"),
				"m-c": debateRevision("c", "r-c-1"),
				"m-d": debateRevision("c", "r-d-1"),
			},
			2: {
				"m-a": debateRevision("c", "r-a-2"),
				"m-c": debateRevision("c", "r-c-2"),
				"m-d": debateRevision("c", "r-d-2"),
			},
		},
		roundErrByModel: map[int]map[string]error{
			1: {"m-b": errors.New("upstream timeout")}, // m-b fails in round 1
		},
		chairmanOut: "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 2, rec)

	var debate *Debate
	err := c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				debate = d.Metadata.Debate
			}
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if debate == nil {
		t.Fatal("debate nil")
	}
	if len(debate.Rounds) != 2 {
		t.Fatalf("rounds: got %d, want 2", len(debate.Rounds))
	}
	// Round 1 should have 3 revisions (m-b dropped).
	if len(debate.Rounds[0].Revisions) != 3 {
		t.Errorf("round 1 revisions: got %d, want 3", len(debate.Rounds[0].Revisions))
	}
	// Round 2 should also have 3 revisions (m-b stays out).
	if len(debate.Rounds[1].Revisions) != 3 {
		t.Errorf("round 2 revisions: got %d, want 3", len(debate.Rounds[1].Revisions))
	}
	// Dropouts should record m-b.
	if len(debate.Dropouts) != 1 {
		t.Fatalf("dropouts: got %d, want 1", len(debate.Dropouts))
	}
	if debate.Dropouts[0].Reason != dropReasonError {
		t.Errorf("dropout reason: got %q, want %q", debate.Dropouts[0].Reason, dropReasonError)
	}
	if debate.Dropouts[0].LastRound != 0 {
		t.Errorf("dropout last_round: got %d, want 0 (m-b only had a round-0 answer)", debate.Dropouts[0].LastRound)
	}
}

// ── 6. Quorum failure mid-debate ────────────────────────────────────────

func TestRunMultiAgentDebate_QuorumFailureMidDebate(t *testing.T) {
	// 4 debaters, need = max(2, ⌈4/2⌉+1) = 3. Two fail in round 1 → 2 survivors → loud error.
	rec := &debateRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": debateRevision("c", "r-a"),
				"m-b": debateRevision("c", "r-b"),
			},
		},
		roundErrByModel: map[int]map[string]error{
			1: {
				"m-c": errors.New("fail c"),
				"m-d": errors.New("fail d"),
			},
		},
		chairmanOut: "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 2, rec)

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
		t.Fatal("expected loud error on mid-debate quorum drop")
	}
	if !strings.Contains(err.Error(), "quorum failed after round 1") {
		t.Errorf("error message: got %q, want mention of 'quorum failed after round 1'", err.Error())
	}
	if stage2CompleteEmitted {
		t.Error("must NOT emit terminal stage2_complete on quorum failure")
	}
	if stage3Emitted {
		t.Error("must NOT emit stage3_complete on quorum failure")
	}
}

// ── 7. Anonymisation ────────────────────────────────────────────────────

func TestRunMultiAgentDebate_AnonymisationInPrompts(t *testing.T) {
	// Verify per-round prompts contain labels but NOT model names. Also verify
	// self is NOT in the OTHERS list of its own prompt.
	rec := &debateRecorder{
		stage1: map[string]string{
			"my-secret-model-a": "a",
			"my-secret-model-b": "b",
			"my-secret-model-c": "c",
		},
		roundDefaults: debateRevision("c", "r"),
		chairmanOut:   "ok",
	}
	c := debateCouncil(t, []string{"my-secret-model-a", "my-secret-model-b", "my-secret-model-c"}, "chairman-z", 1, rec)

	if err := c.RunFull(context.Background(), "q", "test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 debater prompts captured (one per debater in round 1).
	if len(rec.debatePrompts) != 3 {
		t.Fatalf("debate prompts: got %d, want 3", len(rec.debatePrompts))
	}
	for _, prompt := range rec.debatePrompts {
		// Model names must NOT appear in the per-round prompt body.
		for _, model := range []string{"my-secret-model-a", "my-secret-model-b", "my-secret-model-c"} {
			if strings.Contains(prompt, model) {
				t.Errorf("prompt leaks model name %q:\n%s", model, prompt)
			}
		}
		// Labels (Response A/B/C) MUST appear at least somewhere.
		hasAny := false
		for _, lbl := range []string{"Response A", "Response B", "Response C"} {
			if strings.Contains(prompt, lbl) {
				hasAny = true
				break
			}
		}
		if !hasAny {
			t.Errorf("prompt has no anonymous labels:\n%s", prompt)
		}
	}
}

// ── 8. JSON parse failure for one debater ──────────────────────────────

func TestRunMultiAgentDebate_JSONParseFailureDropsOut(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": debateRevision("c", "r-a-1"),
				"m-b": "not valid json {{{",
				"m-c": debateRevision("c", "r-c-1"),
				"m-d": debateRevision("c", "r-d-1"),
			},
		},
		roundDefaults: debateRevision("c", "r-default"),
		chairmanOut:   "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 2, rec)

	var debate *Debate
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				debate = d.Metadata.Debate
			}
		}
	})
	if debate == nil {
		t.Fatal("debate nil")
	}
	// m-b's parse fails in round 1 → dropped.
	if len(debate.Rounds[0].Revisions) != 3 {
		t.Errorf("round 1 revisions: got %d, want 3", len(debate.Rounds[0].Revisions))
	}
	// Dropout reason should be json_parse.
	found := false
	for _, d := range debate.Dropouts {
		if d.Reason == dropReasonJSONParse {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a json_parse dropout, got %+v", debate.Dropouts)
	}
}

// ── 9. Chairman receives full transcript + dropouts ────────────────────

func TestRunMultiAgentDebate_ChairmanReceivesTranscriptAndDropouts(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{"m-a": "stage1-a", "m-b": "stage1-b", "m-c": "stage1-c", "m-d": "stage1-d"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": debateRevision("c", "REV-A-R1"),
				"m-c": debateRevision("c", "REV-C-R1"),
				"m-d": debateRevision("c", "REV-D-R1"),
			},
		},
		roundErrByModel: map[int]map[string]error{
			1: {"m-b": errors.New("upstream")},
		},
		roundDefaults: debateRevision("c", "default-r2"),
		chairmanOut:   "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 2, rec)

	if err := c.RunFull(context.Background(), "q", "test", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Chairman prompt must include round-0 answers (Stage 1) AND round revisions AND dropout markers.
	for _, want := range []string{"stage1-a", "stage1-b", "stage1-c", "stage1-d", "REV-A-R1", "REV-C-R1", "REV-D-R1"} {
		if !strings.Contains(rec.chairmanPrompt, want) {
			t.Errorf("chairman prompt missing %q", want)
		}
	}
	// Dropout marker for m-b's label.
	if !strings.Contains(rec.chairmanPrompt, "Dropouts") {
		t.Error("chairman prompt missing 'Dropouts' section")
	}
}

// ── 10. Empty critique handled ─────────────────────────────────────────

func TestRunMultiAgentDebate_EmptyCritiqueAccepted(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundOutByModel: map[int]map[string]string{
			1: {
				"m-a": `{"critique":"","revision":"answer-a"}`,
				"m-b": `{"critique":"","revision":"answer-b"}`,
				"m-c": `{"critique":"","revision":"answer-c"}`,
			},
		},
		chairmanOut: "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 1, rec)

	var debate *Debate
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				debate = d.Metadata.Debate
			}
		}
	})
	if debate == nil || len(debate.Rounds) != 1 {
		t.Fatalf("expected 1 round, got %v", debate)
	}
	if len(debate.Rounds[0].Revisions) != 3 {
		t.Errorf("revisions: got %d, want 3 (empty critique should NOT drop debater)", len(debate.Rounds[0].Revisions))
	}
	for _, rev := range debate.Rounds[0].Revisions {
		if rev.Critique != "" {
			t.Errorf("expected empty critique, got %q", rev.Critique)
		}
	}
}

// ── 11. Sort stability of revisions within a round ────────────────────

func TestRunMultiAgentDebate_RevisionSortStability(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundDefaults: debateRevision("c", "r"),
		chairmanOut:   "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 1, rec)

	var debate *Debate
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok {
				debate = d.Metadata.Debate
			}
		}
	})
	if debate == nil || len(debate.Rounds) != 1 {
		t.Fatalf("debate nil or wrong round count")
	}
	revs := debate.Rounds[0].Revisions
	for i := 1; i < len(revs); i++ {
		if revs[i-1].Label > revs[i].Label {
			t.Errorf("revisions not sorted ascending by Label: %v", revs)
			break
		}
	}
}

// ── 12. Stage 2 kind on terminal event ────────────────────────────────

func TestRunMultiAgentDebate_TerminalEventKind(t *testing.T) {
	rec := &debateRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c"},
		roundDefaults: debateRevision("c", "r"),
		chairmanOut:   "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c"}, "chairman-z", 1, rec)

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
		if k != "debate_round" {
			t.Errorf("round event kind: got %q, want %q", k, "debate_round")
		}
	}
	if terminalKind != "debate_round" {
		t.Errorf("terminal kind: got %q, want %q", terminalKind, "debate_round")
	}
}

// ── 13. Transcript-agreement contract ─────────────────────────────────

func TestRunMultiAgentDebate_TranscriptAgreement(t *testing.T) {
	// Cumulative []DebateRound from per-round events MUST equal Metadata.Debate.Rounds
	// from the terminal event. Single most important guarantee of the new event type.
	rec := &debateRecorder{
		stage1: map[string]string{"m-a": "a", "m-b": "b", "m-c": "c", "m-d": "d"},
		roundDefaults: debateRevision("c", "r"),
		chairmanOut:   "ok",
	}
	c := debateCouncil(t, []string{"m-a", "m-b", "m-c", "m-d"}, "chairman-z", 2, rec)

	var cumulative []DebateRound
	var canonical []DebateRound
	_ = c.RunFull(context.Background(), "q", "test", func(eventType string, data any) {
		if eventType == "stage2_round_complete" {
			if d, ok := data.(Stage2CompleteData); ok && d.Metadata.Debate != nil {
				cumulative = append(cumulative, d.Metadata.Debate.Rounds...)
			}
		}
		if eventType == "stage2_complete" {
			if d, ok := data.(Stage2CompleteData); ok && d.Metadata.Debate != nil {
				canonical = d.Metadata.Debate.Rounds
			}
		}
	})

	if len(cumulative) == 0 || len(canonical) == 0 {
		t.Fatalf("missing rounds: cumulative=%d, canonical=%d", len(cumulative), len(canonical))
	}
	if !reflect.DeepEqual(cumulative, canonical) {
		t.Errorf("cumulative round events disagree with canonical Metadata.Debate.Rounds:\ncumulative=%+v\ncanonical=%+v", cumulative, canonical)
	}
}
