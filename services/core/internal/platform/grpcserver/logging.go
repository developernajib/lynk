package grpcserver

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"

	"github.com/developernajib/lynk/services/core/internal/platform/apperror"
	"github.com/developernajib/lynk/services/core/internal/platform/logger"
)

// slowRequestThreshold is the duration above which a successful call is
// logged as a warning, surfacing latency regressions without logging every
// request.
const slowRequestThreshold = 1 * time.Second

// loggingInterceptor attaches the request-scoped logger to the context and
// logs only exceptions: unexpected errors and abnormally slow calls.
//
// At high request volume, logging every successful call is an allocation and
// I/O cost with no signal: counts and latency come from metrics (the otelgrpc
// stats handler), and application errors are already fingerprint-logged by
// the error-mapping interceptor, so nothing is logged twice.
func loggingInterceptor(base zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()

		lctx := base.With().
			Str("method", info.FullMethod).
			Str("request_id", RequestIDFromContext(ctx))

		// Stamping trace/span ids on every line is what links a log line in
		// the aggregator to the full distributed trace and back. The span
		// context comes from the incoming traceparent metadata, extracted by
		// the stats handler before interceptors run.
		if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
			lctx = lctx.
				Str("trace_id", sc.TraceID().String()).
				Str("span_id", sc.SpanID().String())
		}

		reqLogger := lctx.Logger()
		ctx = logger.IntoContext(ctx, reqLogger)

		resp, err := handler(ctx, req)
		elapsed := time.Since(start)

		switch {
		case err != nil:
			// Application errors were already logged by errorMap; log only
			// unexpected ones so nothing is lost and nothing is duplicated.
			if _, isAppError := apperror.As(err); !isAppError {
				reqLogger.Error().Err(err).Dur("duration", elapsed).Msg("unexpected grpc error")
			}
		case elapsed > slowRequestThreshold:
			reqLogger.Warn().Dur("duration", elapsed).Msg("slow grpc request")
		}

		return resp, err
	}
}
