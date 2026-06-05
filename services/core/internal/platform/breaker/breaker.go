// Package breaker implements a circuit breaker for calls to flaky
// dependencies: after enough consecutive failures it fails fast instead of
// piling more load onto a struggling downstream, then probes periodically and
// closes again once a probe succeeds.
package breaker

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrOpen is returned without calling the dependency while the circuit is
// open. Callers can errors.Is on it to degrade gracefully.
var ErrOpen = errors.New("breaker: circuit open")

// State is the circuit's position.
type State int

const (
	// StateClosed lets all calls through (healthy).
	StateClosed State = iota
	// StateOpen fails fast without calling the dependency.
	StateOpen
	// StateHalfOpen lets a bounded number of probe calls through.
	StateHalfOpen
)

// String returns a stable lowercase name for logs and metrics.
func (s State) String() string {
	switch s {
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

// Config tunes a Breaker. Zero values take the documented defaults.
type Config struct {
	// FailureThreshold is the consecutive-failure count that opens the
	// circuit. Default 5.
	FailureThreshold int
	// OpenTimeout is how long the circuit stays open before allowing probe
	// calls. Default 30s.
	OpenTimeout time.Duration
	// HalfOpenMax caps concurrent probe calls in the half-open state.
	// Default 1.
	HalfOpenMax int
}

// Breaker is a thread-safe circuit breaker.
type Breaker struct {
	cfg Config
	// now is injectable so state transitions are testable without sleeping.
	now func() time.Time

	mu       sync.Mutex
	state    State
	failures int
	openedAt time.Time
	probes   int
}

// New returns a closed Breaker with defaults applied.
func New(cfg Config) *Breaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.OpenTimeout <= 0 {
		cfg.OpenTimeout = 30 * time.Second
	}
	if cfg.HalfOpenMax <= 0 {
		cfg.HalfOpenMax = 1
	}
	return &Breaker{cfg: cfg, now: time.Now}
}

// State reports the current state, transitioning open to half-open when the
// open timeout has elapsed.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()
	return b.state
}

// Do runs fn through the circuit. While open it returns ErrOpen immediately.
// Caller cancellation (context.Canceled) does not count against the
// dependency: it says nothing about downstream health. Deadline expiry does
// count, since a slow dependency is exactly what the breaker watches for.
func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := b.allow(); err != nil {
		return err
	}

	err := fn(ctx)

	if err != nil && !errors.Is(err, context.Canceled) {
		b.recordFailure()
		return err
	}
	b.recordSuccess()
	return err
}

// allow admits or rejects a call under the current state.
func (b *Breaker) allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()

	switch b.state {
	case StateOpen:
		return ErrOpen
	case StateHalfOpen:
		if b.probes >= b.cfg.HalfOpenMax {
			return ErrOpen
		}
		b.probes++
		return nil
	default:
		return nil
	}
}

// refresh moves open to half-open once the open timeout has elapsed. Callers
// must hold the mutex.
func (b *Breaker) refresh() {
	if b.state == StateOpen && b.now().Sub(b.openedAt) >= b.cfg.OpenTimeout {
		b.state = StateHalfOpen
		b.probes = 0
	}
}

func (b *Breaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// One successful probe closes the circuit; in closed state a success
	// resets the consecutive-failure run.
	if b.state == StateHalfOpen {
		b.state = StateClosed
	}
	b.failures = 0
}

func (b *Breaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// A failed probe reopens immediately: the dependency is still down.
	if b.state == StateHalfOpen {
		b.open()
		return
	}
	b.failures++
	if b.failures >= b.cfg.FailureThreshold {
		b.open()
	}
}

// open transitions to open and stamps the time the timeout counts from.
// Callers must hold the mutex.
func (b *Breaker) open() {
	b.state = StateOpen
	b.openedAt = b.now()
	b.failures = 0
	b.probes = 0
}
