package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valpere/llm-council/internal/council"
	"github.com/valpere/llm-council/internal/storage"
)

// ── mocks ────────────────────────────────────────────────────────────────────

type mockStorer struct {
	listConversations          func() ([]storage.ConversationMeta, error)
	createConversation         func() (*storage.Conversation, error)
	getConversation            func(string) (*storage.Conversation, error)
	saveUserMessage            func(string, string) error
	saveAssistantMessage       func(string, council.AssistantMessage) error
	saveTitle                  func(string, string) error
	closeConversation          func(string) error
	saveClarificationRound     func(string, int, []council.ClarificationQuestion, string) error
	updateClarificationAnswers func(string, int, []council.ClarificationAnswer) error
	getLastClarificationRound  func(string) (*council.ClarificationRound, error)
}

func (m *mockStorer) ListConversations() ([]storage.ConversationMeta, error) {
	if m.listConversations != nil {
		return m.listConversations()
	}
	return nil, nil
}
func (m *mockStorer) CreateConversation() (*storage.Conversation, error) {
	if m.createConversation != nil {
		return m.createConversation()
	}
	return &storage.Conversation{ID: testConvID}, nil
}
func (m *mockStorer) GetConversation(id string) (*storage.Conversation, error) {
	if m.getConversation != nil {
		return m.getConversation(id)
	}
	return &storage.Conversation{ID: id}, nil
}
func (m *mockStorer) SaveUserMessage(id, content string) error {
	if m.saveUserMessage != nil {
		return m.saveUserMessage(id, content)
	}
	return nil
}
func (m *mockStorer) SaveAssistantMessage(id string, msg council.AssistantMessage) error {
	if m.saveAssistantMessage != nil {
		return m.saveAssistantMessage(id, msg)
	}
	return nil
}
func (m *mockStorer) SaveTitle(id, title string) error {
	if m.saveTitle != nil {
		return m.saveTitle(id, title)
	}
	return nil
}
func (m *mockStorer) CloseConversation(id string) error {
	if m.closeConversation != nil {
		return m.closeConversation(id)
	}
	return nil
}
func (m *mockStorer) SaveClarificationRound(id string, round int, questions []council.ClarificationQuestion, councilType string) error {
	if m.saveClarificationRound != nil {
		return m.saveClarificationRound(id, round, questions, councilType)
	}
	return nil
}
func (m *mockStorer) UpdateClarificationAnswers(id string, round int, answers []council.ClarificationAnswer) error {
	if m.updateClarificationAnswers != nil {
		return m.updateClarificationAnswers(id, round, answers)
	}
	return nil
}
func (m *mockStorer) GetLastClarificationRound(id string) (*council.ClarificationRound, error) {
	if m.getLastClarificationRound != nil {
		return m.getLastClarificationRound(id)
	}
	return nil, nil
}

type mockRunner struct {
	runFull func(context.Context, string, string, council.EventFunc) error
}

func (m *mockRunner) RunFull(ctx context.Context, query, councilType string, onEvent council.EventFunc) error {
	if m.runFull != nil {
		return m.runFull(ctx, query, councilType, onEvent)
	}
	return nil
}

// mockStage0Runner implements council.Stage0Runner for tests.
type mockStage0Runner struct {
	runClarificationRound     func(context.Context, string, []council.ClarificationRound, council.ClarificationConfig, string, council.EventFunc) error
	runFullWithClarifications func(context.Context, string, []council.ClarificationRound, string, council.EventFunc) error
}

func (m *mockStage0Runner) RunClarificationRound(ctx context.Context, query string, history []council.ClarificationRound, cfg council.ClarificationConfig, councilType string, onEvent council.EventFunc) error {
	if m.runClarificationRound != nil {
		return m.runClarificationRound(ctx, query, history, cfg, councilType, onEvent)
	}
	if onEvent != nil {
		onEvent("stage0_done", nil)
	}
	return nil
}

func (m *mockStage0Runner) RunFullWithClarifications(ctx context.Context, originalQuery string, history []council.ClarificationRound, councilType string, onEvent council.EventFunc) error {
	if m.runFullWithClarifications != nil {
		return m.runFullWithClarifications(ctx, originalQuery, history, councilType, onEvent)
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// testConvID is a canonical UUID v4 used across tests.
const testConvID = "00000000-0000-4000-8000-000000000001"

// noopStage0Runner returns a stage0 runner that always emits stage0_done then
// delegates RunFullWithClarifications to the provided runner.RunFull.
func noopStage0Runner(runner *mockRunner) *mockStage0Runner {
	return &mockStage0Runner{
		runClarificationRound: func(_ context.Context, _ string, _ []council.ClarificationRound, _ council.ClarificationConfig, _ string, onEvent council.EventFunc) error {
			if onEvent != nil {
				onEvent("stage0_done", nil)
			}
			return nil
		},
		runFullWithClarifications: func(ctx context.Context, query string, _ []council.ClarificationRound, councilType string, onEvent council.EventFunc) error {
			return runner.RunFull(ctx, query, councilType, onEvent)
		},
	}
}

// newTestHandler builds a Handler with no-op defaults and a silent logger.
// clarification is disabled (MaxRounds=0) so existing tests are unaffected.
func newTestHandler(storer *mockStorer, runner *mockRunner) *Handler {
	return NewHandler(runner, noopStage0Runner(runner), storer, nil, "standard", council.ClarificationConfig{})
}

// parseSSEEventTypes returns the "type" field from every SSE data line in body.
func parseSSEEventTypes(body string) []string {
	var types []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var env struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line[6:]), &env); err == nil && env.Type != "" {
			types = append(types, env.Type)
		}
	}
	return types
}

//── ListConversations ────────────────────────────────────────────────────────

func TestListConversations(t *testing.T) {
	tests := []struct {
		name     string
		storer   *mockStorer
		wantCode int
		check    func(t *testing.T, body string)
	}{
		{
			name: "happy path",
			storer: &mockStorer{
				listConversations: func() ([]storage.ConversationMeta, error) {
					return []storage.ConversationMeta{{ID: testConvID, Title: "Test"}}, nil
				},
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, body string) {
				var convs []storage.ConversationMeta
				if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &convs); err != nil {
					t.Fatalf("parse body: %v", err)
				}
				if len(convs) != 1 || convs[0].ID != testConvID {
					t.Errorf("body: got %v, want 1 item with ID %q", convs, testConvID)
				}
			},
		},
		{
			name: "empty list returns [] not null",
			storer: &mockStorer{
				listConversations: func() ([]storage.ConversationMeta, error) {
					return nil, nil // storage returns nil slice → handler converts to []
				},
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, body string) {
				trimmed := strings.TrimSpace(body)
				if !strings.HasPrefix(trimmed, "[") {
					t.Errorf("body: got %q, want JSON array (not null)", trimmed)
				}
			},
		},
		{
			name: "storage error returns 500",
			storer: &mockStorer{
				listConversations: func() ([]storage.ConversationMeta, error) {
					return nil, errors.New("disk failure")
				},
			},
			wantCode: http.StatusInternalServerError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(tc.storer, &mockRunner{})
			req := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
			w := httptest.NewRecorder()
			h.listConversations(w, req)
			if w.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantCode)
			}
			if tc.check != nil {
				tc.check(t, w.Body.String())
			}
		})
	}
}

// ── CreateConversation ───────────────────────────────────────────────────────

func TestCreateConversation(t *testing.T) {
	tests := []struct {
		name     string
		storer   *mockStorer
		wantCode int
	}{
		{
			name: "happy path returns 201",
			storer: &mockStorer{
				createConversation: func() (*storage.Conversation, error) {
					return &storage.Conversation{ID: testConvID, Title: "New Conversation"}, nil
				},
			},
			wantCode: http.StatusCreated,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(tc.storer, &mockRunner{})
			req := httptest.NewRequest(http.MethodPost, "/api/conversations", nil)
			w := httptest.NewRecorder()
			h.createConversation(w, req)
			if w.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantCode)
			}
		})
	}
}

// ── GetConversation ──────────────────────────────────────────────────────────

func TestGetConversation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		storer   *mockStorer
		wantCode int
	}{
		{
			name: "200 found",
			id:   testConvID,
			storer: &mockStorer{
				getConversation: func(id string) (*storage.Conversation, error) {
					return &storage.Conversation{ID: id}, nil
				},
			},
			wantCode: http.StatusOK,
		},
		{
			name: "404 not found",
			id:   testConvID,
			storer: &mockStorer{
				getConversation: func(id string) (*storage.Conversation, error) {
					return nil, &storage.NotFoundError{ID: id}
				},
			},
			wantCode: http.StatusNotFound,
		},
		{
			name:     "400 invalid UUID",
			id:       "not-a-uuid",
			storer:   &mockStorer{},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "500 storage error",
			id:   testConvID,
			storer: &mockStorer{
				getConversation: func(id string) (*storage.Conversation, error) {
					return nil, errors.New("db error")
				},
			},
			wantCode: http.StatusInternalServerError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(tc.storer, &mockRunner{})
			req := httptest.NewRequest(http.MethodGet, "/api/conversations/"+tc.id, nil)
			req.SetPathValue("id", tc.id)
			w := httptest.NewRecorder()
			h.getConversation(w, req)
			if w.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantCode)
			}
		})
	}
}

// ── SendMessage (blocking) ───────────────────────────────────────────────────

func TestSendMessage(t *testing.T) {
	successRunner := &mockRunner{
		runFull: func(_ context.Context, query, ct string, onEvent council.EventFunc) error {
			onEvent("stage1_complete", []council.StageOneResult{
				{Label: "Response A", Content: "answer A", Model: "model-a"},
			})
			onEvent("stage2_complete", council.Stage2CompleteData{
				Results: []council.StageTwoResult{
					{ReviewerLabel: "Response A", Rankings: []string{"Response A"}},
				},
				Metadata: council.Metadata{
					CouncilType:  ct,
					ConsensusW:   0.9,
					LabelToModel: map[string]string{"Response A": "model-a"},
				},
			})
			onEvent("stage3_complete", council.StageThreeResult{Content: "synthesized answer", Model: "chairman"})
			return nil
		},
	}

	tests := []struct {
		name      string
		id        string
		body      string
		storer    *mockStorer
		runner    *mockRunner
		wantCode  int
		checkBody func(t *testing.T, body string)
	}{
		{
			name:     "happy path returns 200 AssistantMessage with metadata",
			id:       testConvID,
			body:     `{"content":"why is the sky blue?","council_type":"standard"}`,
			storer:   okStorer(),
			runner:   successRunner,
			wantCode: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				var msg council.AssistantMessage
				if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &msg); err != nil {
					t.Fatalf("parse body: %v", err)
				}
				if msg.Role != "assistant" {
					t.Errorf("Role: got %q, want %q", msg.Role, "assistant")
				}
				if len(msg.Stage1) != 1 {
					t.Errorf("Stage1 len: got %d, want 1", len(msg.Stage1))
				}
				if msg.Stage3.Content != "synthesized answer" {
					t.Errorf("Stage3.Content: got %q, want %q", msg.Stage3.Content, "synthesized answer")
				}
				if msg.Metadata.ConsensusW != 0.9 {
					t.Errorf("Metadata.ConsensusW: got %f, want 0.9", msg.Metadata.ConsensusW)
				}
				if msg.Metadata.LabelToModel["Response A"] != "model-a" {
					t.Errorf("Metadata.LabelToModel: got %v", msg.Metadata.LabelToModel)
				}
			},
		},
		{
			name:   "council_type defaults to handler default when omitted",
			id:     testConvID,
			body:   `{"content":"test query"}`,
			storer: okStorer(),
			runner: &mockRunner{
				runFull: func(_ context.Context, _, ct string, onEvent council.EventFunc) error {
					if ct != "standard" {
						t.Errorf("council_type: got %q, want %q", ct, "standard")
					}
					onEvent("stage3_complete", council.StageThreeResult{Content: "ok"})
					return nil
				},
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "400 invalid UUID",
			id:       "not-a-uuid",
			body:     `{"content":"test"}`,
			storer:   &mockStorer{},
			runner:   &mockRunner{},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "400 missing content",
			id:       testConvID,
			body:     `{"content":""}`,
			storer:   &mockStorer{},
			runner:   &mockRunner{},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "400 malformed JSON body",
			id:       testConvID,
			body:     `not json`,
			storer:   &mockStorer{},
			runner:   &mockRunner{},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "404 conversation not found on save user message",
			id:   testConvID,
			body: `{"content":"test"}`,
			storer: &mockStorer{
				saveUserMessage: func(id, _ string) error {
					return &storage.NotFoundError{ID: id}
				},
			},
			runner:   &mockRunner{},
			wantCode: http.StatusNotFound,
		},
		{
			name: "503 QuorumError from RunFull",
			id:   testConvID,
			body: `{"content":"test"}`,
			storer: okStorer(),
			runner: &mockRunner{
				runFull: func(_ context.Context, _, _ string, _ council.EventFunc) error {
					return &council.QuorumError{Got: 1, Need: 3}
				},
			},
			wantCode: http.StatusServiceUnavailable,
			checkBody: func(t *testing.T, body string) {
				var resp map[string]string
				if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &resp); err != nil {
					t.Fatalf("parse body: %v", err)
				}
				if resp["error"] == "" {
					t.Errorf("error field missing: %v", resp)
				}
			},
		},
		{
			name: "500 generic RunFull error",
			id:   testConvID,
			body: `{"content":"test"}`,
			storer: okStorer(),
			runner: &mockRunner{
				runFull: func(_ context.Context, _, _ string, _ council.EventFunc) error {
					return errors.New("chairman failed")
				},
			},
			wantCode: http.StatusInternalServerError,
		},
		{
			name: "500 on save assistant message failure",
			id:   testConvID,
			body: `{"content":"test"}`,
			storer: &mockStorer{
				saveUserMessage: func(string, string) error { return nil },
				saveAssistantMessage: func(string, council.AssistantMessage) error {
					return errors.New("disk full")
				},
				saveTitle: func(string, string) error { return nil },
			},
			runner:   successRunner,
			wantCode: http.StatusInternalServerError,
		},
		{
			name: "title truncated to 50 chars",
			id:   testConvID,
			body: `{"content":"test"}`,
			storer: &mockStorer{
				saveUserMessage:      func(string, string) error { return nil },
				saveAssistantMessage: func(string, council.AssistantMessage) error { return nil },
				saveTitle: func(_ string, title string) error {
					if len(title) > 50 {
						t.Errorf("title length: got %d, want ≤50", len(title))
					}
					return nil
				},
			},
			runner: &mockRunner{
				runFull: func(_ context.Context, _, _ string, onEvent council.EventFunc) error {
					onEvent("stage3_complete", council.StageThreeResult{
						Content: strings.Repeat("x", 100),
					})
					return nil
				},
			},
			wantCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(tc.storer, tc.runner)
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/conversations/"+tc.id+"/message",
				bytes.NewBufferString(tc.body),
			)
			req.SetPathValue("id", tc.id)
			w := httptest.NewRecorder()
			h.sendMessage(w, req)
			if w.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d\nbody: %s", w.Code, tc.wantCode, w.Body.String())
			}
			if tc.checkBody != nil {
				tc.checkBody(t, w.Body.String())
			}
		})
	}
}

// ── SendMessageStream ────────────────────────────────────────────────────────

// okStorer returns a mockStorer that succeeds silently for all write operations.
func okStorer() *mockStorer {
	return &mockStorer{
		saveUserMessage:      func(string, string) error { return nil },
		saveAssistantMessage: func(string, council.AssistantMessage) error { return nil },
		saveTitle:            func(string, string) error { return nil },
	}
}

func TestSendMessageStream(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		storer   *mockStorer
		runner   *mockRunner
		wantCode int
		checkSSE func(t *testing.T, body string)
	}{
		{
			name:   "event sequence with metadata in stage2_complete",
			body:   `{"content":"what is Go?","council_type":"standard"}`,
			storer: okStorer(),
			runner: &mockRunner{
				runFull: func(ctx context.Context, query, ct string, onEvent council.EventFunc) error {
					onEvent("stage1_complete", []council.StageOneResult{
						{Label: "Response A", Content: "Go is a compiled language"},
					})
					onEvent("stage2_complete", council.Stage2CompleteData{
						Kind: "peer_ranking",
						Results: []council.StageTwoResult{
							{ReviewerLabel: "Response A", Rankings: []string{"Response A"}},
						},
						Metadata: council.Metadata{
							CouncilType:  "standard",
							ConsensusW:   0.9,
							LabelToModel: map[string]string{"Response A": "openai/gpt-4o"},
						},
					})
					onEvent("stage3_complete", council.StageThreeResult{Content: "final answer"})
					return nil
				},
			},
			wantCode: http.StatusOK,
			checkSSE: func(t *testing.T, body string) {
				types := parseSSEEventTypes(body)

				// Verify required events are present.
				for _, want := range []string{"stage1_complete", "stage2_complete", "stage3_complete", "complete"} {
					found := false
					for _, got := range types {
						if got == want {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("missing event %q in sequence %v", want, types)
					}
				}

				// "complete" must be the last event.
				if len(types) == 0 || types[len(types)-1] != "complete" {
					t.Errorf("last event: got %v, want 'complete'", types)
				}

				// stage2_complete must have kind + metadata as TOP-LEVEL fields per the
				// streaming spec: { "type": "stage2_complete", "kind": "...", "data": [...], "metadata": {...}, "round"?: N }
				for _, line := range strings.Split(body, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					raw := []byte(line[6:])
					// First-pass: typed unmarshal for value assertions.
					var env struct {
						Type     string                   `json:"type"`
						Kind     string                   `json:"kind"`
						Data     []council.StageTwoResult `json:"data"`
						Metadata council.Metadata         `json:"metadata"`
					}
					if err := json.Unmarshal(raw, &env); err != nil || env.Type != "stage2_complete" {
						continue
					}
					if env.Kind != "peer_ranking" {
						t.Errorf("kind: got %q, want %q", env.Kind, "peer_ranking")
					}
					if env.Metadata.ConsensusW != 0.9 {
						t.Errorf("consensus_w: got %f, want 0.9", env.Metadata.ConsensusW)
					}
					if env.Metadata.LabelToModel["Response A"] != "openai/gpt-4o" {
						t.Errorf("label_to_model: got %v", env.Metadata.LabelToModel)
					}
					// Second-pass: raw key inspection — `round` must be ABSENT when zero
					// (omitempty), not present-but-zero. A typed int unmarshal cannot
					// distinguish absence from explicit zero, so check the key directly.
					var keys map[string]json.RawMessage
					if err := json.Unmarshal(raw, &keys); err != nil {
						t.Fatalf("re-unmarshal as map: %v", err)
					}
					if _, present := keys["round"]; present {
						t.Errorf("round: must be omitted when zero (omitempty), but key was present in JSON: %s", string(raw))
					}
					break
				}
			},
		},
		{
			name:   "Majority strategy emits kind=vote_tally on the wire",
			body:   `{"content":"q","council_type":"majority"}`,
			storer: okStorer(),
			runner: &mockRunner{
				runFull: func(ctx context.Context, query, ct string, onEvent council.EventFunc) error {
					onEvent("stage1_complete", []council.StageOneResult{
						{Label: "Response A", Content: "yes", Model: "openai/gpt-4o-mini"},
						{Label: "Response B", Content: "yes", Model: "anthropic/claude-haiku-4-5"},
						{Label: "Response C", Content: "no", Model: "google/gemini-flash-1.5"},
					})
					tally := &council.VoteTally{
						Clusters: []council.VoteCluster{
							{Members: []string{"Response A", "Response B"}, Representative: "yes", Votes: 2},
							{Members: []string{"Response C"}, Representative: "no", Votes: 1},
						},
						WinnerLabel: "Response A",
					}
					onEvent("stage2_complete", council.Stage2CompleteData{
						Kind:    "vote_tally",
						Results: []council.StageTwoResult{},
						Metadata: council.Metadata{
							CouncilType:       "majority",
							LabelToModel:      map[string]string{"Response A": "openai/gpt-4o-mini"},
							AggregateRankings: []council.RankedModel{},
							ConsensusW:        1.0,
							VoteTally:         tally,
						},
					})
					onEvent("stage3_complete", council.StageThreeResult{Content: "yes"}) // no LLM call → empty Model
					return nil
				},
			},
			wantCode: http.StatusOK,
			checkSSE: func(t *testing.T, body string) {
				for _, line := range strings.Split(body, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var env struct {
						Type     string           `json:"type"`
						Kind     string           `json:"kind"`
						Metadata council.Metadata `json:"metadata"`
					}
					if err := json.Unmarshal([]byte(line[6:]), &env); err != nil || env.Type != "stage2_complete" {
						continue
					}
					if env.Kind != "vote_tally" {
						t.Errorf("kind: got %q, want %q", env.Kind, "vote_tally")
					}
					if env.Metadata.VoteTally == nil {
						t.Fatalf("metadata.vote_tally: nil; expected populated")
					}
					if env.Metadata.VoteTally.WinnerLabel != "Response A" {
						t.Errorf("winner_label: got %q, want %q", env.Metadata.VoteTally.WinnerLabel, "Response A")
					}
					if len(env.Metadata.VoteTally.Clusters) != 2 {
						t.Errorf("clusters: got %d, want 2", len(env.Metadata.VoteTally.Clusters))
					}
					return
				}
				t.Errorf("no stage2_complete event found in body: %s", body)
			},
		},
		{
			name:   "GenerateRankRefine strategy emits kind=rank_refine on the wire",
			body:   `{"content":"q","council_type":"generate-rank-refine"}`,
			storer: okStorer(),
			runner: &mockRunner{
				runFull: func(ctx context.Context, query, ct string, onEvent council.EventFunc) error {
					onEvent("stage1_complete", []council.StageOneResult{
						{Label: "Response A", Content: "answer a", Model: "openai/gpt-4o-mini"},
						{Label: "Response B", Content: "answer b", Model: "anthropic/claude-haiku-4-5"},
						{Label: "Response C", Content: "answer c", Model: "google/gemini-flash-1.5"},
						{Label: "Response D", Content: "answer d", Model: "qwen/qwen3.6-plus"},
					})
					tally := &council.RankRefine{
						TopK:     3,
						Criteria: []string{"correctness", "clarity", "completeness", "originality"},
						Rankings: []council.RankedCandidate{
							{Label: "Response A", Scores: map[string]float64{"correctness": 0.9, "clarity": 0.9, "completeness": 0.9, "originality": 0.9}, TotalScore: 3.6, Advancing: true},
							{Label: "Response B", Scores: map[string]float64{"correctness": 0.7, "clarity": 0.7, "completeness": 0.7, "originality": 0.7}, TotalScore: 2.8, Advancing: true},
							{Label: "Response C", Scores: map[string]float64{"correctness": 0.5, "clarity": 0.5, "completeness": 0.5, "originality": 0.5}, TotalScore: 2.0, Advancing: true},
							{Label: "Response D", Scores: map[string]float64{"correctness": 0.3, "clarity": 0.3, "completeness": 0.3, "originality": 0.3}, TotalScore: 1.2, Advancing: false},
						},
					}
					onEvent("stage2_complete", council.Stage2CompleteData{
						Kind:    "rank_refine",
						Results: []council.StageTwoResult{},
						Metadata: council.Metadata{
							CouncilType:       "generate-rank-refine",
							LabelToModel:      map[string]string{"Response A": "openai/gpt-4o-mini"},
							AggregateRankings: []council.RankedModel{},
							ConsensusW:        0.625, // 2.5 / 4
							RankRefine:        tally,
						},
					})
					onEvent("stage3_complete", council.StageThreeResult{Content: "refined", Model: "chairman-z", DurationMs: 100})
					return nil
				},
			},
			wantCode: http.StatusOK,
			checkSSE: func(t *testing.T, body string) {
				for _, line := range strings.Split(body, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var env struct {
						Type     string           `json:"type"`
						Kind     string           `json:"kind"`
						Metadata council.Metadata `json:"metadata"`
					}
					if err := json.Unmarshal([]byte(line[6:]), &env); err != nil || env.Type != "stage2_complete" {
						continue
					}
					if env.Kind != "rank_refine" {
						t.Errorf("kind: got %q, want %q", env.Kind, "rank_refine")
					}
					if env.Metadata.RankRefine == nil {
						t.Fatalf("metadata.rank_refine: nil; expected populated")
					}
					if env.Metadata.RankRefine.TopK != 3 {
						t.Errorf("top_k: got %d, want 3", env.Metadata.RankRefine.TopK)
					}
					if len(env.Metadata.RankRefine.Rankings) != 4 {
						t.Errorf("rankings: got %d, want 4", len(env.Metadata.RankRefine.Rankings))
					}
					if len(env.Metadata.RankRefine.Criteria) != 4 {
						t.Errorf("criteria: got %d, want 4", len(env.Metadata.RankRefine.Criteria))
					}
					advancing := 0
					for _, r := range env.Metadata.RankRefine.Rankings {
						if r.Advancing {
							advancing++
						}
					}
					if advancing != 3 {
						t.Errorf("advancing count: got %d, want 3", advancing)
					}
					return
				}
				t.Errorf("no stage2_complete event found in body: %s", body)
			},
		},
		{
			name:   "MultiAgentDebate emits stage2_round_complete events with required round",
			body:   `{"content":"q","council_type":"debate"}`,
			storer: okStorer(),
			runner: &mockRunner{
				runFull: func(ctx context.Context, query, ct string, onEvent council.EventFunc) error {
					onEvent("stage1_complete", []council.StageOneResult{
						{Label: "Response A", Content: "ans-a", Model: "openai/gpt-4o-mini"},
						{Label: "Response B", Content: "ans-b", Model: "anthropic/claude-haiku-4-5"},
						{Label: "Response C", Content: "ans-c", Model: "google/gemini-flash-1.5"},
					})
					// Round 1 — per-round event with required `round` field.
					onEvent("stage2_round_complete", council.Stage2CompleteData{
						Kind:    "debate_round",
						Round:   1,
						Results: []council.StageTwoResult{},
						Metadata: council.Metadata{
							CouncilType:       "debate",
							LabelToModel:      map[string]string{"Response A": "openai/gpt-4o-mini"},
							AggregateRankings: []council.RankedModel{},
							Debate: &council.Debate{
								Rounds: []council.DebateRound{{Round: 1, Revisions: []council.DebaterRevision{
									{Label: "Response A", Critique: "c", Content: "rev-a-1"},
								}}},
								FinalRound: 1,
							},
						},
					})
					// Terminal event — canonical transcript with both rounds.
					onEvent("stage2_complete", council.Stage2CompleteData{
						Kind:    "debate_round",
						Results: []council.StageTwoResult{},
						Metadata: council.Metadata{
							CouncilType:       "debate",
							LabelToModel:      map[string]string{"Response A": "openai/gpt-4o-mini"},
							AggregateRankings: []council.RankedModel{},
							Debate: &council.Debate{
								Rounds: []council.DebateRound{
									{Round: 1, Revisions: []council.DebaterRevision{{Label: "Response A", Content: "rev-a-1"}}},
									{Round: 2, Revisions: []council.DebaterRevision{{Label: "Response A", Content: "rev-a-2"}}},
								},
								FinalRound: 2,
							},
						},
					})
					onEvent("stage3_complete", council.StageThreeResult{Content: "synthesis", Model: "chairman-z", DurationMs: 100})
					return nil
				},
			},
			wantCode: http.StatusOK,
			checkSSE: func(t *testing.T, body string) {
				// 1. A stage2_round_complete event MUST appear with `round: 1`
				//    present on the wire (not omitempty).
				// 2. The terminal stage2_complete event MUST carry the full
				//    transcript with metadata.debate populated.
				var sawRoundEvent bool
				var sawTerminalDebate bool
				for _, line := range strings.Split(body, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					raw := []byte(line[6:])

					// Detect round events by Type+Round-key presence in raw JSON.
					var keys map[string]json.RawMessage
					if err := json.Unmarshal(raw, &keys); err != nil {
						continue
					}
					var typ string
					if err := json.Unmarshal(keys["type"], &typ); err == nil && typ == "stage2_round_complete" {
						sawRoundEvent = true
						roundRaw, present := keys["round"]
						if !present {
							t.Errorf("stage2_round_complete: 'round' key missing on the wire (must be required, not omitempty)")
						}
						var roundVal int
						if err := json.Unmarshal(roundRaw, &roundVal); err != nil || roundVal != 1 {
							t.Errorf("stage2_round_complete round: got %s, want 1", string(roundRaw))
						}
						var kind string
						_ = json.Unmarshal(keys["kind"], &kind)
						if kind != "debate_round" {
							t.Errorf("stage2_round_complete kind: got %q, want %q", kind, "debate_round")
						}
					}

					// Detect the terminal event with full transcript.
					if err := json.Unmarshal(keys["type"], &typ); err == nil && typ == "stage2_complete" {
						var env struct {
							Kind     string           `json:"kind"`
							Metadata council.Metadata `json:"metadata"`
						}
						if err := json.Unmarshal(raw, &env); err != nil {
							continue
						}
						if env.Kind != "debate_round" {
							t.Errorf("terminal kind: got %q, want %q", env.Kind, "debate_round")
						}
						if env.Metadata.Debate == nil {
							t.Errorf("terminal stage2_complete: metadata.debate is nil")
							continue
						}
						if env.Metadata.Debate.FinalRound != 2 {
							t.Errorf("FinalRound: got %d, want 2", env.Metadata.Debate.FinalRound)
						}
						if len(env.Metadata.Debate.Rounds) != 2 {
							t.Errorf("transcript rounds: got %d, want 2", len(env.Metadata.Debate.Rounds))
						}
						sawTerminalDebate = true
					}
				}
				if !sawRoundEvent {
					t.Error("stage2_round_complete event not seen on the wire")
				}
				if !sawTerminalDebate {
					t.Error("terminal stage2_complete with debate transcript not seen on the wire")
				}
			},
		},
		{
			name:   "QuorumError emits error event",
			body:   `{"content":"test","council_type":"standard"}`,
			storer: okStorer(),
			runner: &mockRunner{
				runFull: func(ctx context.Context, query, ct string, onEvent council.EventFunc) error {
					return &council.QuorumError{Got: 1, Need: 3}
				},
			},
			wantCode: http.StatusOK,
			checkSSE: func(t *testing.T, body string) {
				// Error event must be present with a non-empty "message" field
				// per the SSE spec: { "type": "error", "message": "..." }
				for _, line := range strings.Split(body, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var env struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					}
					if err := json.Unmarshal([]byte(line[6:]), &env); err != nil || env.Type != "error" {
						continue
					}
					if env.Message == "" {
						t.Errorf("error event missing 'message' field, got: %s", line[6:])
					}
					return
				}
				t.Errorf("expected 'error' event for QuorumError, got:\n%s", body)
			},
		},
		{
			name:     "malformed JSON body returns 400 before SSE starts",
			body:     `not json`,
			storer:   okStorer(),
			runner:   &mockRunner{},
			wantCode: http.StatusBadRequest,
			checkSSE: func(t *testing.T, body string) {
				// Must be a plain JSON error, not an SSE stream.
				var errResp map[string]string
				if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &errResp); err != nil {
					t.Errorf("expected JSON error body, got: %q", body)
				}
				if errResp["error"] == "" {
					t.Errorf("error field missing in response: %v", errResp)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(tc.storer, tc.runner)
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/conversations/"+testConvID+"/message/stream",
				bytes.NewBufferString(tc.body),
			)
			req.SetPathValue("id", testConvID)
			w := httptest.NewRecorder()
			h.sendMessageStream(w, req)
			if w.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d\nbody: %s", w.Code, tc.wantCode, w.Body.String())
			}
			if tc.checkSSE != nil {
				tc.checkSSE(t, w.Body.String())
			}
		})
	}
}
