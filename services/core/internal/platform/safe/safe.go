// Package safe runs background goroutines with panic recovery, so one
// panicking background task logs its stack and dies alone instead of taking
// the whole process down. Request-path panics are already covered by the
// grpcserver recovery interceptor; this is for everything outside a request.
package safe

import (
	"context"
	"runtime/debug"

	"github.com/rs/zerolog"
)

// Go runs fn on a new goroutine, logging any panic with its stack under the
// given name.
func Go(log zerolog.Logger, name string, fn func()) {
	go func() {
		defer recoverPanic(log, name)
		fn()
	}()
}

// GoCtx is Go for context-aware functions whose error is worth a log line
// (background pumps, consumers). context.Canceled is expected on shutdown
// and not logged.
func GoCtx(ctx context.Context, log zerolog.Logger, name string, fn func(ctx context.Context) error) {
	go func() {
		defer recoverPanic(log, name)
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Str("task", name).Msg("background task failed")
		}
	}()
}

func recoverPanic(log zerolog.Logger, name string) {
	if r := recover(); r != nil {
		log.Error().
			Interface("panic", r).
			Str("task", name).
			Bytes("stack", debug.Stack()).
			Msg("recovered from panic in background task")
	}
}
