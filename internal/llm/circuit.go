package llm

import (
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // healthy, requests flow through
	CircuitOpen                         // unhealthy, requests are rejected
	CircuitHalfOpen                     // testing recovery, limited requests allowed
)

// CircuitBreaker tracks provider health and prevents cascading failures.
type CircuitBreaker struct {
	mu               sync.Mutex
	state            CircuitState
	failures         int
	successes        int
	failureThreshold int           // consecutive failures before opening
	successThreshold int           // consecutive successes in half-open before closing
	openDuration     time.Duration // how long to stay open before trying half-open
	lastFailure      time.Time
}

// NewCircuitBreaker creates a circuit breaker with sensible defaults.
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: 5,
		successThreshold: 2,
		openDuration:     30 * time.Second,
	}
}

// Allow returns true if the circuit allows a request through.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if enough time has passed to try half-open.
		if time.Since(cb.lastFailure) >= cb.openDuration {
			cb.state = CircuitHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return false
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	if cb.state == CircuitHalfOpen {
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.state = CircuitClosed
		}
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()
	cb.successes = 0

	if cb.failures >= cb.failureThreshold {
		cb.state = CircuitOpen
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Check for auto-transition from open to half-open.
	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= cb.openDuration {
		cb.state = CircuitHalfOpen
	}
	return cb.state
}
