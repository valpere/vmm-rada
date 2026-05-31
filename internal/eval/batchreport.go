package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// BatchReport is the top-level record written to runs/<timestamp>/report.json.
// It extends the single-run Output with metering totals and timing metadata.
type BatchReport struct {
	RunID         string    `json:"run_id"`
	BenchmarkFile string    `json:"benchmark_file"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	Meta          Meta      `json:"meta"`
	Results       []Result  `json:"results"`
	Summary       Summary   `json:"summary"`
	TotalTokens   int       `json:"total_tokens"`
	TotalCostUSD  float64   `json:"total_cost_usd"`
}

// WriteBatchReport serialises report as indented JSON to w.
func WriteBatchReport(w io.Writer, report BatchReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode batch report: %w", err)
	}
	return nil
}
