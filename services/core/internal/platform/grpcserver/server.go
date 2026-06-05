// Package grpcserver builds the service's gRPC server with a standard
// interceptor chain, hardening limits, optional TLS/mTLS, a health service,
// and development-only reflection.
//
// Module adapters register their generated services onto the *grpc.Server
// this package returns; cross-cutting concerns (panic recovery, request ids,
// logging, error mapping, validation, timeouts) live in the interceptors here
// so every service inherits them identically.
package grpcserver

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Config holds the server's own settings; the package does not import the
// central config so it stays independently usable. Zero values take the
// documented defaults.
type Config struct {
	// Port is the TCP port the gRPC API listens on.
	Port int
	// MaxRecvMessageBytes caps inbound message size (memory-DoS guard).
	// Default 4 MiB.
	MaxRecvMessageBytes int
	// MaxConcurrentStreams caps in-flight RPCs per connection so one
	// connection cannot monopolize the server. Default 1000.
	MaxConcurrentStreams uint32
	// HandlerTimeout bounds a single RPC handler. Non-positive disables
	// enforcement. Default 30s.
	HandlerTimeout time.Duration
	// TLSCertFile and TLSKeyFile enable TLS when both are set; setting only
	// one fails startup rather than silently downgrading.
	TLSCertFile string
	TLSKeyFile  string
	// TLSClientCAFile additionally turns on mTLS: clients must present a
	// certificate signed by this CA, restricting callers to trusted services.
	TLSClientCAFile string
	// Reflection enables server reflection for tools like grpcurl. Keep it
	// off in production: it needlessly exposes the API surface.
	Reflection bool
}

func (c Config) withDefaults() Config {
	if c.MaxRecvMessageBytes <= 0 {
		c.MaxRecvMessageBytes = 4 << 20
	}
	if c.MaxConcurrentStreams == 0 {
		c.MaxConcurrentStreams = 1000
	}
	if c.HandlerTimeout == 0 {
		c.HandlerTimeout = 30 * time.Second
	}
	return c
}

// Option customizes New beyond Config.
type Option func(*options)

type options struct {
	unary  []grpc.UnaryServerInterceptor
	stream []grpc.StreamServerInterceptor
}

// WithUnaryInterceptors appends service-specific unary guards (e.g. auth)
// that run after the standard chain, closest to the handler.
func WithUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) Option {
	return func(o *options) { o.unary = append(o.unary, interceptors...) }
}

// WithStreamInterceptors appends stream guards. Streams bypass the unary
// chain entirely, so any service exposing server streams must pass its auth
// or header interceptors here as well.
func WithStreamInterceptors(interceptors ...grpc.StreamServerInterceptor) Option {
	return func(o *options) { o.stream = append(o.stream, interceptors...) }
}

// Server wraps *grpc.Server with the listener address and the gRPC health
// service so readiness can flip serving status.
type Server struct {
	grpcServer   *grpc.Server
	healthServer *health.Server
	addr         string
}

// New constructs the gRPC server with the full interceptor chain installed.
//
// Unary order (outermost first): recovery catches panics from everything
// inside; requestID establishes correlation before anything logs; logging
// attaches the request logger; errorMap translates and fingerprints errors
// from all inner layers; timeout bounds the handler; validation rejects
// malformed input before business logic; extras run last, closest to the
// handler.
func New(cfg Config, log zerolog.Logger, opts ...Option) (*Server, error) {
	cfg = cfg.withDefaults()

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	unary := append([]grpc.UnaryServerInterceptor{
		recoveryInterceptor(log),
		requestIDInterceptor(),
		loggingInterceptor(log),
		errorMappingInterceptor(),
		timeoutInterceptor(cfg.HandlerTimeout),
		validationInterceptor(),
	}, o.unary...)

	stream := append([]grpc.StreamServerInterceptor{
		streamRecoveryInterceptor(log),
	}, o.stream...)

	serverOptions := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
		// The otelgrpc stats handler emits spans and metrics for every RPC
		// without hand-instrumenting handlers.
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.MaxRecvMsgSize(cfg.MaxRecvMessageBytes),
		grpc.MaxConcurrentStreams(cfg.MaxConcurrentStreams),
		grpc.ConnectionTimeout(10 * time.Second),
		keepaliveEnforcement(),
		keepaliveParams(),
	}

	// TLS/mTLS is opt-in via config; nil credentials mean plain HTTP/2 (dev).
	creds, err := loadTransportCredentials(cfg)
	if err != nil {
		return nil, err
	}
	if creds != nil {
		serverOptions = append(serverOptions, grpc.Creds(creds))
	}

	grpcServer := grpc.NewServer(serverOptions...)

	// The standard health service lets load balancers and probes query
	// serving status via the well-known Health/Check RPC.
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)

	if cfg.Reflection {
		reflection.Register(grpcServer)
	}

	return &Server{
		grpcServer:   grpcServer,
		healthServer: healthServer,
		addr:         fmt.Sprintf(":%d", cfg.Port),
	}, nil
}

// GRPC exposes the underlying *grpc.Server so module adapters can register
// their generated services during bootstrap.
func (s *Server) GRPC() *grpc.Server { return s.grpcServer }

// Start binds the listener and serves. It blocks; run it in a goroutine.
func (s *Server) Start() error {
	// The context only bounds listener setup; Serve blocks until Stop, so
	// Background is the honest root here.
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("grpcserver: listen %s: %w", s.addr, err)
	}
	// Empty service name is the gRPC convention for overall server status.
	s.healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	return s.grpcServer.Serve(lis)
}

// Stop drains gracefully, bounded by ctx: GracefulStop has no timeout of its
// own, so it races the context and falls back to a hard stop.
func (s *Server) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.grpcServer.Stop()
		return ctx.Err()
	}
}
