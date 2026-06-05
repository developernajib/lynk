package grpcserver

import (
	"context"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/developernajib/lynk/services/core/internal/platform/apperror"
)

// validationInterceptor enforces the constraints declared in the .proto
// contracts (protovalidate) on every inbound message before business logic
// runs: server-side input validation applied uniformly at the boundary.
func validationInterceptor() grpc.UnaryServerInterceptor {
	// The validator compiles constraint rules once at startup; a failure here
	// means bad constraints baked into the binary, a build error worth a
	// panic, same rationale as regexp.MustCompile on a literal.
	validator, err := protovalidate.New()
	if err != nil {
		panic("grpcserver: build protovalidator: " + err.Error())
	}

	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if msg, ok := req.(proto.Message); ok {
			if err := validator.Validate(msg); err != nil {
				// errorMap turns this into codes.InvalidArgument with a safe
				// public message.
				return nil, apperror.Wrap(err, apperror.KindInvalidInput, "invalid_input", "request validation failed")
			}
		}
		return handler(ctx, req)
	}
}
