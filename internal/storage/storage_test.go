package storage_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/valpere/vmm-rada/internal/council"
	"github.com/valpere/vmm-rada/internal/storage"
)

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.NewStore(t.TempDir(), slog.Default())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestCreateGetRoundTrip(t *testing.T) {
	s := newTestStore(t)

	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if c.Title != "New Conversation" {
		t.Errorf("Title: got %q, want %q", c.Title, "New Conversation")
	}

	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.ID != c.ID {
		t.Errorf("ID: got %q, want %q", got.ID, c.ID)
	}
	if !got.CreatedAt.Equal(c.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, c.CreatedAt)
	}
}

func TestListNewestFirst(t *testing.T) {
	s := newTestStore(t)

	c1, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation c1: %v", err)
	}
	time.Sleep(2 * time.Millisecond) // ensure distinct timestamps
	c2, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation c2: %v", err)
	}

	list, err := s.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(list))
	}
	if list[0].ID != c2.ID {
		t.Errorf("expected newest first: got %q, want %q", list[0].ID, c2.ID)
	}
	if list[1].ID != c1.ID {
		t.Errorf("expected oldest last: got %q, want %q", list[1].ID, c1.ID)
	}
}

func TestSaveAssistantMessageRoundTrip(t *testing.T) {
	s := newTestStore(t)

	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	msg := council.AssistantMessage{
		Role: "assistant",
		Stage3: council.StageThreeResult{
			Content:    "synthesised answer",
			Model:      "openai/gpt-4o",
			DurationMs: 1234,
		},
		Metadata: council.Metadata{
			CouncilType: "default",
			LabelToModel: map[string]string{
				"Response A": "openai/gpt-4o",
				"Response B": "anthropic/claude-haiku-4-5",
			},
			AggregateRankings: []council.RankedModel{
				{Model: "openai/gpt-4o", Score: 1.5},
				{Model: "anthropic/claude-haiku-4-5", Score: 2.5},
			},
			ConsensusW: 0.72,
		},
	}

	if err := s.SaveAssistantMessage(c.ID, msg); err != nil {
		t.Fatalf("SaveAssistantMessage: %v", err)
	}

	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}

	var gotMsg council.AssistantMessage
	if err := json.Unmarshal(got.Messages[0], &gotMsg); err != nil {
		t.Fatalf("unmarshal assistant message: %v", err)
	}

	if gotMsg.Metadata.ConsensusW != 0.72 {
		t.Errorf("ConsensusW: got %v, want 0.72", gotMsg.Metadata.ConsensusW)
	}
	if gotMsg.Metadata.CouncilType != "default" {
		t.Errorf("CouncilType: got %q, want %q", gotMsg.Metadata.CouncilType, "default")
	}
	if len(gotMsg.Metadata.AggregateRankings) != 2 {
		t.Errorf("AggregateRankings len: got %d, want 2", len(gotMsg.Metadata.AggregateRankings))
	}
	if gotMsg.Stage3.DurationMs != 1234 {
		t.Errorf("Stage3.DurationMs: got %d, want 1234", gotMsg.Stage3.DurationMs)
	}
}

func TestSaveUserMessageRoundTrip(t *testing.T) {
	s := newTestStore(t)

	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	if err := s.SaveUserMessage(c.ID, "Why is the sky blue?"); err != nil {
		t.Fatalf("SaveUserMessage: %v", err)
	}

	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}

	var m struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(got.Messages[0], &m); err != nil {
		t.Fatalf("unmarshal user message: %v", err)
	}
	if m.Role != "user" {
		t.Errorf("role: got %q, want %q", m.Role, "user")
	}
	if m.Content != "Why is the sky blue?" {
		t.Errorf("content: got %q, want %q", m.Content, "Why is the sky blue?")
	}
}

func TestSaveTitleRoundTrip(t *testing.T) {
	s := newTestStore(t)

	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	if err := s.SaveTitle(c.ID, "Why is the sky blue?"); err != nil {
		t.Fatalf("SaveTitle: %v", err)
	}

	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.Title != "Why is the sky blue?" {
		t.Errorf("Title: got %q, want %q", got.Title, "Why is the sky blue?")
	}
}

func TestMissingMetadataUnmarshalsToZero(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.NewStore(dir, slog.Default())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	// Overwrite the file with a legacy message that has no metadata field.
	type legacyConv struct {
		ID        string            `json:"id"`
		CreatedAt time.Time         `json:"created_at"`
		Messages  []json.RawMessage `json:"messages"`
	}
	legacyMsg := json.RawMessage(`{"role":"assistant","stage1":[],"stage2":[],"stage3":{"content":"old","model":"gpt-3","duration_ms":0}}`)
	lc := legacyConv{
		ID:        c.ID,
		CreatedAt: c.CreatedAt,
		Messages:  []json.RawMessage{legacyMsg},
	}
	data, _ := json.Marshal(lc)
	if err := os.WriteFile(filepath.Join(dir, c.ID+".json"), data, 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}

	var gotMsg council.AssistantMessage
	if err := json.Unmarshal(got.Messages[0], &gotMsg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if gotMsg.Metadata.ConsensusW != 0 {
		t.Errorf("ConsensusW: got %v, want 0", gotMsg.Metadata.ConsensusW)
	}
	if gotMsg.Metadata.CouncilType != "" {
		t.Errorf("CouncilType: got %q, want empty", gotMsg.Metadata.CouncilType)
	}
	if gotMsg.Metadata.LabelToModel != nil {
		t.Errorf("LabelToModel: got %v, want nil", gotMsg.Metadata.LabelToModel)
	}
}

func TestNotFoundError(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetConversation("00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var nfe *storage.NotFoundError
	if !errors.As(err, &nfe) {
		t.Errorf("expected *storage.NotFoundError, got %T: %v", err, err)
	}
	if nfe.ID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("NotFoundError.ID: got %q", nfe.ID)
	}
}

func TestInvalidUUIDReturnNotFound(t *testing.T) {
	s := newTestStore(t)

	for _, id := range []string{"../etc/passwd", "not-a-uuid", "", "../../secret"} {
		_, err := s.GetConversation(id)
		var nfe *storage.NotFoundError
		if !errors.As(err, &nfe) {
			t.Errorf("id %q: expected *storage.NotFoundError, got %T: %v", id, err, err)
		}
	}
}

// ── Clarification storage ─────────────────────────────────────────────────────

func TestSaveClarificationRound(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	questions := []council.ClarificationQuestion{
		{ID: "q1", Text: "What is the target audience?"},
		{ID: "q2", Text: "What is the time constraint?"},
	}
	if err := s.SaveClarificationRound(c.ID, 1, questions, "default"); err != nil {
		t.Fatalf("SaveClarificationRound: %v", err)
	}

	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}

	var raw struct {
		Role        string                          `json:"role"`
		Round       int                             `json:"round"`
		Questions   []council.ClarificationQuestion `json:"questions"`
		CouncilType string                          `json:"council_type"`
	}
	if err := json.Unmarshal(got.Messages[0], &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw.Role != "clarification" {
		t.Errorf("Role: got %q, want %q", raw.Role, "clarification")
	}
	if raw.Round != 1 {
		t.Errorf("Round: got %d, want 1", raw.Round)
	}
	if len(raw.Questions) != 2 {
		t.Errorf("Questions len: got %d, want 2", len(raw.Questions))
	}
	if raw.CouncilType != "default" {
		t.Errorf("CouncilType: got %q, want %q", raw.CouncilType, "default")
	}
}

func TestUpdateClarificationAnswers(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	questions := []council.ClarificationQuestion{
		{ID: "q1", Text: "What format?"},
	}
	if err := s.SaveClarificationRound(c.ID, 1, questions, "default"); err != nil {
		t.Fatalf("SaveClarificationRound: %v", err)
	}

	answers := []council.ClarificationAnswer{
		{ID: "q1", Text: "Markdown"},
	}
	if err := s.UpdateClarificationAnswers(c.ID, 1, answers); err != nil {
		t.Fatalf("UpdateClarificationAnswers: %v", err)
	}

	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	var raw struct {
		Answers []council.ClarificationAnswer `json:"answers"`
	}
	if err := json.Unmarshal(got.Messages[0], &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(raw.Answers) != 1 || raw.Answers[0].Text != "Markdown" {
		t.Errorf("Answers: got %v, want [{q1 Markdown}]", raw.Answers)
	}
}

func TestGetLastClarificationRound_NoRound(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	got, err := s.GetLastClarificationRound(c.ID)
	if err != nil {
		t.Fatalf("GetLastClarificationRound: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil when no clarification round, got %+v", got)
	}
}

func TestGetLastClarificationRound_ReturnsLast(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	// Save two rounds.
	if err := s.SaveClarificationRound(c.ID, 1, []council.ClarificationQuestion{{ID: "q1", Text: "First?"}}, "default"); err != nil {
		t.Fatalf("SaveClarificationRound round 1: %v", err)
	}
	if err := s.SaveClarificationRound(c.ID, 2, []council.ClarificationQuestion{{ID: "q2", Text: "Second?"}}, "default"); err != nil {
		t.Fatalf("SaveClarificationRound round 2: %v", err)
	}

	got, err := s.GetLastClarificationRound(c.ID)
	if err != nil {
		t.Fatalf("GetLastClarificationRound: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil round, got nil")
	}
	if got.Round != 2 {
		t.Errorf("Round: got %d, want 2", got.Round)
	}
	if len(got.Questions) != 1 || got.Questions[0].ID != "q2" {
		t.Errorf("Questions: got %v, want [{q2 Second?}]", got.Questions)
	}
}

func TestStore_SaveUserMessage_RejectsAfterClose(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if err := s.SaveUserMessage(c.ID, "hello"); err != nil {
		t.Fatalf("SaveUserMessage: %v", err)
	}
	if err := s.CloseConversation(c.ID); err != nil {
		t.Fatalf("CloseConversation: %v", err)
	}
	err = s.SaveUserMessage(c.ID, "should fail")
	if !errors.Is(err, storage.ErrConversationClosed) {
		t.Errorf("expected ErrConversationClosed, got %v", err)
	}
	// Verify the closed flag is persisted.
	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if !got.Closed {
		t.Error("expected Closed=true after CloseConversation")
	}
}

func TestStore_CloseConversation_RaceWithSaveUserMessage(t *testing.T) {
	s := newTestStore(t)
	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	const goroutines = 8
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines + 1)

	// N goroutines try to save user messages concurrently.
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			errs[i] = s.SaveUserMessage(c.ID, "msg")
		}(i)
	}
	// One goroutine closes the conversation.
	go func() {
		defer wg.Done()
		if err := s.CloseConversation(c.ID); err != nil {
			t.Errorf("CloseConversation: %v", err)
		}
	}()
	wg.Wait()

	// Every error must be nil (saved before close) or ErrConversationClosed.
	for i, err := range errs {
		if err != nil && !errors.Is(err, storage.ErrConversationClosed) {
			t.Errorf("goroutine %d: unexpected error %v", i, err)
		}
	}

	// Count successful saves.
	saved := 0
	for _, err := range errs {
		if err == nil {
			saved++
		}
	}

	// File must parse as valid JSON with a consistent message count.
	got, err := s.GetConversation(c.ID)
	if err != nil {
		t.Fatalf("GetConversation after race: %v", err)
	}
	if len(got.Messages) != saved {
		t.Errorf("message count mismatch: file has %d, expected %d (successful saves)", len(got.Messages), saved)
	}
	if !got.Closed {
		t.Error("expected Closed=true after CloseConversation")
	}
}

func TestCorruptFileSkippedInList(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.NewStore(dir, slog.Default())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	c, err := s.CreateConversation()
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	// Plant a corrupt file with a valid UUID name so it passes the UUID filter
	// and exercises the JSON-parse-error path in ListConversations.
	corruptID := "ffffffff-ffff-4fff-bfff-ffffffffffff"
	if err := os.WriteFile(filepath.Join(dir, corruptID+".json"), []byte("{not valid json{{"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	list, err := s.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 conversation (corrupt skipped), got %d", len(list))
	}
	if len(list) > 0 && list[0].ID != c.ID {
		t.Errorf("ID: got %q, want %q", list[0].ID, c.ID)
	}
}

func TestDeleteConversation(t *testing.T) {
	dir := t.TempDir()
	s, _ := storage.NewStore(dir, slog.Default())

	t.Run("happy path removes file", func(t *testing.T) {
		c, err := s.CreateConversation()
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := s.DeleteConversation(c.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := s.GetConversation(c.ID); err == nil {
			t.Error("expected not-found after delete")
		}
	})

	t.Run("not found returns NotFoundError", func(t *testing.T) {
		err := s.DeleteConversation("00000000-0000-4000-8000-000000000099")
		var nfe *storage.NotFoundError
		if !errors.As(err, &nfe) {
			t.Errorf("expected NotFoundError, got %v", err)
		}
	})

	t.Run("invalid UUID returns NotFoundError", func(t *testing.T) {
		err := s.DeleteConversation("not-a-uuid")
		var nfe *storage.NotFoundError
		if !errors.As(err, &nfe) {
			t.Errorf("expected NotFoundError for invalid UUID, got %v", err)
		}
	})
}
