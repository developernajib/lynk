// Package edge implements the gateway's HTTP edge: the hardened middleware
// chain, edge authentication, three-level rate limiting, and the
// gRPC-Web ↔ gRPC bridge. Every public request enters the system here.
package edge

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"github.com/developernajib/lynk/services/gateway/internal/platform/logger"
	"github.com/developernajib/lynk/services/gateway/internal/platform/secure"
)

// Middleware wraps an http.Handler to add one cross-cutting behavior, the
// standard Go middleware shape so everything composes with the ecosystem.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so the FIRST listed runs outermost.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDFromContext returns the correlation id for the current request.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// Recovery is the outermost middleware: a panic in any inner handler becomes
// a clean 500 instead of a dead gateway process.
func Recovery(log zerolog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error().Interface("panic", rec).Str("path", r.URL.Path).Msg("recovered panic")
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID reuses an inbound X-Request-Id or mints one (UUIDv7), storing it
// in the context and echoing it back so clients and proxies can correlate.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if id == "" {
				generated, err := secure.UUIDv7()
				if err != nil {
					generated = "unavailable"
				}
				id = generated
			}
			w.Header().Set("X-Request-Id", id)
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Logger attaches a per-request logger to the context and logs only
// exceptions (5xx and slow requests): at high volume, per-request counts and
// latency belong to metrics, not logs. Trace and span ids are stamped on
// every line for log-to-trace navigation.
func Logger(log zerolog.Logger) Middleware {
	const slowThreshold = 1 * time.Second
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			lctx := log.With().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("request_id", RequestIDFromContext(r.Context()))

			if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
				lctx = lctx.
					Str("trace_id", sc.TraceID().String()).
					Str("span_id", sc.SpanID().String())
			}
			reqLogger := lctx.Logger()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(logger.IntoContext(r.Context(), reqLogger)))

			elapsed := time.Since(start)
			switch {
			case rec.status >= 500:
				reqLogger.Error().Int("status", rec.status).Dur("duration", elapsed).Msg("server error")
			case elapsed > slowThreshold:
				reqLogger.Warn().Int("status", rec.status).Dur("duration", elapsed).Msg("slow request")
			}
		})
	}
}

// BodyLimit rejects bodies above the cap (memory-exhaustion DoS guard).
// MaxBytesReader enforces lazily as the body is read.
func BodyLimit(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout bounds total handling time with a 503 on overrun so a slow handler
// cannot pin a connection indefinitely.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, "request timed out")
	}
}

// statusRecorder remembers the status code a handler wrote, which the stdlib
// does not otherwise expose to middleware.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
