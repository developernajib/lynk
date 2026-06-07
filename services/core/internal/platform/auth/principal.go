// Package auth represents the authenticated caller (the principal) and the
// interceptors that establish it.
//
// Two trust models share one Principal type:
//   - The gateway verifies the RS256 JWT once at the edge and builds a
//     Principal from the claims.
//   - Downstream services sit behind the gateway and internal mTLS, so they
//     trust the identity the gateway injects as metadata headers instead of
//     re-verifying the signature on every hop; the mTLS edge already proved
//     the caller is the gateway.
package auth

import "context"

// Principal is the authenticated caller's identity for one request. Its
// fields are the SUBJECT ATTRIBUTES the ABAC policy engine evaluates;
// always derived from the verified token or trusted header, never from
// client-supplied request fields.
type Principal struct {
	// UserID is the subject id.
	UserID string
	// Role is a subject attribute for policy decisions (e.g. "admin").
	Role string
	// TokenType is "user", "admin", or "service".
	TokenType string
}

type principalContextKey struct{}

// IntoContext returns a child context carrying the principal.
func IntoContext(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

// FromContext returns the principal and whether one was present, so callers
// can distinguish an authenticated request from an anonymous one (a public
// read) without an ambiguous zero value.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(Principal)
	return p, ok
}

// IsAuthenticated reports whether someone is logged in, for guards that do
// not care who.
func IsAuthenticated(ctx context.Context) bool {
	_, ok := FromContext(ctx)
	return ok
}
