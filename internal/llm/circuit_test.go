package llm

import (
	"testing"
	"time"
)

func TestCircuitBreaker_ClosedAllowsRequests(t *testing.T) {
	cb := NewCircuitBreaker()
	if !cb.Allow() {
		t.Fatal("closed circuit should allow requests")
	}
	if cb.State() != CircuitClosed {
		t.Fatal("initial state should be closed")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker()
	cb.failureThreshold = 3

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after %d failures, got %v", 3, cb.State())
	}
	if cb.Allow() {
		t.Fatal("open circuit should reject requests")
	}
}

func TestCircuitBreaker_HalfOpenAfterDuration(t *testing.T) {
	cb := NewCircuitBreaker()
	cb.failureThreshold = 2
	cb.openDuration = 10 * time.Millisecond

	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatal("should be open")
	}

	time.Sleep(15 * time.Millisecond)

	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after duration, got %v", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("half-open should allow requests")
	}
}

func TestCircuitBreaker_ClosesAfterSuccessInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker()
	cb.failureThreshold = 2
	cb.successThreshold = 2
	cb.openDuration = 10 * time.Millisecond

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)

	// Half-open — record successes.
	cb.Allow() // triggers half-open transition
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after successes, got %v", cb.State())
	}
}

func TestCircuitBreaker_SuccessResetFailureCount(t *testing.T) {
	cb := NewCircuitBreaker()
	cb.failureThreshold = 3

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // resets failure count

	cb.RecordFailure()
	cb.RecordFailure()
	// Only 2 consecutive failures, not 3
	if cb.State() != CircuitClosed {
		t.Fatal("should still be closed after reset")
	}
}
