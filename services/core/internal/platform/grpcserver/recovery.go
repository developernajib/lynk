package grpcserver

import (
	"context"
	"runtime/debug"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recoveryInterceptor is the outermost unary interceptor: it turns a panic
// anywhere in the call stack into a clean Internal error instead of killing
// the process. The stack goes to the log, never to the client.
func recoveryInterceptor(log zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		// Named returns let the deferred recover rewrite the result.
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Interface("panic", r).
					Str("method", info.FullMethod).
					Bytes("stack", debug.Stack()).
					Msg("recovered from panic in grpc handler")
				err = status.Error(codes.Internal, "internal server error")
			}
		}()

		return handler(ctx, req)
	}
}

// streamRecoveryInterceptor is the stream-side twin: streams bypass the unary
// chain, so they need their own panic guard.
func streamRecoveryInterceptor(log zerolog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Interface("panic", r).
					Str("method", info.FullMethod).
					Bytes("stack", debug.Stack()).
					Msg("recovered from panic in grpc stream handler")
				err = status.Error(codes.Internal, "internal server error")
			}
		}()

		return handler(srv, ss)
	}
}
