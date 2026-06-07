// auth.go is the one place a JWT is verified. The gateway then injects the
// caller's identity as trusted internal headers that backend services
// consume without re-verifying (the mTLS edge proves the caller is us).
package edge

import (
	"net/http"
	"strings"

	"github.com/developernajib/lynk/services/gateway/internal/platform/auth"
	"github.com/developernajib/lynk/services/gateway/internal/platform/jwt"
)

// Trusted internal headers injected downstream after verification.
const (
	headerUserID    = "X-User-Id"
	headerRole      = "X-Role"
	headerTokenType = "X-Token-Type" //nolint:gosec // a header NAME, not a credential
)

// Authenticator verifies bearer tokens and consults the blacklist.
type Authenticator struct {
	verifier  *jwt.Verifier
	blacklist *TokenBlacklist
}

// NewAuthenticator builds the authenticator. verifier may be nil in
// development (no public key configured), in which case requests pass
// through anonymous.
func NewAuthenticator(verifier *jwt.Verifier, blacklist *TokenBlacklist) *Authenticator {
	return &Authenticator{verifier: verifier, blacklist: blacklist}
}

// Middleware establishes identity when a valid token is presented.
//
// The critical anti-spoofing step: any client-supplied identity headers are
// stripped FIRST, unconditionally. Downstream services trust those headers,
// so only the gateway, after verifying a signature, may set them.
//
// Anonymous requests pass through: public routes must work without a token,
// and per-handler guards decide what requires a principal.
func (a *Authenticator) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stripIdentityHeaders(r)

			token := bearerToken(r)
			if token == "" || a.verifier == nil {
				next.ServeHTTP(w, r)
				return
			}

			claims, err := a.verifier.Verify(token)
			if err != nil {
				// Presented but invalid: continue anonymously without
				// leaking why verification failed.
				next.ServeHTTP(w, r)
				return
			}

			// Bloom filter first, Redis only on "possibly revoked".
			if a.blacklist != nil && a.blacklist.IsRevoked(r.Context(), claims.ID) {
				http.Error(w, "token revoked", http.StatusUnauthorized)
				return
			}

			p := auth.Principal{
				UserID:    claims.Subject,
				Role:      claims.Role,
				TokenType: string(claims.TokenType),
			}
			injectIdentityHeaders(r, p)
			next.ServeHTTP(w, r.WithContext(auth.IntoContext(r.Context(), p)))
		})
	}
}

func stripIdentityHeaders(r *http.Request) {
	r.Header.Del(headerUserID)
	r.Header.Del(headerRole)
	r.Header.Del(headerTokenType)
}

func injectIdentityHeaders(r *http.Request, p auth.Principal) {
	r.Header.Set(headerUserID, p.UserID)
	r.Header.Set(headerRole, p.Role)
	r.Header.Set(headerTokenType, p.TokenType)
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if remainder, found := strings.CutPrefix(header, "Bearer "); found {
		return remainder
	}
	if remainder, found := strings.CutPrefix(header, "bearer "); found {
		return remainder
	}
	return ""
}
