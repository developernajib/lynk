package edge

import "net/http"

// HTTPSRedirect bounces plain-HTTP requests to https:// in production,
// closing the first-visit hole HSTS cannot cover (HSTS only protects after
// the first successful HTTPS response). X-Forwarded-Proto is honored because
// production gateways usually sit behind a TLS-terminating proxy, so
// request.TLS is nil even when the client connection is secure.
//
// Disabled outside production: local development runs plain HTTP on purpose.
func HTTPSRedirect(enabled bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled || isSecureRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			target := "https://" + r.Host + r.URL.RequestURI()

			// 301 lets browsers cache the redirect for GET/HEAD, but permits
			// the method to change to GET on follow; 308 preserves the method
			// and body for everything else.
			status := http.StatusMovedPermanently
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				status = http.StatusPermanentRedirect
			}
			// #nosec G710 -- not an open redirect: the target is the client's
			// own Host + URI with only the scheme upgraded (the standard
			// force-HTTPS bounce); a forged Host redirects only the forger.
			http.Redirect(w, r, target, status)
		})
	}
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
