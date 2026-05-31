package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"

	"github.com/valpere/vmm-rada/internal/council"
)

const (
	defaultURL    = "https://openrouter.ai/api/v1/chat/completions"
	maxBodyBytes  = 4 * 1024 * 1024  // 4 MiB cap on response bodies
	maxRetryAfter = 30 * time.Second // ceiling applied to Retry-After header values

	defaultRetryBaseDelay              = 500 * time.Millisecond
	defaultMaxCumulativeBackoffDuration = 60 * time.Second
)

// retryAfterAbsent is the sentinel returned by parseRetryAfter when no usable
// Retry-After value is present. A genuine "Retry-After: 0" maps to 0
// (retry immediately), distinct from absent.
const retryAfterAbsent time.Duration = -1

// APIError is returned when OpenRouter responds with a non-200 status code.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openrouter: API error %d: %s", e.StatusCode, e.Body)
}

// Client sends completion requests to the OpenRouter API.
type Client struct {
	apiKey     string
	baseURL    string // overridable in tests; defaults to defaultURL
	http       *http.Client
	maxRetries int          // total retries (1 initial attempt + maxRetries retries)
	logger     *slog.Logger // never nil — NewClient substitutes slog.Default()
	cb         *CircuitBreaker // optional; nil disables circuit breaking

	// retryBaseDelay is the first attempt's nominal backoff. Per-Client so tests
	// can shrink it without mutating package-level state and breaking parallel runs.
	// Production initialises this to defaultRetryBaseDelay.
	retryBaseDelay time.Duration

	// maxCumulativeBackoffDuration caps the total time spent sleeping across
	// retry attempts in a single Complete call. Per-Client for the same reason.
	maxCumulativeBackoffDuration time.Duration
}

// NewClient creates a Client with the given API key, base URL, HTTP timeout,
// retry budget, logger, and optional circuit breaker. baseURL overrides the
// default OpenRouter endpoint; pass "" to use the default. maxRetries of 0
// means a single attempt (no retries). A nil logger falls back to
// slog.Default(). A nil cb disables circuit breaking.
func NewClient(apiKey, baseURL string, timeout time.Duration, maxRetries int, logger *slog.Logger, cb *CircuitBreaker) *Client {
	if baseURL == "" {
		baseURL = defaultURL
	}
	if logger == nil {
		logger = slog.Default()
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &Client{
		apiKey:                       apiKey,
		baseURL:                      baseURL,
		http:                         &http.Client{Timeout: timeout},
		maxRetries:                   maxRetries,
		logger:                       logger,
		cb:                           cb,
		retryBaseDelay:               defaultRetryBaseDelay,
		maxCumulativeBackoffDuration: defaultMaxCumulativeBackoffDuration,
	}
}

// Compile-time assertion: Client implements council.LLMClient.
var _ council.LLMClient = (*Client)(nil)

// Complete POSTs a chat completion request to OpenRouter and returns the response.
// On transient failures (HTTP 429/502/503/504, network blips), it retries with
// exponential backoff and ±25% jitter, honoring Retry-After headers (capped at
// 30 s) and a cumulative 60 s sleep budget. Returns *APIError on non-200
// responses after retries are exhausted.
//
// If a circuit breaker is configured and open, Complete returns council.ErrCircuitOpen
// immediately without making an HTTP call.
func (c *Client) Complete(ctx context.Context, req council.CompletionRequest) (council.CompletionResponse, error) {
	if c.cb != nil && !c.cb.Allow() {
		return council.CompletionResponse{}, council.ErrCircuitOpen
	}

	body, err := json.Marshal(req)
	if err != nil {
		return council.CompletionResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	var (
		cumulativeBackoff time.Duration
		lastErr           error
	)
	for attempt := 0; ; attempt++ {
		// Honour a context cancellation observed before issuing the next request.
		if cerr := ctx.Err(); cerr != nil {
			return council.CompletionResponse{}, cerr
		}

		resp, attemptErr := c.doAttempt(ctx, body)
		shouldRetry, retryAfter, finalErr := c.classifyAttempt(resp, attemptErr)

		if !shouldRetry {
			// Either we have a successful response, an unmarshal error, or a
			// non-retryable failure. classifyAttempt returns the right value
			// in finalErr (and resp on success) for us to surface.
			if finalErr != nil {
				if attempt > 0 {
					c.logger.Info("openrouter: failed after retries",
						"attempts", attempt+1, "final_error", finalErr)
				}
				if c.cb != nil {
					c.cb.RecordFailure()
				}
				return council.CompletionResponse{}, finalErr
			}
			// Successful 200 + decoded body.
			result, decErr := c.decodeBody(resp)
			if decErr == nil && c.cb != nil {
				c.cb.RecordSuccess()
			}
			return result, decErr
		}

		// Retryable. lastErr always tracks the most recent reason for retry so
		// we can surface it if the cap or maxRetries forces a final return.
		lastErr = finalErr

		if attempt >= c.maxRetries {
			// Defence in depth: if the user just cancelled, prefer their error
			// over the last attempt's error.
			if cerr := ctx.Err(); cerr != nil {
				return council.CompletionResponse{}, cerr
			}
			c.logger.Info("openrouter: retries exhausted",
				"attempts", attempt+1, "final_error", lastErr)
			if c.cb != nil {
				c.cb.RecordFailure()
			}
			return council.CompletionResponse{}, lastErr
		}

		delay := c.backoffDelay(attempt, retryAfter)
		if cumulativeBackoff+delay > c.maxCumulativeBackoffDuration {
			c.logger.Info("openrouter: cumulative backoff cap reached",
				"cumulative_ms", cumulativeBackoff.Milliseconds(),
				"final_error", lastErr)
			if c.cb != nil {
				c.cb.RecordFailure()
			}
			return council.CompletionResponse{}, lastErr
		}
		cumulativeBackoff += delay

		c.logger.Debug("openrouter: retrying",
			"attempt", attempt+1, "delay_ms", delay.Milliseconds(),
			"cause", lastErr)

		select {
		case <-ctx.Done():
			return council.CompletionResponse{}, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// doAttempt performs a single HTTP request. On HTTP-level responses, it leaves
// resp.Body open so the caller can decide whether to drain (retry path) or
// read (non-retry path). On network errors it returns err only.
func (c *Client) doAttempt(ctx context.Context, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/valpere/vmm-rada")
	httpReq.Header.Set("X-Title", "VMM Rada")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	return resp, nil
}

// classifyAttempt inspects the result of doAttempt and decides whether to
// retry. On a retryable status it reads (then drains and closes) the body so
// that the final *APIError can carry the body text if retries are exhausted.
// On a non-retryable status it surfaces *APIError with whatever body bytes were
// readable — partial body is preferable to retrying a non-retryable status
// just because the body read hiccupped.
func (c *Client) classifyAttempt(resp *http.Response, err error) (shouldRetry bool, retryAfter time.Duration, finalErr error) {
	// Network-level error path.
	if err != nil {
		if isRetriableNetErr(err) {
			return true, retryAfterAbsent, err
		}
		return false, retryAfterAbsent, err
	}

	// HTTP-level path.
	if isRetriableStatus(resp.StatusCode) {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		// If the body read was cancelled by the user, propagate that immediately
		// — explicit cancellation is never retried, even on a retryable status.
		if errors.Is(readErr, context.Canceled) {
			return false, retryAfterAbsent, readErr
		}

		return true, retryAfter, &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	// Non-retryable status (4xx other than 429, 5xx other than 502/503/504, or 200).
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Surface *APIError with whatever body was readable. A partial read on a
		// non-retryable status code does not justify retrying — the status code
		// itself was already non-retryable.
		return false, retryAfterAbsent, &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	// Status 200. A body read failure is the actual problem.
	if readErr != nil {
		if errors.Is(readErr, context.Canceled) {
			return false, retryAfterAbsent, readErr
		}
		if isRetriableNetErr(readErr) {
			return true, retryAfterAbsent, fmt.Errorf("read response: %w", readErr)
		}
		return false, retryAfterAbsent, fmt.Errorf("read response: %w", readErr)
	}

	// Success — stash the body on resp so decodeBody can read it without re-reading.
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	return false, retryAfterAbsent, nil
}

// decodeBody parses a successful response body into a CompletionResponse.
func (c *Client) decodeBody(resp *http.Response) (council.CompletionResponse, error) {
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return council.CompletionResponse{}, fmt.Errorf("read response: %w", err)
	}
	var completionResp council.CompletionResponse
	if err := json.Unmarshal(respBody, &completionResp); err != nil {
		return council.CompletionResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return completionResp, nil
}

// isRetriableStatus returns true for HTTP status codes worth retrying:
// 429 (rate limit), 502/503/504 (transient upstream errors). HTTP 500 is
// deliberately excluded — it often indicates a deterministic upstream bug
// rather than a transient hiccup.
func isRetriableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// isRetriableNetErr returns true for network-level errors worth retrying:
// timeouts (including http.Client.Timeout), connection resets, broken pipes,
// EOFs from closed keep-alive connections.
//
// User-context cancellation is NOT classified here — Complete checks ctx.Err()
// at the top of each iteration before retrying, so any user-cancelled context
// short-circuits before this function would observe it. We deliberately treat
// net.Error.Timeout() as retriable even when its underlying cause unwraps to
// context.DeadlineExceeded, because http.Client.Timeout fires that exact shape
// and we want it to retry.
func isRetriableNetErr(err error) bool {
	if err == nil {
		return false
	}
	// Explicit user cancellation is never retried — no point continuing if the
	// caller has given up. (DeadlineExceeded is intentionally not rejected here:
	// see the doc comment above.)
	if errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	return false
}

// parseRetryAfter parses an HTTP Retry-After header. Supports both
// delta-seconds (integer) and HTTP-date forms. Returns retryAfterAbsent (-1)
// when no usable value is present so callers can distinguish "no header" from
// an explicit "Retry-After: 0" (= retry immediately, RFC 7231). Values are
// capped at maxRetryAfter to prevent the gateway from forcing arbitrarily
// long client waits.
func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return retryAfterAbsent
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs < 0 {
			return retryAfterAbsent
		}
		d := time.Duration(secs) * time.Second
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d < 0 {
			return retryAfterAbsent
		}
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	return retryAfterAbsent
}

// backoffDelay returns the next sleep duration. If retryAfter is present
// (>= 0), it is used directly — including 0 to honour an explicit
// "Retry-After: 0" (retry immediately). Otherwise: c.retryBaseDelay * 3^attempt
// with ±25% jitter via math/rand/v2 (Go 1.22+ — no global lock).
func (c *Client) backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter >= 0 {
		return retryAfter
	}
	base := c.retryBaseDelay
	for range attempt {
		base *= 3
	}
	if base <= 0 {
		return c.retryBaseDelay
	}
	// Jitter range: [-base/4, +base/4].
	jitter := time.Duration(rand.Int64N(int64(base)/2)) - base/4
	return base + jitter
}
