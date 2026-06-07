package edge

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/developernajib/lynk/services/gateway/internal/platform/config"
)

// EndpointLimits maps method suffixes to per-minute per-IP limits, far
// tighter than the general per-IP level on the paths attackers brute-force.
// Extend it as auth-sensitive RPCs appear.
var EndpointLimits = map[string]int{
	"Login":                    10,
	"Register":                 20,
	"RequestPasswordReset":     10,
	"ResetPassword":            10,
	"RequestEmailVerification": 10,
	"VerifyEmail":              10,
	"ValidateAPIKey":           120,
}

// BuildHandler composes the full edge chain around the bridge. The order is
// the design: recover first, correlate, cheap header work, size and timeout
// guards, rate limits before auth (a flood must never get free crypto),
// CSRF, then auth (the expensive verify), endpoint limits, and the proxy.
//
// otelhttp wraps the whole chain: browsers send no traceparent, so the edge
// is where every distributed trace is born; the otelgrpc client handler on
// the backend connections then carries it downstream as one trace.
func BuildHandler(
	cfg *config.Config,
	log zerolog.Logger,
	rl *RateLimiter,
	authenticator *Authenticator,
	proxy *Proxy,
) http.Handler {
	return otelhttp.NewHandler(Chain(
		proxy.Handler(),
		Recovery(log),
		RequestID(),
		Logger(log),
		HTTPSRedirect(cfg.IsProduction()),
		SecurityHeaders(cfg.IsProduction()),
		Gzip(),
		CORS(cfg.CORS),
		BodyLimit(cfg.Server.MaxBodyBytes),
		Timeout(cfg.Server.HandlerTimeout),
		rl.Global(cfg.RateLimit.GlobalPerSecond),
		rl.PerIP(cfg.RateLimit.PerIPPerMinute),
		CSRF(cfg.IsProduction()),
		authenticator.Middleware(),
		rl.PerEndpoint(EndpointLimits),
	), "gateway")
}

// Server wraps the hardened public HTTP server. Timeouts and header caps are
// non-optional: a server without them is a slowloris target.
type Server struct {
	httpServer  *http.Server
	tlsCertFile string
	tlsKeyFile  string
}

// NewServer builds the public server from config.
func NewServer(cfg *config.Config, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.App.HTTPPort),
			Handler:           handler,
			ReadTimeout:       cfg.Server.ReadTimeout,
			ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
			WriteTimeout:      cfg.Server.WriteTimeout,
			IdleTimeout:       cfg.Server.IdleTimeout,
			MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
		},
		tlsCertFile: cfg.Server.TLSCertFile,
		tlsKeyFile:  cfg.Server.TLSKeyFile,
	}
}

// Start serves (TLS when certs are configured) until Stop.
func (s *Server) Start() error {
	var err error
	if s.tlsCertFile != "" && s.tlsKeyFile != "" {
		err = s.httpServer.ListenAndServeTLS(s.tlsCertFile, s.tlsKeyFile)
	} else {
		err = s.httpServer.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop drains in-flight requests within the context deadline.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
