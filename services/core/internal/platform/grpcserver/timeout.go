package grpcserver

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// timeoutInterceptor bounds every handler with a deadline so a stuck query or
// downstream call fails predictably instead of holding a goroutine forever.
// Context-aware I/O (pgx, redis, downstream gRPC) aborts promptly on expiry.
// Non-positive disables enforcement so a service can opt out via config.
func timeoutInterceptor(timeout time.Duration) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if timeout <= 0 {
			return handler(ctx, req)
		}

		tctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return handler(tctx, req)
	}
}
