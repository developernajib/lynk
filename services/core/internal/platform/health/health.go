// Package health serves liveness and readiness probes over plain HTTP on the
// ops port.
//
//   - /healthz (liveness): is the process up? No dependency checks. A failure
//     makes the orchestrator restart the pod.
//   - /readyz (readiness): are dependencies reachable? A failure pulls the pod
//     out of rotation without killing it, so a brief database blip never
//     triggers a restart loop.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Check is one named readiness probe. Returning an error rather than a bool
// lets the response body say what failed.
type Check func(ctx context.Context) error

// Server owns the ops HTTP server and its registered readiness checks.
type Server struct {
	httpServer *http.Server
	mux        *http.ServeMux
	checks     map[string]Check
}

// NewServer builds the ops server on the given port. Register checks and
// Mount extras before Start.
func NewServer(port string) *Server {
	s := &Server{checks: make(map[string]Check)}

	mux := http.NewServeMux()
	s.mux = mux
	mux.HandleFunc("/healthz", s.handleLiveness)
	mux.HandleFunc("/readyz", s.handleReadiness)

	s.httpServer = &http.Server{
		Addr:    ":" + port,
		Handler: mux,
		// An ops endpoint must never be a way to exhaust sockets.
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	return s
}

// Register adds a named readiness check such as "postgres" or "redis".
func (s *Server) Register(name string, check Check) {
	s.checks[name] = check
}

// Mount attaches an extra handler to the ops mux, e.g. "/metrics". The ops
// port is the right home: probes and scrapes are operator traffic and must
// never share the public listener. Call before Start; ServeMux registration
// is not synchronized with serving.
func (s *Server) Mount(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

// Start runs the HTTP server and blocks; run it in its own goroutine.
// http.ErrServerClosed is the normal result of Stop, not a failure.
func (s *Server) Start() error {
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop gracefully shuts the ops server down, registered as a shutdown hook.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadiness runs every check and returns 200 only when all pass,
// otherwise 503 with a per-dependency breakdown.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	// Bound the pass so a hung dependency cannot hang the probe itself.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	results := make(map[string]string, len(s.checks))
	healthy := true
	for name, check := range s.checks {
		if err := check(ctx); err != nil {
			results[name] = "down: " + err.Error()
			healthy = false
			continue
		}
		results[name] = "ok"
	}

	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, results)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed encode of a small fixed map leaves a truncated body; the
	// status code already told the story.
	_ = json.NewEncoder(w).Encode(body)
}
