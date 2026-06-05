// Package shutdown coordinates graceful teardown of the server and worker on
// SIGINT/SIGTERM: drain in-flight work, flush telemetry, close pools, in the
// right order, so stopping a container never cuts a request mid-flight.
package shutdown

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

// hook pairs a cleanup step with a name so the log shows exactly which step
// ran and how long it took.
type hook struct {
	name string
	run  func(ctx context.Context) error
}

// Manager collects shutdown hooks and runs them on signal. The mutex guards
// the slice against registration from concurrent startup goroutines.
type Manager struct {
	mu      sync.Mutex
	log     zerolog.Logger
	hooks   []hook
	timeout time.Duration
}

// NewManager creates a Manager. timeout caps total drain time so a stuck
// dependency cannot hang shutdown forever; non-positive means 15s.
func NewManager(log zerolog.Logger, timeout time.Duration) *Manager {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Manager{log: log, timeout: timeout}
}

// Register adds a named cleanup hook. Hooks run in reverse registration
// order; see Run.
func (m *Manager) Register(name string, run func(ctx context.Context) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = append(m.hooks, hook{name: name, run: run})
}

// WaitForSignal blocks until SIGINT or SIGTERM, then runs all hooks.
func (m *Manager) WaitForSignal() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	m.log.Info().Msg("shutdown signal received, draining")
	m.Run()
}

// Run executes hooks LIFO. Dependencies are registered outermost-first at
// startup (server, then bus, then DB pool), so reverse order stops accepting
// requests before closing what those requests depend on.
func (m *Manager) Run() {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	// Snapshot under lock, release before running: hooks may be slow and the
	// lock must never be held during I/O.
	m.mu.Lock()
	snapshot := make([]hook, len(m.hooks))
	copy(snapshot, m.hooks)
	m.mu.Unlock()

	for i := len(snapshot) - 1; i >= 0; i-- {
		h := snapshot[i]
		start := time.Now()
		if err := h.run(ctx); err != nil {
			m.log.Error().Err(err).Str("hook", h.name).Msg("shutdown hook failed")
			continue
		}
		m.log.Info().Str("hook", h.name).Dur("took", time.Since(start)).Msg("shutdown hook done")
	}
	m.log.Info().Msg("shutdown complete")
}
