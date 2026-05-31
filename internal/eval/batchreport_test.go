package eval

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestBatchReport_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	report := BatchReport{
		RunID:         "20260601-120000",
		BenchmarkFile: "eval/benchmarks/baseline.yaml",
		StartedAt:     now,
		FinishedAt:    now.Add(5 * time.Minute),
		Meta: Meta{
			Seed:          42,
			BaselineModel: "gpt-4o-mini",
			JudgeModel:    "claude-sonnet",
			CouncilType:   "default",
		},
		Results: []Result{
			{ID: "code-001", JudgeVerdict: VerdictCouncil},
			{ID: "factual-001", JudgeVerdict: VerdictTie},
		},
		Summary:      Summary{CouncilWon: 1, Ties: 1},
		TotalTokens:  1234,
		TotalCostUSD: 0.05,
	}

	var buf bytes.Buffer
	if err := WriteBatchReport(&buf, report); err != nil {
		t.Fatalf("write: %v", err)
	}

	var got BatchReport
	if err := json.NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.RunID != report.RunID {
		t.Errorf("RunID: got %q, want %q", got.RunID, report.RunID)
	}
	if got.TotalTokens != report.TotalTokens {
		t.Errorf("TotalTokens: got %d, want %d", got.TotalTokens, report.TotalTokens)
	}
	if got.TotalCostUSD != report.TotalCostUSD {
		t.Errorf("TotalCostUSD: got %f, want %f", got.TotalCostUSD, report.TotalCostUSD)
	}
	if len(got.Results) != 2 {
		t.Errorf("Results: got %d, want 2", len(got.Results))
	}
	if got.Summary.CouncilWon != 1 || got.Summary.Ties != 1 {
		t.Errorf("Summary: got %+v, want {CouncilWon:1 Ties:1}", got.Summary)
	}
}
