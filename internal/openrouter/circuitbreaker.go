package openrouter

import (
	"sync"
	"time"
)

type cbState int

const (
	cbClosed   cbState = iota
	cbOpen
	cbHalfOpen
)

// CircuitBreakerConfig holds the tuneable parameters for the circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int
	WindowDuration   time.Duration
	ResetTimeout     time.Duration
}

// CircuitBreaker implements the three-state (closed → open → half-open → closed)
// pattern with a sliding-window failure counter. All methods are safe for
// concurrent use.
//
// Transition rules:
//   - closed → open:      FailureThreshold terminal failures within WindowDuration
//   - open → half-open:   ResetTimeout elapsed since the circuit opened
//   - half-open → closed: one successful probe
//   - half-open → open:   one failed probe
type CircuitBreaker struct {
	cfg         CircuitBreakerConfig
	mu          sync.Mutex
	state       cbState
	failures    int
	windowStart time.Time
	openedAt    time.Time
}

// NewCircuitBreaker constructs a CircuitBreaker in the closed state.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		cfg:         cfg,
		state:       cbClosed,
		windowStart: time.Now(),
	}
}

// Allow reports whether a request should be attempted.
//   - Closed: always allowed.
//   - Open: denied unless ResetTimeout has elapsed, in which case the circuit
//     moves to half-open and one probe is allowed.
//   - Half-open: denied — only one probe is allowed at a time; subsequent calls
//     must wait for the probe to succeed or fail before the state changes.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Since(cb.openedAt) >= cb.cfg.ResetTimeout {
			cb.state = cbHalfOpen
			return true
		}
		return false
	case cbHalfOpen:
		return false
	}
	return false
}

// RecordSuccess records a successful terminal outcome.
// In half-open: closes the circuit and resets the failure window.
// In closed: no-op (success does not affect the failure counter).
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == cbHalfOpen {
		cb.state = cbClosed
		cb.failures = 0
		cb.windowStart = time.Now()
	}
}

// RecordFailure records a terminal failure (retries exhausted or non-retryable
// error). Per-attempt errors must NOT be recorded here; only call this when the
// entire Complete() call gives up.
//
// In closed: increments the sliding-window counter; opens the circuit when
// FailureThreshold is reached. Resets the window if WindowDuration has elapsed.
// In half-open: re-opens the circuit immediately.
// In open: no-op (already open).
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbClosed:
		now := time.Now()
		if now.Sub(cb.windowStart) >= cb.cfg.WindowDuration {
			cb.failures = 0
			cb.windowStart = now
		}
		cb.failures++
		if cb.failures >= cb.cfg.FailureThreshold {
			cb.state = cbOpen
			cb.openedAt = now
		}
	case cbHalfOpen:
		cb.state = cbOpen
		cb.openedAt = time.Now()
	}
}
