package council

import (
	"errors"
	"fmt"
	"math/rand"
)

// ErrCircuitOpen is returned by LLMClient.Complete when the circuit breaker is
// open and the call is rejected without an HTTP attempt. Defined here so both
// the openrouter implementation and the council runner can reference it without
// a circular import.
var ErrCircuitOpen = errors.New("circuit breaker open")

// QuorumError is returned when not enough council members responded successfully.
type QuorumError struct {
	Got    int
	Need   int
	Reason string // "provider circuit open" when all failures are ErrCircuitOpen; empty otherwise
}

func (e *QuorumError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("council quorum not met: got %d successful responses, need %d (%s)", e.Got, e.Need, e.Reason)
	}
	return fmt.Sprintf("council quorum not met: got %d successful responses, need %d", e.Got, e.Need)
}

// checkQuorum filters results to successful entries and verifies the minimum quorum.
// M_min = max(2, ⌈N/2⌉+1) where N = len(results), unless minQuorum > 0 overrides it.
// Returns the successful subset or *QuorumError if the threshold is not met.
// QuorumError.Reason is set to "provider circuit open" when every failed member
// returned ErrCircuitOpen.
func checkQuorum(results []StageOneResult, minQuorum int) ([]StageOneResult, error) {
	var successful, failed []StageOneResult
	for _, r := range results {
		if r.Error == nil {
			successful = append(successful, r)
		} else {
			failed = append(failed, r)
		}
	}
	n := len(results)
	need := minQuorum
	if need <= 0 {
		need = max(2, (n+1)/2+1) // ⌈N/2⌉ = (N+1)/2 in integer arithmetic
	}
	if len(successful) < need {
		qe := &QuorumError{Got: len(successful), Need: need}
		if allCircuitOpen(failed) {
			qe.Reason = "provider circuit open"
		}
		return nil, qe
	}
	return successful, nil
}

// allCircuitOpen returns true when every entry in failed has ErrCircuitOpen as
// its error. Returns false for an empty slice (no failures ≠ all circuit open).
func allCircuitOpen(failed []StageOneResult) bool {
	if len(failed) == 0 {
		return false
	}
	for _, r := range failed {
		if !errors.Is(r.Error, ErrCircuitOpen) {
			return false
		}
	}
	return true
}

// assignLabels assigns anonymous labels to models using a per-request random shuffle.
// Labels are "Response A", "Response B", … (up to 26 models).
// Both forward (label→model) and reverse (model→label) maps are returned.
func assignLabels(models []string) (labelToModel, modelToLabel map[string]string) {
	perm := rand.Perm(len(models))
	labelToModel = make(map[string]string, len(models))
	modelToLabel = make(map[string]string, len(models))
	for i, idx := range perm {
		label := fmt.Sprintf("Response %c", rune('A'+i))
		labelToModel[label] = models[idx]
		modelToLabel[models[idx]] = label
	}
	return
}

// assignAggregatorLabels mirrors assignLabels but uses the "Aggregator " prefix
// so MixtureOfAgents Layer 2 labels stay visually distinct from Layer 1
// proposer labels. Both label families end up flat in Metadata.LabelToModel —
// key collisions are impossible because the prefixes differ.
func assignAggregatorLabels(models []string) (labelToModel, modelToLabel map[string]string) {
	perm := rand.Perm(len(models))
	labelToModel = make(map[string]string, len(models))
	modelToLabel = make(map[string]string, len(models))
	for i, idx := range perm {
		label := fmt.Sprintf("Aggregator %c", rune('A'+i))
		labelToModel[label] = models[idx]
		modelToLabel[models[idx]] = label
	}
	return
}
