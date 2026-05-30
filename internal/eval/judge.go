package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/valpere/vmm-rada/internal/council"
)

// Judge runs a pairwise comparison between two answers and returns one of
// "A", "B", or "tie" together with a short explanation. The judge is a third
// model — distinct from the baseline — selected by the caller. The system
// prompt explicitly tells the model not to use length as a quality signal,
// which mitigates the most common LLM-as-judge bias against the council
// (whose 3-stage pipeline tends to produce longer answers).
type Judge struct {
	Client      council.LLMClient
	Model       string
	Temperature float64
}

// judgeSystemPrompt is the verbatim system message sent to the judge for
// every comparison. Order randomisation happens at the orchestrator level —
// the judge always sees "Answer A" then "Answer B" without knowing which is
// which.
const judgeSystemPrompt = `You are an impartial evaluator. Compare two candidate answers ` +
	`to the same question. Length is not a quality signal — prefer the more correct ` +
	`and concise answer. Return ONLY a JSON object with fields ` +
	`{"verdict": "A" | "B" | "tie", "explanation": "..."}.`

// judgeUserTemplate formats the question + two answers into a single user
// message. Plain-text concatenation is sufficient: the judge does not need to
// see structured data, only the raw text.
const judgeUserTemplate = "Question:\n%s\n\nAnswer A:\n%s\n\nAnswer B:\n%s\n\nWhich is better?"

// judgeResponse is the schema the judge is expected to return.
type judgeResponse struct {
	Verdict     string `json:"verdict"`
	Explanation string `json:"explanation"`
}

// Compare asks the judge model to pick between answerA and answerB for the
// given prompt. Returns the raw verdict ("A", "B", or "tie") and an
// explanation. Returns an error only if the LLM call itself fails or the
// response is unparseable; an unrecognised verdict string surfaces as an
// error so the orchestrator records it as VerdictError rather than silently
// remapping it to one of the pipelines.
func (j *Judge) Compare(ctx context.Context, prompt, answerA, answerB string) (verdict, explanation string, err error) {
	if j == nil || j.Client == nil {
		return "", "", errors.New("eval: Judge.Client is required")
	}
	if j.Model == "" {
		return "", "", errors.New("eval: Judge.Model is required")
	}

	req := council.CompletionRequest{
		Model: j.Model,
		Messages: []council.ChatMessage{
			{Role: "system", Content: judgeSystemPrompt},
			{Role: "user", Content: fmt.Sprintf(judgeUserTemplate, prompt, answerA, answerB)},
		},
		Temperature:    j.Temperature,
		ResponseFormat: &council.ResponseFormat{Type: "json_object"},
	}
	resp, err := j.Client.Complete(ctx, req)
	if err != nil {
		return "", "", fmt.Errorf("judge llm call: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", "", errors.New("judge llm response had no choices")
	}

	raw := council.StripCodeFence(resp.Choices[0].Message.Content)
	var parsed judgeResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", "", fmt.Errorf("judge response parse: %w (raw: %q)", err, truncate(raw, 200))
	}

	switch parsed.Verdict {
	case "A", "B", "tie":
		return parsed.Verdict, parsed.Explanation, nil
	case "":
		return "", "", fmt.Errorf("judge response missing verdict (raw: %q)", truncate(raw, 200))
	default:
		return "", "", fmt.Errorf("judge response has unknown verdict %q (raw: %q)", parsed.Verdict, truncate(raw, 200))
	}
}

// truncate returns at most n bytes of s, used when embedding malformed
// model output into error messages so a 4 MiB blob doesn't drown logs.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
