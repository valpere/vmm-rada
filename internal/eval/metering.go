package eval

import (
	"context"
	"sync"

	"github.com/valpere/vmm-rada/internal/council"
)

// meteringClient wraps a council.LLMClient and accumulates token counts and
// costs across calls. Only successful calls (err == nil) are counted.
// All methods are safe for concurrent use.
type meteringClient struct {
	inner            council.LLMClient
	mu               sync.Mutex
	promptTokens     int
	completionTokens int
	costUSD          float64
}

// NewMeteringClient wraps inner with token/cost accumulation.
func NewMeteringClient(inner council.LLMClient) *meteringClient {
	return &meteringClient{inner: inner}
}

func (m *meteringClient) Complete(ctx context.Context, req council.CompletionRequest) (council.CompletionResponse, error) {
	resp, err := m.inner.Complete(ctx, req)
	if err == nil {
		m.mu.Lock()
		m.promptTokens += resp.Usage.PromptTokens
		m.completionTokens += resp.Usage.CompletionTokens
		m.costUSD += resp.Usage.CostUSD
		m.mu.Unlock()
	}
	return resp, err
}

// Totals returns the accumulated token counts and cost. Safe to call
// concurrently with Complete.
func (m *meteringClient) Totals() (promptTokens, completionTokens int, costUSD float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.promptTokens, m.completionTokens, m.costUSD
}

// TotalTokens returns promptTokens + completionTokens.
func (m *meteringClient) TotalTokens() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.promptTokens + m.completionTokens
}
