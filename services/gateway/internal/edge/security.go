package edge

import (
	"context"
	"fmt"
	"net/http"

	"github.com/developernajib/lynk/services/gateway/internal/platform/config"
	"github.com/developernajib/lynk/services/gateway/internal/platform/secure"
)

// cspNonceKey is unexported so no other package can collide with the context
// slot (context keys compare by type and value).
type cspNonceKey struct{}

// CSPNonceFromContext returns the per-request CSP nonce so template handlers
// can mark their own inline <script>/<style> blocks as intentional without
// the blanket 'unsafe-inline' that would defeat CSP.
func CSPNonceFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(cspNonceKey{}).(string); ok {
		return v
	}
	return ""
}

// SecurityHeaders sets the full suite of browser security headers and
// generates a per-request CSP nonce (crypto/rand; a predictable nonce would
// defeat CSP entirely).
//
// In production the CSP is strict nonce-based and HSTS is on. In development
// the CSP allows eval and websockets for hot reload, and HSTS is off so local
// HTTP tooling is not locked into HTTPS for a year.
func SecurityHeaders(isProduction bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			// nosniff: stop browsers "guessing" that an uploaded JPEG is
			// JavaScript (MIME confusion → script execution).
			h.Set("X-Content-Type-Options", "nosniff")
			// DENY framing: clickjacking; CSP frame-ancestors below is the
			// modern equivalent, both set for defense in depth.
			h.Set("X-Frame-Options", "DENY")
			// Legacy XSS heuristics for old browsers; harmless on modern ones.
			h.Set("X-XSS-Protection", "1; mode=block")
			// Send only the origin cross-site so tokens in URLs never leak
			// through the Referer header.
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			// A compromised third-party script cannot silently surveil users.
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

			if isProduction {
				// HSTS: refuse plain HTTP for a year, killing SSL stripping.
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			nonce, err := secure.HexToken(16)
			if err == nil {
				var csp string
				if isProduction {
					// Strict allowlist: only scripts carrying this request's
					// nonce run; injected markup cannot know the value.
					csp = fmt.Sprintf(
						"default-src 'self'; script-src 'self' 'nonce-%s'; style-src 'self' 'nonce-%s' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
						nonce, nonce,
					)
				} else {
					// Dev: allow eval (source maps) and ws (hot reload).
					// Never serve this policy in production.
					csp = fmt.Sprintf("default-src 'self' 'unsafe-eval'; script-src 'self' 'unsafe-inline' 'unsafe-eval' 'nonce-%s'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:", nonce)
				}
				h.Set("Content-Security-Policy", csp)
				r = r.WithContext(context.WithValue(r.Context(), cspNonceKey{}, nonce))
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CORS reflects the request Origin only when it is on the explicit
// allowlist, never "*": the spec forbids "*" with credentials because it
// would let any site make cookie-bearing requests as a logged-in user.
func CORS(cfg config.CORSConfig) Middleware {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, ok := allowed[origin]; ok {
					h := w.Header()
					h.Set("Access-Control-Allow-Origin", origin)
					h.Set("Access-Control-Allow-Credentials", "true")
					h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
					// X-XSRF-Token must be allowlisted or browsers strip the
					// CSRF header; the grpc-web headers are what connect-web
					// sends.
					h.Set("Access-Control-Allow-Headers",
						"Content-Type, Authorization, X-Request-Id, X-Grpc-Web, X-User-Agent, "+
							"Connect-Protocol-Version, X-XSRF-Token")
					// Vary keys caches on Origin so one origin's response is
					// never served (with its allow header) to another.
					h.Add("Vary", "Origin")
				}
				// Unknown origin: no CORS headers at all; the browser blocks.
				// No error response, which would enable origin enumeration.
			}

			// Preflights expect a bare 2xx with the headers above.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
