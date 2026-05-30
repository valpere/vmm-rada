package council

import "context"

// LLMClient is the interface for sending completion requests to an LLM gateway.
// Rada logic depends only on this interface, not on a specific gateway implementation.
type LLMClient interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// Runner orchestrates a full council deliberation.
// All stage results are delivered via onEvent — the caller never receives
// stage structs directly, keeping the handler free of council-internal types.
// councilType is a string name resolved to a CouncilType by the Runner implementation.
type Runner interface {
	RunFull(ctx context.Context, query string, councilType string, onEvent EventFunc) error
}

// Stage0Runner orchestrates a single clarification round.
// Results are delivered via onEvent only — no stage structs are returned directly.
// Emits one of:
//
//	"stage0_round_complete" — chairman has questions; stream should close
//	"stage0_done"           — loop should terminate; Stage 1/2/3 can proceed
type Stage0Runner interface {
	RunClarificationRound(
		ctx context.Context,
		query string,
		history []ClarificationRound,
		cfg ClarificationConfig,
		councilType string,
		onEvent EventFunc,
	) error

	RunFullWithClarifications(
		ctx context.Context,
		originalQuery string,
		history []ClarificationRound,
		councilType string,
		onEvent EventFunc,
	) error
}
