package eval

import (
	"errors"
	"testing"
)

func TestLoadBenchmark_ValidParse(t *testing.T) {
	yaml := []byte(`
- id: code-001
  category: code
  question: Write a Go function that reverses a string.
- id: analysis-001
  category: analysis
  question: Explain the CAP theorem.
  rubric: Must cover all three properties.
`)
	items, err := LoadBenchmark(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].ID != "code-001" || items[0].Category != "code" {
		t.Errorf("item 0: got id=%q cat=%q", items[0].ID, items[0].Category)
	}
	if items[1].Rubric == "" {
		t.Error("item 1 rubric should be non-empty")
	}
}

func TestLoadBenchmark_MissingID(t *testing.T) {
	yaml := []byte(`
- category: code
  question: Write a Go function.
`)
	_, err := LoadBenchmark(yaml)
	var ve *BenchmarkValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *BenchmarkValidationError, got %T: %v", err, err)
	}
	if ve.Field != "id" {
		t.Errorf("want field=%q, got %q", "id", ve.Field)
	}
}

func TestLoadBenchmark_MissingQuestion(t *testing.T) {
	yaml := []byte(`
- id: code-001
  category: code
`)
	_, err := LoadBenchmark(yaml)
	var ve *BenchmarkValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *BenchmarkValidationError, got %T: %v", err, err)
	}
	if ve.Field != "question" {
		t.Errorf("want field=%q, got %q", "question", ve.Field)
	}
}

func TestLoadBenchmark_EmptyFile(t *testing.T) {
	items, err := LoadBenchmark([]byte{})
	if err != nil {
		t.Fatalf("unexpected error on empty input: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("want 0 items, got %d", len(items))
	}
}

func TestLoadBenchmark_UnknownFieldIgnored(t *testing.T) {
	yaml := []byte(`
- id: code-001
  category: code
  question: Write something.
  unknown_future_field: ignored
`)
	items, err := LoadBenchmark(yaml)
	if err != nil {
		t.Fatalf("unknown field should be ignored, got error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
}
