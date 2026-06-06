package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/developernajib/lynk/services/core/internal/platform/jwt"
)

// Trusted identity headers the gateway injects after verifying the JWT.
// Downstream services read these instead of re-verifying. Lowercase because
// gRPC canonicalizes metadata keys.
const (
	headerUserID    = "x-user-id"
	headerTenantID  = "x-tenant-id"
	headerBranchID  = "x-branch-id"
	headerRole      = "x-role"
	headerTokenType = "x-token-type"
)

// These interceptors populate the context when an identity is present and
// otherwise pass through unauthenticated. They never reject: whether an RPC
// requires auth (or a permission) is a per-handler guard decision, because
// some endpoints (login itself, public reads) are intentionally open.

// VerifyTokenInterceptor runs at the gateway: it verifies the bearer token's
// RS256 signature once at the edge so inner services never pay the crypto.
func VerifyTokenInterceptor(verifier *jwt.Verifier) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		token := bearerToken(ctx)
		if token == "" {
			return handler(ctx, req)
		}

		claims, err := verifier.Verify(token)
		if err != nil {
			// Invalid or expired token: continue without a principal rather
			// than failing hard, so public RPCs still work; protected
			// handlers deny via their guard.
			return handler(ctx, req)
		}

		p := Principal{
			UserID:    claims.Subject,
			TenantID:  claims.TenantID,
			BranchID:  claims.BranchID,
			Role:      claims.Role,
			TokenType: string(claims.TokenType),
		}
		return handler(IntoContext(ctx, p), req)
	}
}

// TrustedHeaderInterceptor runs in downstream services: it rebuilds the
// Principal from the gateway-injected headers, no signature check, because
// the mTLS edge already proved the caller is the trusted gateway.
func TrustedHeaderInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		return handler(withPrincipalFromMetadata(ctx), req)
	}
}

// TrustedHeaderStreamInterceptor is the stream-side twin. Streams bypass the
// unary chain entirely, so any service exposing server streams must install
// this or its streams run unauthenticated.
func TrustedHeaderStreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		// grpc.ServerStream exposes its context only through Context(), so
		// injecting a principal means wrapping the stream with an override.
		wrapped := &principalServerStream{ServerStream: ss, ctx: withPrincipalFromMetadata(ss.Context())}
		return handler(srv, wrapped)
	}
}

type principalServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *principalServerStream) Context() context.Context { return s.ctx }

// withPrincipalFromMetadata reads the trusted headers and attaches a
// Principal when an identity is present; otherwise it returns ctx unchanged
// (anonymous call, e.g. internal health or event paths).
func withPrincipalFromMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}

	userID := firstValue(md, headerUserID)
	if userID == "" {
		return ctx
	}

	return IntoContext(ctx, Principal{
		UserID:    userID,
		TenantID:  firstValue(md, headerTenantID),
		BranchID:  firstValue(md, headerBranchID),
		Role:      firstValue(md, headerRole),
		TokenType: firstValue(md, headerTokenType),
	})
}

// bearerToken extracts the token from "authorization: Bearer <token>". The
// prefix match is case-insensitive per the HTTP spec.
func bearerToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	header := firstValue(md, "authorization")
	if remainder, found := strings.CutPrefix(header, "Bearer "); found {
		return remainder
	}
	if remainder, found := strings.CutPrefix(header, "bearer "); found {
		return remainder
	}
	return ""
}

func firstValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
