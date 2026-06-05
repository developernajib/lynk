package grpcserver

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

type requestIDContextKey struct{}

// requestIDMetadataKey is the metadata header trusted upstreams (the gateway)
// use to propagate an existing correlation id. Lowercase because gRPC
// metadata keys are canonicalized to lowercase.
const requestIDMetadataKey = "x-request-id"

// RequestIDFromContext returns the correlation id attached to ctx, or "".
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return id
	}
	return ""
}

// requestIDInterceptor reuses an incoming x-request-id or mints one, and
// stores it in the context so every layer logs the same correlation id.
func requestIDInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		id := incomingRequestID(ctx)
		if id == "" {
			generated, err := secure.UUIDv7()
			if err != nil {
				// An entropy failure must not fail the request over a log id;
				// a sentinel keeps the field grep-able.
				generated = "unavailable"
			}
			id = generated
		}
		return handler(context.WithValue(ctx, requestIDContextKey{}, id), req)
	}
}

func incomingRequestID(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(requestIDMetadataKey); len(values) > 0 {
			return values[0]
		}
	}
	return ""
}
