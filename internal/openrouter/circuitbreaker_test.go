package openrouter

import (
	"testing"
	"time"
)

func testCBConfig(threshold int, window, reset time.Duration) CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: threshold,
		WindowDuration:   window,
		ResetTimeout:     reset,
	}
}

// TestCB_InitialState_Allows verifies the circuit starts closed and allows calls.
func TestCB_InitialState_Allows(t *testing.T) {
	cb := NewCircuitBreaker(testCBConfig(3, time.Minute, time.Minute))
	if !cb.Allow() {
		t.Error("new circuit breaker should allow requests")
	}
}

// TestCB_OpenAfterThreshold verifies the circuit opens after FailureThreshold failures.
func TestCB_OpenAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(testCBConfig(3, time.Minute, time.Minute))
	for i := range 2 {
		cb.RecordFailure()
		if !cb.Allow() {
			t.Errorf("circuit should still be closed after %d failures", i+1)
		}
	}
	cb.RecordFailure() // 3rd failure — should open
	if cb.Allow() {
		t.Error("circuit should be open after reaching threshold")
	}
}

// TestCB_BlocksWhenOpen verifies that an open circuit rejects all calls.
func TestCB_BlocksWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(testCBConfig(1, time.Minute, time.Hour))
	cb.RecordFailure()
	for range 5 {
		if cb.Allow() {
			t.Error("open circuit should block requests")
		}
	}
}

// TestCB_TransitionsToHalfOpenAfterReset verifies the circuit moves to half-open
// after ResetTimeout and allows one probe.
func TestCB_TransitionsToHalfOpenAfterReset(t *testing.T) {
	cb := NewCircuitBreaker(testCBConfig(1, time.Minute, 10*time.Millisecond))
	cb.RecordFailure() // open
	if cb.Allow() {
		t.Fatal("circuit should be open immediately after failure")
	}
	time.Sleep(20 * time.Millisecond)
	if !cb.Allow() {
		t.Error("circuit should allow one probe after reset timeout")
	}
	// Second call while half-open: denied
	if cb.Allow() {
		t.Error("circuit should deny second call in half-open state")
	}
}

// TestCB_HalfOpen_SuccessCloses verifies a successful probe closes the circuit.
func TestCB_HalfOpen_SuccessCloses(t *testing.T) {
	cb := NewCircuitBreaker(testCBConfig(1, time.Minute, 10*time.Millisecond))
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	cb.Allow() // move to half-open
	cb.RecordSuccess()
	if !cb.Allow() {
		t.Error("circuit should be closed after successful probe")
	}
}

// TestCB_HalfOpen_FailureReopens verifies a failed probe re-opens the circuit.
func TestCB_HalfOpen_FailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(testCBConfig(1, time.Minute, 10*time.Millisecond))
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	cb.Allow() // move to half-open
	cb.RecordFailure()
	if cb.Allow() {
		t.Error("circuit should be open after failed probe")
	}
}

// TestCB_WindowReset verifies the failure counter resets when the window expires.
func TestCB_WindowReset(t *testing.T) {
	cb := NewCircuitBreaker(testCBConfig(3, 20*time.Millisecond, time.Hour))
	cb.RecordFailure()
	cb.RecordFailure() // 2 of 3 — still closed
	time.Sleep(30 * time.Millisecond)
	// Window expired — next failure starts a fresh window
	cb.RecordFailure() // 1 of 3 in new window
	if !cb.Allow() {
		t.Error("circuit should still be closed after window reset")
	}
}
