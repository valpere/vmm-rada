package api

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/valpere/vmm-rada/internal/council"
	"github.com/valpere/vmm-rada/internal/storage"
)

// updateGolden regenerates testdata/*.golden from current handler output.
// Regenerated files are reviewed via `git diff` like any other source
// change — that review is the drift check this test suite exists to
// provide. Run: go test ./internal/api/... -run TestContract -update
var updateGolden = flag.Bool("update", false, "update golden files")

// checkGolden compares got against testdata/<name>.golden, or writes it
// when -update is passed. JSON bodies should be pretty-printed by the
// caller before calling this, for stable, readable diffs.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")

	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden mismatch for %s (run with -update to review and accept the diff):\n--- want (%s) ---\n%s\n--- got ---\n%s",
			name, path, want, got)
	}
}

// prettyJSON re-indents a JSON body for stable, readable golden diffs.
func prettyJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("prettyJSON: invalid JSON: %v\nraw: %s", err, raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("prettyJSON: marshal: %v", err)
	}
	return append(out, '\n')
}

// contractStage0RoundRunner returns a mockStage0Runner whose
// RunClarificationRound always fires stage0_round_complete with a fixed
// round/question, never delegating to RunFullWithClarifications. Used for
// the "Stage 0 fires" golden scenarios.
func contractStage0RoundRunner() *mockStage0Runner {
	return &mockStage0Runner{
		runClarificationRound: func(_ context.Context, _ string, _ []council.ClarificationRound, _ council.ClarificationConfig, _ string, onEvent council.EventFunc) error {
			if onEvent != nil {
				onEvent("stage0_round_complete", council.Stage0RoundData{
					Round: 1,
					Questions: []council.ClarificationQuestion{
						{ID: "q1", Text: "What database are you currently using?"},
					},
				})
			}
			return nil
		},
	}
}

// contractRunFull emits a fixed, deterministic PeerReview-shaped sequence —
// no time.Now(), no random labels; DurationMs is a fixed fixture value.
func contractRunFull(_ context.Context, _, _ string, onEvent council.EventFunc) error {
	onEvent("stage1_complete", []council.StageOneResult{
		{Label: "Response A", Content: "Go is a compiled language.", Model: "openai/gpt-4o-mini", DurationMs: 100},
		{Label: "Response B", Content: "Go was created at Google.", Model: "anthropic/claude-haiku-4-5", DurationMs: 120},
	})
	onEvent("stage2_complete", council.Stage2CompleteData{
		Kind: "peer_ranking",
		Results: []council.StageTwoResult{
			{ReviewerLabel: "Response A", Rankings: []string{"Response A", "Response B"}},
			{ReviewerLabel: "Response B", Rankings: []string{"Response A", "Response B"}},
		},
		Metadata: council.Metadata{
			CouncilType:  "standard",
			LabelToModel: map[string]string{"Response A": "openai/gpt-4o-mini", "Response B": "anthropic/claude-haiku-4-5"},
			AggregateRankings: []council.RankedModel{
				{Model: "openai/gpt-4o-mini", Score: 1.0},
				{Model: "anthropic/claude-haiku-4-5", Score: 2.0},
			},
			ConsensusW: 1.0,
		},
	})
	onEvent("stage3_complete", council.StageThreeResult{Content: "Go is a compiled language created at Google.", Model: "openai/gpt-4o-mini", DurationMs: 200})
	return nil
}

// closedConversationStorer returns a mockStorer whose SaveUserMessage
// always reports the conversation as closed, for the 409-parity scenario.
func closedConversationStorer() *mockStorer {
	return &mockStorer{
		saveUserMessage: func(string, string) error { return storage.ErrConversationClosed },
	}
}

// roundNClosedConversationStorer returns a mockStorer simulating a
// round-2+ answers submission against a closed conversation: a pending
// clarification round exists, but UpdateClarificationAnswers reports the
// conversation as closed (regression coverage for #309).
func roundNClosedConversationStorer() *mockStorer {
	return &mockStorer{
		getLastClarificationRound: func(string) (*council.ClarificationRound, error) {
			return &council.ClarificationRound{
				Round:     1,
				Questions: []council.ClarificationQuestion{{ID: "q1", Text: "What database?"}},
			}, nil
		},
		updateClarificationAnswers: func(string, int, []council.ClarificationAnswer) error {
			return storage.ErrConversationClosed
		},
	}
}

// ── POST /message ────────────────────────────────────────────────────────

func TestContract_SendMessage_HappyPath(t *testing.T) {
	h := newTestHandler(okStorer(), &mockRunner{runFull: contractRunFull})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message", bytes.NewBufferString(`{"content":"What is Go?"}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	checkGolden(t, "message_happy_path", prettyJSON(t, w.Body.Bytes()))
}

func TestContract_SendMessage_Stage0Fires(t *testing.T) {
	h := NewHandler(&mockRunner{runFull: contractRunFull}, contractStage0RoundRunner(), okStorer(), nil, "standard", council.ClarificationConfig{MaxRounds: 2, MaxTotalQuestions: 5, MaxQuestionsPerRound: 3})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message", bytes.NewBufferString(`{"content":"Should I migrate to Postgres?"}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	checkGolden(t, "message_stage0_fires", prettyJSON(t, w.Body.Bytes()))
}

func TestContract_SendMessage_ConversationClosed(t *testing.T) {
	h := newTestHandler(closedConversationStorer(), &mockRunner{})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message", bytes.NewBufferString(`{"content":"hello"}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessage(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409; body: %s", w.Code, w.Body.String())
	}
	checkGolden(t, "message_conversation_closed", prettyJSON(t, w.Body.Bytes()))
}

// TestContract_SendMessage_RoundNConversationClosed is a regression test
// for #309: round-N answers submissions must be rejected the same way
// round-1 content submissions already are, not just silently accepted.
func TestContract_SendMessage_RoundNConversationClosed(t *testing.T) {
	h := newTestHandler(roundNClosedConversationStorer(), &mockRunner{})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message", bytes.NewBufferString(`{"answers":[{"id":"q1","text":"Postgres"}]}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessage(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409; body: %s", w.Code, w.Body.String())
	}
	checkGolden(t, "message_conversation_closed", prettyJSON(t, w.Body.Bytes()))
}

// ── POST /message/stream ─────────────────────────────────────────────────

func TestContract_SendMessageStream_HappyPath(t *testing.T) {
	h := newTestHandler(okStorer(), &mockRunner{runFull: contractRunFull})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message/stream", bytes.NewBufferString(`{"content":"What is Go?"}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessageStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	checkGolden(t, "message_stream_happy_path", stripVolatileSSEFields(t, w.Body.Bytes()))
}

func TestContract_SendMessageStream_Stage0Fires(t *testing.T) {
	h := NewHandler(&mockRunner{runFull: contractRunFull}, contractStage0RoundRunner(), okStorer(), nil, "standard", council.ClarificationConfig{MaxRounds: 2, MaxTotalQuestions: 5, MaxQuestionsPerRound: 3})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message/stream", bytes.NewBufferString(`{"content":"Should I migrate to Postgres?"}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessageStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	// Confirms drift #1/#3: stage0_done never appears on the wire, and a
	// complete event IS sent after stage0_round_complete.
	checkGolden(t, "message_stream_stage0_fires", stripVolatileSSEFields(t, w.Body.Bytes()))
}

func TestContract_SendMessageStream_ConversationClosed(t *testing.T) {
	h := newTestHandler(closedConversationStorer(), &mockRunner{})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message/stream", bytes.NewBufferString(`{"content":"hello"}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessageStream(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409; body: %s", w.Code, w.Body.String())
	}
	checkGolden(t, "message_stream_conversation_closed", prettyJSON(t, w.Body.Bytes()))
}

// TestContract_SendMessageStream_RoundNConversationClosed is the streaming
// counterpart of TestContract_SendMessage_RoundNConversationClosed (#309).
func TestContract_SendMessageStream_RoundNConversationClosed(t *testing.T) {
	h := newTestHandler(roundNClosedConversationStorer(), &mockRunner{})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message/stream", bytes.NewBufferString(`{"answers":[{"id":"q1","text":"Postgres"}]}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessageStream(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409; body: %s", w.Code, w.Body.String())
	}
	checkGolden(t, "message_stream_conversation_closed", prettyJSON(t, w.Body.Bytes()))
}

func TestContract_SendMessageStream_MultiAgentDebate(t *testing.T) {
	h := newTestHandler(okStorer(), &mockRunner{runFull: func(_ context.Context, _, _ string, onEvent council.EventFunc) error {
		onEvent("stage1_complete", []council.StageOneResult{
			{Label: "Response A", Content: "ans-a", Model: "openai/gpt-4o-mini", DurationMs: 100},
			{Label: "Response B", Content: "ans-b", Model: "anthropic/claude-haiku-4-5", DurationMs: 110},
		})
		onEvent("stage2_round_complete", council.Stage2CompleteData{
			Kind:  "debate_round",
			Round: 1,
			Metadata: council.Metadata{
				CouncilType:  "debate",
				LabelToModel: map[string]string{"Response A": "openai/gpt-4o-mini", "Response B": "anthropic/claude-haiku-4-5"},
				Debate: &council.Debate{
					Rounds:     []council.DebateRound{{Round: 1, Revisions: []council.DebaterRevision{{Label: "Response A", Critique: "c", Content: "rev-a-1"}}}},
					FinalRound: 1,
				},
			},
		})
		onEvent("stage2_complete", council.Stage2CompleteData{
			Kind: "debate_round",
			Metadata: council.Metadata{
				CouncilType:  "debate",
				LabelToModel: map[string]string{"Response A": "openai/gpt-4o-mini", "Response B": "anthropic/claude-haiku-4-5"},
				Debate: &council.Debate{
					Rounds:     []council.DebateRound{{Round: 1, Revisions: []council.DebaterRevision{{Label: "Response A", Content: "rev-a-1"}}}},
					FinalRound: 1,
				},
			},
		})
		onEvent("stage3_complete", council.StageThreeResult{Content: "synthesis", Model: "chairman-z", DurationMs: 150})
		return nil
	}})
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message/stream", bytes.NewBufferString(`{"content":"q","council_type":"debate"}`))
	req.SetPathValue("id", testConvID)
	w := httptest.NewRecorder()

	h.sendMessageStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	checkGolden(t, "message_stream_multi_agent_debate", stripVolatileSSEFields(t, w.Body.Bytes()))
}

// stripVolatileSSEFields removes the title_complete event, which is
// produced by a real goroutine racing a 30s timer against title-derivation
// logic and is exercised separately in handler_test.go; keeping it out of
// golden files avoids a source of flakiness unrelated to what these
// contract tests check (route/stage-event wire shape).
func stripVolatileSSEFields(t *testing.T, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, line := range bytes.Split(body, []byte("\n")) {
		if bytes.Contains(line, []byte(`"type":"title_complete"`)) {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// TestContract_ConversationClosedParity is the drift-#-none regression:
// both endpoints must produce the same status code and error body shape
// for the same underlying error, proving the sendMessage fix actually
// closes the gap with sendMessageStream.
func TestContract_ConversationClosedParity(t *testing.T) {
	blocking := newTestHandler(closedConversationStorer(), &mockRunner{})
	breq := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message", bytes.NewBufferString(`{"content":"hello"}`))
	breq.SetPathValue("id", testConvID)
	bw := httptest.NewRecorder()
	blocking.sendMessage(bw, breq)

	streaming := newTestHandler(closedConversationStorer(), &mockRunner{})
	sreq := httptest.NewRequest(http.MethodPost, "/api/conversations/"+testConvID+"/message/stream", bytes.NewBufferString(`{"content":"hello"}`))
	sreq.SetPathValue("id", testConvID)
	sw := httptest.NewRecorder()
	streaming.sendMessageStream(sw, sreq)

	if bw.Code != sw.Code {
		t.Errorf("status parity: blocking got %d, streaming got %d — want equal", bw.Code, sw.Code)
	}
	if bw.Code != http.StatusConflict {
		t.Errorf("blocking status: got %d, want 409", bw.Code)
	}
	var blockingBody, streamBody map[string]string
	if err := json.Unmarshal(bw.Body.Bytes(), &blockingBody); err != nil {
		t.Fatalf("blocking body not JSON: %v", err)
	}
	if err := json.Unmarshal(sw.Body.Bytes(), &streamBody); err != nil {
		t.Fatalf("streaming body not JSON: %v", err)
	}
	if blockingBody["error"] != streamBody["error"] {
		t.Errorf("error message parity: blocking %q, streaming %q", blockingBody["error"], streamBody["error"])
	}
}
