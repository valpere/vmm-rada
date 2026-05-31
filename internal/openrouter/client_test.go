package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/valpere/vmm-rada/internal/council"
)

// testClient returns a Client pointed at srv with the given API key and no
// retries. Use testClientWithRetries when a test exercises the retry loop.
func testClient(apiKey string, srv *httptest.Server) *Client {
	return testClientWithRetries(apiKey, srv, 0)
}

// testClientWithRetries returns a Client pointed at srv with the given API key
// and retry budget. Logger is silenced (Discard). Backoff knobs are set to
// fast defaults (1ms base, 60s cap) so retry tests run quickly without
// mutating package globals — each test gets its own isolated Client.
func testClientWithRetries(apiKey string, srv *httptest.Server, retries int) *Client {
	return &Client{
		apiKey:                       apiKey,
		baseURL:                      srv.URL,
		http:                         srv.Client(),
		maxRetries:                   retries,
		logger:                       slog.New(slog.NewTextHandler(io.Discard, nil)),
		retryBaseDelay:               1 * time.Millisecond,
		maxCumulativeBackoffDuration: defaultMaxCumulativeBackoffDuration,
	}
}

// mockCompletion is the minimal OpenAI-compatible completion shape.
type mockCompletion struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ── TestComplete_RequiredHeaders ──────────────────────────────────────────────

func TestComplete_RequiredHeaders(t *testing.T) {
	var gotAuth, gotReferer, gotTitle string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		writeJSON(w, mockCompletion{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "hi"}},
			},
		})
	}))
	defer srv.Close()

	c := testClient("sk-test-key", srv)
	_, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []council.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization: got %q, want %q", gotAuth, "Bearer sk-test-key")
	}
	if gotReferer == "" {
		t.Error("HTTP-Referer header missing")
	}
	if gotTitle == "" {
		t.Error("X-Title header missing")
	}
}

// ── TestComplete_SuccessfulResponse ──────────────────────────────────────────

func TestComplete_SuccessfulResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, mockCompletion{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "Paris"}},
			},
		})
	}))
	defer srv.Close()

	c := testClient("key", srv)
	resp, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []council.ChatMessage{{Role: "user", Content: "capital of France?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices in response")
	}
	if got := resp.Choices[0].Message.Content; got != "Paris" {
		t.Errorf("content: got %q, want %q", got, "Paris")
	}
}

// ── TestComplete_APIError_OnNonOK ─────────────────────────────────────────────

func TestComplete_APIError_OnNonOK(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"400 bad request", http.StatusBadRequest},
		{"429 rate limited", http.StatusTooManyRequests},
		{"500 internal error", http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(`{"error":"test error"}`))
			}))
			defer srv.Close()

			c := testClient("key", srv)
			_, err := c.Complete(context.Background(), council.CompletionRequest{
				Model:    "openai/gpt-4o-mini",
				Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %T: %v", err, err)
			}
			if apiErr.StatusCode != tc.statusCode {
				t.Errorf("StatusCode: got %d, want %d", apiErr.StatusCode, tc.statusCode)
			}
			if apiErr.Body == "" {
				t.Error("APIError.Body should not be empty")
			}
		})
	}
}

// ── TestComplete_ResponseFormatForwarded ─────────────────────────────────────

func TestComplete_ResponseFormatForwarded(t *testing.T) {
	tests := []struct {
		name           string
		responseFormat *council.ResponseFormat
		wantInBody     bool
	}{
		{
			name:           "nil response_format omitted from request",
			responseFormat: nil,
			wantInBody:     false,
		},
		{
			name:           "json_object format forwarded",
			responseFormat: &council.ResponseFormat{Type: "json_object"},
			wantInBody:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				writeJSON(w, mockCompletion{
					Choices: []struct {
						Message struct {
							Role    string `json:"role"`
							Content string `json:"content"`
						} `json:"message"`
					}{
						{Message: struct {
							Role    string `json:"role"`
							Content string `json:"content"`
						}{Role: "assistant", Content: "{}"}},
					},
				})
			}))
			defer srv.Close()

			c := testClient("key", srv)
			_, err := c.Complete(context.Background(), council.CompletionRequest{
				Model:          "openai/gpt-4o-mini",
				Messages:       []council.ChatMessage{{Role: "user", Content: "hi"}},
				ResponseFormat: tc.responseFormat,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			_, present := gotBody["response_format"]
			if present != tc.wantInBody {
				t.Errorf("response_format present=%v, want %v", present, tc.wantInBody)
			}
		})
	}
}

// ── TestNewClient ─────────────────────────────────────────────────────────────

func TestNewClient_DefaultURL(t *testing.T) {
	c := NewClient("my-key", "", 30*time.Second, 2, nil, nil)
	if c.apiKey != "my-key" {
		t.Errorf("apiKey: got %q, want %q", c.apiKey, "my-key")
	}
	if c.baseURL != defaultURL {
		t.Errorf("baseURL: got %q, want %q", c.baseURL, defaultURL)
	}
	if c.http.Timeout != 30*time.Second {
		t.Errorf("timeout: got %v, want 30s", c.http.Timeout)
	}
	if c.maxRetries != 2 {
		t.Errorf("maxRetries: got %d, want 2", c.maxRetries)
	}
	if c.logger == nil {
		t.Error("logger should be substituted with slog.Default(), got nil")
	}
}

func TestNewClient_CustomURL(t *testing.T) {
	const custom = "http://localhost:11434/v1/chat/completions"
	c := NewClient("ollama", custom, 10*time.Second, 0, nil, nil)
	if c.baseURL != custom {
		t.Errorf("baseURL: got %q, want %q", c.baseURL, custom)
	}
	if c.maxRetries != 0 {
		t.Errorf("maxRetries: got %d, want 0", c.maxRetries)
	}
}

func TestNewClient_NegativeRetriesClampedToZero(t *testing.T) {
	c := NewClient("k", "", time.Second, -5, nil, nil)
	if c.maxRetries != 0 {
		t.Errorf("maxRetries: got %d, want 0 (clamped)", c.maxRetries)
	}
}

// ── Retry tests ────────────────────────────────────────────────────────────
//
// These tests configure backoff knobs on a per-Client basis (set on the
// struct returned by testClientWithRetries), not via package-level globals,
// so each case is fully isolated and safe to run in parallel.

// successfulCompletion returns a minimal-shape JSON OpenAI-compat success body.
func successfulCompletion(content string) any {
	return mockCompletion{
		Choices: []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}{{Message: struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "assistant", Content: content}}},
	}
}

func TestComplete_RetryOn503ThenSuccess(t *testing.T) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&counter, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, successfulCompletion("recovered"))
	}))
	defer srv.Close()

	c := testClientWithRetries("key", srv, 3)
	resp, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&counter); got != 2 {
		t.Errorf("call count: got %d, want 2 (initial 503 + 1 retry success)", got)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "recovered" {
		t.Errorf("response: got %+v, want content=recovered", resp)
	}
}

func TestComplete_RetryOn429WithRetryAfter(t *testing.T) {
	var (
		counter int32
		gap     time.Duration
		first   time.Time
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&counter, 1)
		switch n {
		case 1:
			first = time.Now()
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			gap = time.Since(first)
			writeJSON(w, successfulCompletion("ok"))
		}
	}))
	defer srv.Close()

	c := testClientWithRetries("key", srv, 3)
	if _, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&counter); got != 2 {
		t.Errorf("call count: got %d, want 2", got)
	}
	if gap < 900*time.Millisecond || gap > 1500*time.Millisecond {
		t.Errorf("gap between attempts: got %v, want ~1s honoring Retry-After", gap)
	}
}

func TestComplete_RetryOnTimeout(t *testing.T) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&counter, 1)
		if n == 1 {
			// First attempt hangs past client timeout.
			time.Sleep(150 * time.Millisecond)
			return
		}
		writeJSON(w, successfulCompletion("ok"))
	}))
	defer srv.Close()

	c := testClientWithRetries("key", srv, 2)
	c.http.Timeout = 50 * time.Millisecond

	if _, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if got := atomic.LoadInt32(&counter); got != 2 {
		t.Errorf("call count: got %d, want 2", got)
	}
}

func TestComplete_NoRetryOn401(t *testing.T) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&counter, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := testClientWithRetries("key", srv, 3)
	_, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected APIError, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected *APIError with status 401, got %v", err)
	}
	if got := atomic.LoadInt32(&counter); got != 1 {
		t.Errorf("call count: got %d, want 1 (no retry on 4xx other than 429)", got)
	}
}

func TestComplete_NoRetryOn200(t *testing.T) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&counter, 1)
		writeJSON(w, successfulCompletion("ok"))
	}))
	defer srv.Close()

	c := testClientWithRetries("key", srv, 3)
	if _, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&counter); got != 1 {
		t.Errorf("call count: got %d, want 1 (no retry on success)", got)
	}
}

func TestComplete_RetriesExhausted(t *testing.T) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&counter, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := testClientWithRetries("key", srv, 2) // 1 initial + 2 retries = 3 total
	_, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected *APIError with status 503, got %v", err)
	}
	if got := atomic.LoadInt32(&counter); got != 3 {
		t.Errorf("call count: got %d, want 3 (1 initial + 2 retries)", got)
	}
}

func TestComplete_ContextCancelDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := testClientWithRetries("key", srv, 3)
	// Use a long base delay so cancellation can interrupt the sleep.
	c.retryBaseDelay = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after the first 503 response, while we're sleeping.
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := c.Complete(ctx, council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed > 1*time.Second {
		t.Errorf("elapsed %v; cancel during backoff should return promptly", elapsed)
	}
}

func TestComplete_CumulativeBackoffCap(t *testing.T) {
	var counter int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&counter, 1)
		w.Header().Set("Retry-After", "1") // forces 1s delays
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := testClientWithRetries("key", srv, 5) // generous budget; cap should fire first
	c.maxCumulativeBackoffDuration = 1500 * time.Millisecond
	start := time.Now()
	_, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected *APIError 503, got %v", err)
	}
	// Attempt 0: 503, sleep 1s (cum=1s). Attempt 1: 503, would sleep 1s
	// (cum+delay=2s > 1.5s cap), so cap fires and we return without retry.
	if got := atomic.LoadInt32(&counter); got != 2 {
		t.Errorf("call count: got %d, want 2 (cap fires before third attempt)", got)
	}
	if elapsed > 2*time.Second {
		t.Errorf("elapsed %v; cap should have prevented further retries", elapsed)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty returns absent", "", retryAfterAbsent},
		{"valid seconds", "5", 5 * time.Second},
		{"zero seconds means retry-immediately", "0", 0}, // RFC 7231 — distinct from absent
		{"negative seconds returns absent", "-3", retryAfterAbsent},
		{"invalid returns absent", "soon", retryAfterAbsent},
		{"capped at maxRetryAfter", "3600", maxRetryAfter},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRetryAfter(tc.in)
			if got != tc.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}

	// HTTP-date forms — relative to time.Now(), so use a tolerance.
	t.Run("future HTTP-date ~5s", func(t *testing.T) {
		future := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
		got := parseRetryAfter(future)
		if got < 3*time.Second || got > 5*time.Second {
			t.Errorf("parseRetryAfter(%q) = %v, want ~5s", future, got)
		}
	})
	t.Run("past HTTP-date returns absent", func(t *testing.T) {
		past := time.Now().Add(-1 * time.Hour).UTC().Format(http.TimeFormat)
		if got := parseRetryAfter(past); got != retryAfterAbsent {
			t.Errorf("parseRetryAfter(%q) = %v, want retryAfterAbsent", past, got)
		}
	})
	t.Run("future HTTP-date capped at maxRetryAfter", func(t *testing.T) {
		far := time.Now().Add(1 * time.Hour).UTC().Format(http.TimeFormat)
		if got := parseRetryAfter(far); got != maxRetryAfter {
			t.Errorf("parseRetryAfter(%q) = %v, want %v (capped)", far, got, maxRetryAfter)
		}
	})
}

// TestComplete_RetryAfterZeroHonored verifies an explicit "Retry-After: 0"
// header is honored as "retry immediately" and not treated as absent.
func TestComplete_RetryAfterZeroHonored(t *testing.T) {
	var (
		counter int32
		gap     time.Duration
		first   time.Time
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&counter, 1)
		if n == 1 {
			first = time.Now()
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		gap = time.Since(first)
		writeJSON(w, successfulCompletion("ok"))
	}))
	defer srv.Close()

	// Set a relatively long retryBaseDelay so we can detect that Retry-After: 0
	// short-circuits the schedule rather than falling back to it.
	c := testClientWithRetries("key", srv, 2)
	c.retryBaseDelay = 250 * time.Millisecond

	if _, err := c.Complete(context.Background(), council.CompletionRequest{
		Model:    "x",
		Messages: []council.ChatMessage{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&counter); got != 2 {
		t.Errorf("call count: got %d, want 2", got)
	}
	// With Retry-After: 0 honored, the gap should be near-zero (well below
	// retryBaseDelay). Allow generous slack for goroutine scheduling.
	if gap > 100*time.Millisecond {
		t.Errorf("gap %v; expected ~0 because Retry-After: 0 means retry immediately", gap)
	}
}

func TestIsRetriableNetErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context cancelled", context.Canceled, false},
		// Bare DeadlineExceeded satisfies net.Error.Timeout(), so it's retried.
		// Real user cancellation is caught by Complete's loop-top ctx.Err() check.
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"io.EOF", io.EOF, true},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"syscall.ECONNRESET", syscall.ECONNRESET, true},
		{"syscall.EPIPE", syscall.EPIPE, true},
		{"random error", errors.New("nope"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetriableNetErr(tc.err); got != tc.want {
				t.Errorf("isRetriableNetErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsRetriableStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, false}, // 500 not retried — could be deterministic upstream bug
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			if got := isRetriableStatus(tc.code); got != tc.want {
				t.Errorf("isRetriableStatus(%d) = %v, want %v", tc.code, got, tc.want)
			}
		})
	}
}

// ── Circuit breaker smoke tests ───────────────────────────────────────────────

// TestComplete_CircuitOpen_FailsFast verifies Complete returns council.ErrCircuitOpen
// immediately (no HTTP call) when the circuit breaker is open.
func TestComplete_CircuitOpen_FailsFast(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(w, mockCompletion{Choices: []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}{{Message: struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "assistant", Content: "hello"}}}})
	}))
	defer srv.Close()

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		WindowDuration:   time.Minute,
		ResetTimeout:     time.Hour,
	})
	cb.RecordFailure() // force open

	c := testClient("key", srv)
	c.cb = cb

	_, err := c.Complete(context.Background(), council.CompletionRequest{Model: "m"})
	if !errors.Is(err, council.ErrCircuitOpen) {
		t.Errorf("want council.ErrCircuitOpen, got %v", err)
	}
	if called {
		t.Error("HTTP server should not have been called when circuit is open")
	}
}

// TestComplete_CircuitClosed_RecordsSuccess verifies a successful call transitions
// the CB through half-open to closed.
func TestComplete_CircuitClosed_RecordsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, mockCompletion{Choices: []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}{{Message: struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "assistant", Content: "ok"}}}})
	}))
	defer srv.Close()

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		WindowDuration:   time.Minute,
		ResetTimeout:     10 * time.Millisecond,
	})
	cb.RecordFailure() // open
	time.Sleep(20 * time.Millisecond)
	// Now in half-open — one probe allowed.

	c := testClient("key", srv)
	c.cb = cb

	_, err := c.Complete(context.Background(), council.CompletionRequest{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CB should now be closed — allow a second call.
	if !cb.Allow() {
		t.Error("circuit should be closed after successful probe")
	}
}
