package eval

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/valpere/vmm-rada/internal/council"
)

type stubLLMClient struct {
	resp council.CompletionResponse
	err  error
}

func (s *stubLLMClient) Complete(_ context.Context, _ council.CompletionRequest) (council.CompletionResponse, error) {
	return s.resp, s.err
}

func TestMeteringClient_AccumulatesOnSuccess(t *testing.T) {
	stub := &stubLLMClient{
		resp: council.CompletionResponse{
			Choices: []struct {
				Message council.ChatMessage `json:"message"`
			}{{Message: council.ChatMessage{Role: "assistant", Content: "hello"}}},
			Usage: council.Usage{PromptTokens: 10, CompletionTokens: 5, CostUSD: 0.001},
		},
	}
	mc := NewMeteringClient(stub)

	for range 3 {
		_, err := mc.Complete(context.Background(), council.CompletionRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	pt, ct, cost := mc.Totals()
	if pt != 30 || ct != 15 {
		t.Errorf("tokens: want pt=30 ct=15, got pt=%d ct=%d", pt, ct)
	}
	if cost < 0.0029 || cost > 0.0031 {
		t.Errorf("cost: want ~0.003, got %f", cost)
	}
}

func TestMeteringClient_NoAccumulationOnError(t *testing.T) {
	stub := &stubLLMClient{
		resp: council.CompletionResponse{
			Usage: council.Usage{PromptTokens: 100, CompletionTokens: 50},
		},
		err: errors.New("LLM call failed"),
	}
	mc := NewMeteringClient(stub)

	_, _ = mc.Complete(context.Background(), council.CompletionRequest{})

	pt, ct, _ := mc.Totals()
	if pt != 0 || ct != 0 {
		t.Errorf("tokens should be 0 on error, got pt=%d ct=%d", pt, ct)
	}
}

func TestMeteringClient_ConcurrentAccumulation(t *testing.T) {
	stub := &stubLLMClient{
		resp: council.CompletionResponse{
			Usage: council.Usage{PromptTokens: 1, CompletionTokens: 1},
		},
	}
	mc := NewMeteringClient(stub)

	const goroutines = 50
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mc.Complete(context.Background(), council.CompletionRequest{})
		}()
	}
	wg.Wait()

	if total := mc.TotalTokens(); total != goroutines*2 {
		t.Errorf("want %d total tokens, got %d", goroutines*2, total)
	}
}
