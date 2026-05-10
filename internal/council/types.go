package council

// Strategy identifies the deliberation algorithm used by a CouncilType.
type Strategy int

const (
	// PeerReview runs the 3-stage Karpathy pipeline: parallel generation →
	// anonymous peer ranking → chairman synthesis. Implemented in runner.go.
	PeerReview Strategy = iota

	// RoleBased runs a 2-stage pipeline: parallel specialist roles → chairman
	// synthesis. Stage 2 (peer ranking) is skipped; a stub Stage2CompleteData
	// event is emitted for SSE compatibility. Implemented in rolebased.go.
	RoleBased

	// Majority runs independent generation followed by a vote (exact match,
	// cluster, or weighted). Selects rather than synthesises. Not implemented.
	Majority

	// GenerateRankRefine runs parallel generation, ranks candidates against
	// structured criteria, then refines the top-K. Not implemented.
	GenerateRankRefine

	// MultiAgentDebate runs initial answers followed by N rounds of mutual
	// critique and revision, then synthesises. Not implemented.
	MultiAgentDebate

	// MixtureOfAgents runs a layered architecture: proposers → aggregators →
	// refiner. Not implemented.
	MixtureOfAgents

	// Delphi runs multiple anonymous blind rating rounds with averaged ratings.
	// Not implemented.
	Delphi
)

// Role defines a named participant with a specific mandate in a role-based council.
type Role struct {
	Name        string `json:"name"`
	Instruction string `json:"instruction"` // system-level prompt for this role
}

// CouncilType describes a named council configuration.
// QuorumMin of 0 means use the formula: max(2, ⌈N/2⌉+1).
type CouncilType struct {
	Name          string
	Strategy      Strategy
	Models        []string // Council members. RoleBased assigns models to Roles by index mod len; other strategies use all.
	Roles         []Role   // RoleBased only: role definitions with specialist instructions.
	ChairmanModel string
	Temperature   float64
	QuorumMin       int // 0 = strategy-specific default formula
	RefineTopK      int // GenerateRankRefine only: how many ranked candidates advance to refinement; 0 = default (3)
	MaxDebateRounds int // MultiAgentDebate only: number of debate rounds after Stage 1; 0 = default (2)
}

// ChatMessage is a single turn in a conversation history.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat instructs the LLM to return a specific format.
type ResponseFormat struct {
	Type string `json:"type"` // e.g. "json_object"
}

// CompletionRequest is sent to the LLM gateway.
type CompletionRequest struct {
	Model          string          `json:"model"`
	Messages       []ChatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// CompletionResponse is received from the LLM gateway.
type CompletionResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
}

// EventFunc is the callback used to stream stage-completion events to the caller.
type EventFunc func(eventType string, data any)

// StageOneResult holds a single council member's generated answer.
type StageOneResult struct {
	Label      string `json:"label"`      // anonymised label, e.g. "Response A"
	Content    string `json:"content"`
	Model      string `json:"model"`
	DurationMs int64  `json:"duration_ms"` // elapsed milliseconds
	Error      error  `json:"-"`
}

// StageTwoResult holds a single council member's peer-review rankings.
type StageTwoResult struct {
	ReviewerLabel string   `json:"reviewer_label"`
	Rankings      []string `json:"rankings"` // ordered labels, best first
	Error         error    `json:"-"`
}

// StageThreeResult holds the chairman's synthesised final answer.
//
// Model is the OpenRouter ID of the synthesising model when an LLM call
// produced the content. For strategies whose Stage 3 emits a result without
// an LLM call (e.g. Majority's plurality winner with no chairman), Model is
// the empty string and DurationMs is 0. Frontend renderers must handle the
// empty-Model case gracefully (omit the model badge).
type StageThreeResult struct {
	Content    string `json:"content"`
	Model      string `json:"model"`
	DurationMs int64  `json:"duration_ms"` // elapsed milliseconds
	Error      error  `json:"-"`
}

// RankedModel pairs a model name with its aggregate rank score.
type RankedModel struct {
	Model string  `json:"model"`
	Score float64 `json:"score"`
}

// VoteCluster groups Stage 1 answers that normalise to the same content under
// the Majority strategy's voting algorithm. Members holds the labels of the
// Stage 1 results in the cluster; Representative is the verbatim content of
// the cluster's first member (used for display and downstream synthesis).
type VoteCluster struct {
	Members        []string `json:"members"`
	Representative string   `json:"representative"`
	Votes          int      `json:"votes"`
}

// VoteTally is the result of clustering Majority Stage 1 answers and selecting
// the plurality winner. Clusters are sorted by Votes descending, then by
// Representative ascending for stable output. WinnerLabel is the label of the
// first member in the winning (highest-votes) cluster.
type VoteTally struct {
	Clusters    []VoteCluster `json:"clusters"`
	WinnerLabel string        `json:"winner_label"`
}

// RankedCandidate is a single Stage 1 answer scored by the GenerateRankRefine
// arbiter against a fixed set of criteria. Scores are clamped to [0.0, 1.0]
// per criterion; TotalScore is the sum across all criteria, clamped to
// [0.0, len(Criteria)].
type RankedCandidate struct {
	Label      string             `json:"label"`
	Scores     map[string]float64 `json:"scores"`
	TotalScore float64            `json:"total_score"`
	Advancing  bool               `json:"advancing"`
}

// RankRefine is the Stage 2 payload for the GenerateRankRefine strategy.
// Rankings are sorted by TotalScore descending, then by Label ascending for
// stable output. Exactly TopK candidates have Advancing=true; ties at the
// K boundary are broken by the secondary Label sort (no rebalancing, no
// chairman tiebreak). Criteria lists the criterion names in fixed order.
type RankRefine struct {
	Rankings []RankedCandidate `json:"rankings"`
	TopK     int               `json:"top_k"`
	Criteria []string          `json:"criteria"`
}

// DebaterRevision is a single debater's output in one round of the
// MultiAgentDebate strategy: a critique of the OTHER debaters' previous-round
// answers plus this debater's revised answer. Critique is omitempty so empty
// critiques don't bloat the wire.
type DebaterRevision struct {
	Label      string `json:"label"`
	Critique   string `json:"critique,omitempty"`
	Content    string `json:"content"`
	Model      string `json:"model"`
	DurationMs int64  `json:"duration_ms"`
	Error      error  `json:"-"`
}

// DebateRound holds all surviving debaters' revisions for a single round
// (rounds 1..R; round 0 is the initial Stage 1 generation and lives on
// AssistantMessage.Stage1, not here). Revisions are sorted by Label ascending
// for stable output across runs.
type DebateRound struct {
	Round     int               `json:"round"`
	Revisions []DebaterRevision `json:"revisions"`
}

// DebaterDropout records when and why a debater stopped producing revisions.
// Surfaced to the chairman prompt (so it can reason about an evolving cast)
// and to the frontend DebateView (rendered as muted timeline rows).
type DebaterDropout struct {
	Label     string `json:"label"`
	LastRound int    `json:"last_round"` // last round in which they produced a successful revision; 0 = round 0 only
	Reason    string `json:"reason"`     // "error" / "json_parse" / "empty_revision"
}

// Debate is the Stage 2 payload for the MultiAgentDebate strategy. Rounds
// holds the per-round revisions (rounds 1..FinalRound). FinalRound is the
// last completed round (==len(Rounds) on success). Dropouts records debaters
// that fell out of the debate; omitempty so the field is absent when nobody
// dropped.
type Debate struct {
	Rounds     []DebateRound    `json:"rounds"`
	FinalRound int              `json:"final_round"`
	Dropouts   []DebaterDropout `json:"dropouts,omitempty"`
}

// Metadata is persisted with every assistant message.
//
// VoteTally is populated only by the Majority strategy; RankRefine only by
// GenerateRankRefine; Debate only by MultiAgentDebate. omitempty keeps each
// absent on the wire and at rest for every other strategy.
type Metadata struct {
	CouncilType       string            `json:"council_type"`
	LabelToModel      map[string]string `json:"label_to_model"`
	AggregateRankings []RankedModel     `json:"aggregate_rankings"`
	ConsensusW        float64           `json:"consensus_w"`
	VoteTally         *VoteTally        `json:"vote_tally,omitempty"`
	RankRefine        *RankRefine       `json:"rank_refine,omitempty"`
	Debate            *Debate           `json:"debate,omitempty"`
}

// Stage2CompleteData is the payload emitted by Runner for the "stage2_complete" event.
// It bundles peer-review results with the computed aggregate metadata so callers
// (e.g. the SSE handler) can surface both in one event.
//
// Kind discriminates the strategy-specific payload shape. Today's two implemented
// values are "peer_ranking" (PeerReview) and "role_stub" (RoleBased). Five more
// are reserved for planned strategies; see docs/strategies.md for their schemas.
//
// Round is reserved for future multi-round strategies (MultiAgentDebate, Delphi).
// Today both implemented strategies emit a single stage2_complete with Round=0.
type Stage2CompleteData struct {
	Kind     string           `json:"kind"`
	Round    int              `json:"round,omitempty"`
	Results  []StageTwoResult `json:"results"`
	Metadata Metadata         `json:"metadata"`
}

// AssistantMessage is the full deliberation record stored with each assistant turn.
type AssistantMessage struct {
	Role     string           `json:"role"`
	Stage1   []StageOneResult `json:"stage1"`
	Stage2   []StageTwoResult `json:"stage2"`
	Stage3   StageThreeResult `json:"stage3"`
	Metadata Metadata         `json:"metadata"`
}

// ClarificationQuestion is a single question generated by a council member or chairman.
type ClarificationQuestion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ClarificationAnswer is the user's response to a single clarification question.
type ClarificationAnswer struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ClarificationRound holds one round of clarification Q&A.
// Answers is empty until the user submits a response.
type ClarificationRound struct {
	Round       int                     `json:"round"`
	Questions   []ClarificationQuestion `json:"questions"`
	Answers     []ClarificationAnswer   `json:"answers"`
	CouncilType string                  `json:"council_type,omitempty"` // persisted on round-1
}

// ClarificationConfig holds Stage 0 operational limits and model overrides.
//
// Models and ArbiterModel are optional. When non-empty they override the
// council type's models for Stage 0; when empty the runner falls back to the
// council type's Models / ChairmanModel respectively. See RunClarificationRound
// for the resolution chain (env override → per-council-type → error).
type ClarificationConfig struct {
	MaxRounds            int
	MaxTotalQuestions    int
	MaxQuestionsPerRound int
	Models               []string // optional; empty = use ct.Models
	ArbiterModel         string   // optional; empty = use ct.ChairmanModel
}
