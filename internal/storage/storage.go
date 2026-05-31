package storage

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/valpere/vmm-rada/internal/council"
)

// NotFoundError is returned when a requested conversation does not exist.
type NotFoundError struct {
	ID string
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("conversation not found: %s", e.ID)
}

// ErrConversationClosed is returned when a message is sent to a closed conversation.
var ErrConversationClosed = errors.New("conversation is closed")

// ConversationMeta holds lightweight metadata for list responses.
type ConversationMeta struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	Title        string    `json:"title"`
	MessageCount int       `json:"message_count"`
	Closed       bool      `json:"closed"`
}

// Conversation is the full stored record including the message history.
// Messages is []json.RawMessage so the heterogeneous user/assistant array
// survives round-trips without losing type information; callers demux by
// inspecting the "role" field of each element.
type Conversation struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	Title     string            `json:"title"`
	Closed    bool              `json:"closed"`
	Messages  []json.RawMessage `json:"messages"`
}

// Storer is the persistence interface. The handler depends only on this
// interface — never on a concrete implementation.
type Storer interface {
	CreateConversation() (*Conversation, error)
	GetConversation(id string) (*Conversation, error)
	ListConversations() ([]ConversationMeta, error)
	SaveUserMessage(id, content string) error
	SaveAssistantMessage(id string, msg council.AssistantMessage) error
	SaveTitle(id, title string) error
	DeleteConversation(id string) error
	CloseConversation(id string) error
	SaveClarificationRound(id string, round int, questions []council.ClarificationQuestion, councilType string) error
	UpdateClarificationAnswers(id string, round int, answers []council.ClarificationAnswer) error
	GetLastClarificationRound(id string) (*council.ClarificationRound, error)
}

// Store is the JSON file backend. One file per conversation under dataDir.
// A store-level RWMutex serialises write operations (create/save) while
// allowing concurrent reads.
type Store struct {
	dataDir string
	logger  *slog.Logger
	mu      sync.RWMutex
}

// uuidRE matches a canonical UUID v4.
var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func isValidUUID(id string) bool { return uuidRE.MatchString(id) }

// NewStore creates the data directory (mode 0700) if needed and returns a Store.
// A nil logger falls back to slog.Default().
func NewStore(dataDir string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &Store{dataDir: dataDir, logger: logger}, nil
}

// Compile-time assertion: Store implements Storer.
var _ Storer = (*Store)(nil)

func (s *Store) filePath(id string) string {
	return filepath.Join(s.dataDir, id+".json")
}

// readConversation reads and unmarshals a conversation file.
// Caller must hold at least s.mu.RLock().
func (s *Store) readConversation(id string) (*Conversation, error) {
	if !isValidUUID(id) {
		return nil, &NotFoundError{ID: id}
	}
	data, err := os.ReadFile(s.filePath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &NotFoundError{ID: id}
		}
		return nil, fmt.Errorf("read %s: %w", id, err)
	}
	var c Conversation
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", id, err)
	}
	return &c, nil
}

// writeConversation marshals c and atomically replaces the conversation file
// using a tmp → rename pattern. Files are written with mode 0600.
// Caller must hold s.mu.Lock().
func (s *Store) writeConversation(c *Conversation) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}
	tmp := s.filePath(c.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write tmp %s: %w", c.ID, err)
	}
	if err := os.Rename(tmp, s.filePath(c.ID)); err != nil {
		os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("rename %s: %w", c.ID, err)
	}
	return nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func (s *Store) CreateConversation() (*Conversation, error) {
	id, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("generate id: %w", err)
	}
	c := &Conversation{
		ID:        id,
		CreatedAt: time.Now().UTC(),
		Title:     "New Conversation",
		Messages:  []json.RawMessage{},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeConversation(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) GetConversation(id string) (*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readConversation(id)
}

func (s *Store) ListConversations() ([]ConversationMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	var metas []ConversationMeta
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5] // strip .json
		if !isValidUUID(id) {
			continue // skip non-conversation files (e.g. corrupt.json planted in tests)
		}
		data, err := os.ReadFile(filepath.Join(s.dataDir, e.Name()))
		if err != nil {
			s.logger.Warn("skipping unreadable conversation file", "file", e.Name(), "error", err)
			continue
		}
		var c Conversation
		if err := json.Unmarshal(data, &c); err != nil {
			s.logger.Warn("skipping corrupt conversation file", "file", e.Name(), "error", err)
			continue
		}
		metas = append(metas, ConversationMeta{
			ID:           id,
			CreatedAt:    c.CreatedAt,
			Title:        c.Title,
			MessageCount: len(c.Messages),
			Closed:       c.Closed,
		})
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].CreatedAt.After(metas[j].CreatedAt)
	})
	return metas, nil
}

func (s *Store) SaveUserMessage(id, content string) error {
	raw, err := json.Marshal(struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: content})
	if err != nil {
		return fmt.Errorf("marshal user message: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readConversation(id)
	if err != nil {
		return err
	}
	if c.Closed {
		return ErrConversationClosed
	}
	c.Messages = append(c.Messages, raw)
	return s.writeConversation(c)
}

func (s *Store) CloseConversation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readConversation(id)
	if err != nil {
		return err
	}
	c.Closed = true
	return s.writeConversation(c)
}

func (s *Store) SaveAssistantMessage(id string, msg council.AssistantMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal assistant message: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readConversation(id)
	if err != nil {
		return err
	}
	c.Messages = append(c.Messages, raw)
	return s.writeConversation(c)
}

// clarificationMessage is the on-disk shape of a Stage 0 clarification round.
// It is stored as a message in the conversation's Messages array alongside
// user and assistant messages.
type clarificationMessage struct {
	Role        string                          `json:"role"` // always "clarification"
	Round       int                             `json:"round"`
	Questions   []council.ClarificationQuestion `json:"questions"`
	Answers     []council.ClarificationAnswer   `json:"answers"`
	CouncilType string                          `json:"council_type,omitempty"`
}

func (s *Store) SaveClarificationRound(id string, round int, questions []council.ClarificationQuestion, councilType string) error {
	raw, err := json.Marshal(clarificationMessage{
		Role:        "clarification",
		Round:       round,
		Questions:   questions,
		CouncilType: councilType,
	})
	if err != nil {
		return fmt.Errorf("marshal clarification round: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readConversation(id)
	if err != nil {
		return err
	}
	c.Messages = append(c.Messages, raw)
	return s.writeConversation(c)
}

func (s *Store) UpdateClarificationAnswers(id string, round int, answers []council.ClarificationAnswer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readConversation(id)
	if err != nil {
		return err
	}
	// Find the last message with role="clarification" and matching round.
	for i := len(c.Messages) - 1; i >= 0; i-- {
		var cm clarificationMessage
		if err := json.Unmarshal(c.Messages[i], &cm); err != nil {
			continue
		}
		if cm.Role != "clarification" || cm.Round != round {
			continue
		}
		cm.Answers = answers
		raw, err := json.Marshal(cm)
		if err != nil {
			return fmt.Errorf("marshal updated clarification round: %w", err)
		}
		c.Messages[i] = raw
		return s.writeConversation(c)
	}
	return fmt.Errorf("clarification round %d not found in conversation %s", round, id)
}

func (s *Store) GetLastClarificationRound(id string) (*council.ClarificationRound, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, err := s.readConversation(id)
	if err != nil {
		return nil, err
	}
	for i := len(c.Messages) - 1; i >= 0; i-- {
		var cm clarificationMessage
		if err := json.Unmarshal(c.Messages[i], &cm); err != nil {
			continue
		}
		if cm.Role != "clarification" {
			continue
		}
		return &council.ClarificationRound{
			Round:       cm.Round,
			Questions:   cm.Questions,
			Answers:     cm.Answers,
			CouncilType: cm.CouncilType,
		}, nil
	}
	return nil, nil // no clarification round found
}

func (s *Store) SaveTitle(id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readConversation(id)
	if err != nil {
		return err
	}
	c.Title = title
	return s.writeConversation(c)
}

func (s *Store) DeleteConversation(id string) error {
	if !isValidUUID(id) {
		return &NotFoundError{ID: id}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.filePath(id)); err != nil {
		if os.IsNotExist(err) {
			return &NotFoundError{ID: id}
		}
		return fmt.Errorf("delete %s: %w", id, err)
	}
	return nil
}
