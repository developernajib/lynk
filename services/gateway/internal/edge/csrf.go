package edge

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/developernajib/lynk/services/gateway/internal/platform/secure"
)

const (
	// XSRF-TOKEN / X-XSRF-TOKEN are the de facto standard names; Axios and
	// Angular pick them up automatically.
	csrfCookieName = "XSRF-TOKEN"
	csrfHeaderName = "X-XSRF-TOKEN"
	// 32 bytes = 256 bits of entropy, far beyond brute force.
	csrfTokenLen = 32
)

// CSRF implements the double-submit cookie pattern: the gateway plants a
// random token as a NON-HttpOnly, SameSite=Strict cookie; JavaScript on our
// origin copies it into the X-XSRF-TOKEN header on state-changing requests;
// the gateway requires header == cookie. A hostile page cannot read our
// cookie (same-origin policy) and SameSite=Strict stops the browser sending
// it cross-site at all, so a forged request can never present a matching
// pair. Stateless by design: the randomness lives in the cookie, no
// server-side session needed.
//
// Exemptions: safe methods (no state change), bearer-token requests (a
// cross-origin page cannot set an Authorization header without passing
// CORS), and gRPC-Web (token-authenticated, not cookie-authenticated).
func CSRF(isProduction bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Safe methods still plant the cookie so the next mutation from
			// this user has a token to echo.
			if isSafeMethod(r.Method) {
				ensureCSRFCookie(w, isProduction)
				next.ServeHTTP(w, r)
				return
			}

			authz := r.Header.Get("Authorization")
			if strings.HasPrefix(authz, "Bearer ") || strings.HasPrefix(authz, "bearer ") {
				next.ServeHTTP(w, r)
				return
			}

			if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc-web") {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(csrfCookieName)
			if err != nil || cookie.Value == "" {
				http.Error(w, "CSRF token missing", http.StatusForbidden)
				return
			}

			// Constant-time compare: == short-circuits on the first mismatch,
			// which leaks the token one byte at a time to a timing attacker.
			header := r.Header.Get(csrfHeaderName)
			if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
				http.Error(w, "CSRF token mismatch", http.StatusForbidden)
				return
			}

			// Rotate on success so an intercepted token has a short window.
			ensureCSRFCookie(w, isProduction)
			next.ServeHTTP(w, r)
		})
	}
}

// ensureCSRFCookie plants a fresh token. HttpOnly is false BY DESIGN: the
// pattern requires our JavaScript to read the value; SameSite=Strict plus
// the header check provide the cross-origin protection.
func ensureCSRFCookie(w http.ResponseWriter, isProduction bool) {
	token, err := secure.HexToken(csrfTokenLen)
	if err != nil {
		// Entropy failure: skip rather than serve a weak token; the next
		// response will try again.
		return
	}
	// #nosec G124 -- double-submit CSRF cookie: HttpOnly is false BY DESIGN
	// (JS must echo the value into a header), Secure is on in production (a
	// non-literal gosec cannot evaluate), SameSite is Strict.
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(24 * time.Hour / time.Second),
		HttpOnly: false,
		Secure:   isProduction,
		SameSite: http.SameSiteStrictMode,
	})
}

// isSafeMethod reports whether the method is read-only per RFC 9110.
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}
