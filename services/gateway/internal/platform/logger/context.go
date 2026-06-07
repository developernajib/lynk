package logger

import (
	"context"
	"os"

	"github.com/rs/zerolog"
)

// fallback serves callers whose context carries no request logger (background
// jobs, startup). A real logger instead of nil means call sites never check.
var fallback = zerolog.New(os.Stderr).With().Timestamp().Logger()

// contextKey is unexported so no other package can collide with our key:
// context values are compared by key type and value.
type contextKey struct{}

var loggerKey = contextKey{}

// IntoContext returns a child context carrying the given request-scoped
// logger, typically already enriched with request_id, trace_id, and span_id
// so every layer logs with the same correlation fields.
func IntoContext(ctx context.Context, l zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext extracts the request logger, or a safe fallback when none was
// attached. Logging must never be the thing that crashes a request.
func FromContext(ctx context.Context) zerolog.Logger {
	if l, ok := ctx.Value(loggerKey).(zerolog.Logger); ok {
		return l
	}
	return fallback
}
