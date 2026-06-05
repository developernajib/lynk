package grpcserver

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/developernajib/lynk/services/core/internal/platform/apperror"
	"github.com/developernajib/lynk/services/core/internal/platform/logger"
)

// errorMappingInterceptor converts *apperror.Error values into gRPC statuses
// and emits the fingerprinted error log that powers issue grouping in the log
// aggregator. It sits close to the handler so it sees the raw returned error;
// anything that is not an application error passes through untouched (an
// unknown raw error surfaces upstream as codes.Unknown).
func errorMappingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}

		appErr, ok := apperror.As(err)
		if !ok {
			return resp, err
		}

		// Same error class, same fingerprint: the aggregator groups, counts,
		// and flags regressions without an external error-tracking service.
		reqLogger := logger.FromContext(ctx)
		reqLogger.Error().
			Err(err).
			Str("error.kind", appErr.Kind.String()).
			Str("error.code", appErr.Code).
			Str("error.fingerprint", appErr.Fingerprint()).
			Msg("application error")

		// The status carries only the safe public message.
		return resp, status.Error(toGRPCCode(appErr.Kind), appErr.Message)
	}
}

// toGRPCCode maps the transport-agnostic Kind to the closest gRPC code. The
// translation lives here, at the transport boundary, never in the domain.
func toGRPCCode(kind apperror.Kind) codes.Code {
	switch kind {
	case apperror.KindInvalidInput:
		return codes.InvalidArgument
	case apperror.KindNotFound:
		return codes.NotFound
	case apperror.KindAlreadyExists:
		return codes.AlreadyExists
	case apperror.KindUnauthenticated:
		return codes.Unauthenticated
	case apperror.KindPermissionDenied:
		return codes.PermissionDenied
	case apperror.KindConflict:
		return codes.Aborted
	case apperror.KindRateLimited:
		return codes.ResourceExhausted
	case apperror.KindUnavailable:
		return codes.Unavailable
	default:
		return codes.Internal
	}
}
