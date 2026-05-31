package eval

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// BenchmarkItem is one question in a YAML benchmark file.
type BenchmarkItem struct {
	ID       string `yaml:"id"`
	Category string `yaml:"category"`
	Strategy string `yaml:"strategy,omitempty"`
	Question string `yaml:"question"`
	Rubric   string `yaml:"rubric,omitempty"`
}

// LoadBenchmark parses a YAML benchmark file into a slice of BenchmarkItems.
// Returns a typed error when any item is missing the required id or question
// fields.
func LoadBenchmark(data []byte) ([]BenchmarkItem, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var items []BenchmarkItem
	if err := yaml.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse benchmark: %w", err)
	}
	for i, item := range items {
		if item.ID == "" {
			return nil, &BenchmarkValidationError{Index: i, Field: "id"}
		}
		if item.Question == "" {
			return nil, &BenchmarkValidationError{Index: i, Field: "question"}
		}
	}
	return items, nil
}

// BenchmarkValidationError is returned when a required field is missing.
type BenchmarkValidationError struct {
	Index int
	Field string
}

func (e *BenchmarkValidationError) Error() string {
	return fmt.Sprintf("benchmark item %d: missing required field %q", e.Index, e.Field)
}
