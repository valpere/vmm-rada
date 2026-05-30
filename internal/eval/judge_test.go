package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/valpere/vmm-rada/internal/council"
)

func TestJudge_Compare_HappyPath(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantVerdict string
	}{
		{"verdict A", `{"verdict":"A","explanation":"clearer"}`, "A"},
		{"verdict B", `{"verdict":"B","explanation":"more correct"}`, "B"},
		{"verdict tie", `{"verdict":"tie","explanation":"equivalent"}`, "tie"},
		{"strips ```json fence",
			"```json\n{\"verdict\":\"A\",\"explanation\":\"e\"}\n```",
			"A"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := &Judge{
				Client: &fakeLLMClient{responses: map[string]string{"judge-y": tc.body}},
				Model:  "judge-y",
			}
			verdict, exp, err := j.Compare(context.Background(), "Q?", "answerA", "answerB")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if verdict != tc.wantVerdict {
				t.Errorf("verdict: got %q, want %q", verdict, tc.wantVerdict)
			}
			if exp == "" {
				t.Errorf("explanation should not be empty")
			}
		})
	}
}

func TestJudge_Compare_SystemPromptIncludesVerbosityMitigation(t *testing.T) {
	client := &fakeLLMClient{responses: map[string]string{"judge-y": `{"verdict":"A","explanation":"e"}`}}
	j := &Judge{Client: client, Model: "judge-y"}

	if _, _, err := j.Compare(context.Background(), "Q?", "A", "B"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected exactly 1 LLM call, got %d", len(client.calls))
	}
	msgs := client.calls[0].Messages
	if len(msgs) < 2 || msgs[0].Role != "system" {
		t.Fatalf("expected system message first, got %+v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "Length is not a quality signal") {
		t.Errorf("system prompt missing verbosity-bias mitigation: %q", msgs[0].Content)
	}
}

func TestJudge_Compare_RequestsJSONResponseFormat(t *testing.T) {
	client := &fakeLLMClient{responses: map[string]string{"judge-y": `{"verdict":"A","explanation":"e"}`}}
	j := &Judge{Client: client, Model: "judge-y"}

	_, _, _ = j.Compare(context.Background(), "Q?", "A", "B")
	if len(client.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(client.calls))
	}
	rf := client.calls[0].ResponseFormat
	if rf == nil || rf.Type != "json_object" {
		t.Errorf("ResponseFormat: got %+v, want json_object", rf)
	}
}

func TestJudge_Compare_MalformedJSONReturnsError(t *testing.T) {
	j := &Judge{
		Client: &fakeLLMClient{responses: map[string]string{"judge-y": "not json at all"}},
		Model:  "judge-y",
	}
	_, _, err := j.Compare(context.Background(), "Q?", "A", "B")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "judge response parse") {
		t.Errorf("error: %v, want it to mention parse failure", err)
	}
}

func TestJudge_Compare_MissingVerdictFieldReturnsError(t *testing.T) {
	j := &Judge{
		Client: &fakeLLMClient{responses: map[string]string{"judge-y": `{"explanation":"e"}`}},
		Model:  "judge-y",
	}
	_, _, err := j.Compare(context.Background(), "Q?", "A", "B")
	if err == nil {
		t.Fatal("expected error for missing verdict")
	}
	if !strings.Contains(err.Error(), "missing verdict") {
		t.Errorf("error: %v, want it to mention missing verdict", err)
	}
}

func TestJudge_Compare_UnknownVerdictReturnsError(t *testing.T) {
	j := &Judge{
		Client: &fakeLLMClient{responses: map[string]string{"judge-y": `{"verdict":"maybe","explanation":"e"}`}},
		Model:  "judge-y",
	}
	_, _, err := j.Compare(context.Background(), "Q?", "A", "B")
	if err == nil {
		t.Fatal("expected error for unknown verdict")
	}
	if !strings.Contains(err.Error(), "unknown verdict") {
		t.Errorf("error: %v, want it to mention unknown verdict", err)
	}
}

func TestJudge_Compare_ClientErrorPropagates(t *testing.T) {
	want := errors.New("rate limit")
	j := &Judge{
		Client: &fakeLLMClient{
			responses: map[string]string{"judge-y": ""},
			errs:      map[string]error{"judge-y": want},
		},
		Model: "judge-y",
	}
	_, _, err := j.Compare(context.Background(), "Q?", "A", "B")
	if err == nil || !errors.Is(err, want) {
		t.Errorf("err: %v, want chain to include %v", err, want)
	}
}

func TestJudge_Compare_NilClientReturnsError(t *testing.T) {
	j := &Judge{Model: "judge-y"}
	_, _, err := j.Compare(context.Background(), "Q?", "A", "B")
	if err == nil || !strings.Contains(err.Error(), "Client is required") {
		t.Errorf("err: %v, want 'Client is required'", err)
	}
}

func TestJudge_Compare_EmptyModelReturnsError(t *testing.T) {
	j := &Judge{Client: &fakeLLMClient{}, Model: ""}
	_, _, err := j.Compare(context.Background(), "Q?", "A", "B")
	if err == nil || !strings.Contains(err.Error(), "Model is required") {
		t.Errorf("err: %v, want 'Model is required'", err)
	}
}

func TestJudge_Compare_NoChoicesReturnsError(t *testing.T) {
	// fakeLLMClient with an empty body wouldn't trigger this — we need an
	// LLMClient that returns a CompletionResponse with zero choices.
	j := &Judge{Client: noChoicesClient{}, Model: "judge-y"}
	_, _, err := j.Compare(context.Background(), "Q?", "A", "B")
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Errorf("err: %v, want 'no choices'", err)
	}
}

// noChoicesClient is a one-off fake that returns a successful CompletionResponse
// with an empty Choices slice. Real OpenRouter responses can technically be
// shaped this way if the gateway returns 200 with truncated body — the judge
// must not crash.
type noChoicesClient struct{}

func (noChoicesClient) Complete(_ context.Context, _ council.CompletionRequest) (council.CompletionResponse, error) {
	return council.CompletionResponse{}, nil
}

// truncate is exercised indirectly by error cases above; this test pins its
// own boundary behaviour because future error formatting changes might forget
// the cap.
func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate('short', 10) = %q, want short", got)
	}
	got := truncate(strings.Repeat("a", 250), 200)
	// 200 'a's + the U+2026 ellipsis (3 bytes in UTF-8) → 203 bytes total.
	if len(got) != 203 {
		t.Errorf("truncate len: got %d, want 203 (200 + 3-byte ellipsis)", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate suffix: got %q, want trailing ellipsis", got[len(got)-3:])
	}
}
