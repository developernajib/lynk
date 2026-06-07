package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/developernajib/lynk/services/core/internal/platform/auth"
	"github.com/developernajib/lynk/services/core/internal/platform/config"
	"github.com/developernajib/lynk/services/core/internal/platform/grpcserver"
	"github.com/developernajib/lynk/services/core/internal/platform/health"
	"github.com/developernajib/lynk/services/core/internal/platform/logger"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
	"github.com/developernajib/lynk/services/core/internal/platform/redis"
	"github.com/developernajib/lynk/services/core/internal/platform/runtimeenv"
	"github.com/developernajib/lynk/services/core/internal/platform/shutdown"
	"github.com/developernajib/lynk/services/core/internal/platform/telemetry"
)

// initTimeout bounds startup connections so a dead dependency fails the boot
// fast instead of hanging the deploy.
const initTimeout = 30 * time.Second

// foundation is everything both processes (server and worker) share.
type foundation struct {
	cfg       *config.Config
	log       zerolog.Logger
	pools     *postgres.Pools
	redis     *goredis.Client
	bus       *nats.Connection
	providers *telemetry.Providers
	shutdown  *shutdown.Manager
}

// setup builds the shared foundation in dependency order, registering each
// piece's cleanup as it succeeds so a later failure tears down what already
// started.
func setup(processSuffix string) (*foundation, error) {
	runtimeenv.Configure()

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	processName := cfg.App.Name + processSuffix

	log := logger.New(logger.Config{
		Service: processName,
		Env:     cfg.App.Env,
		Version: config.Version,
		Level:   cfg.Log.Level,
		JSON:    cfg.Log.JSON,
	})

	manager := shutdown.NewManager(log, 20*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()

	providers, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:      processName,
		ServiceVersion:   config.Version,
		Env:              cfg.App.Env,
		Enabled:          cfg.Telemetry.Enabled,
		Endpoint:         cfg.Telemetry.Endpoint,
		TraceSampleRatio: cfg.Telemetry.TraceSampleRatio,
	})
	if err != nil {
		return nil, err
	}
	manager.Register("telemetry", providers.Shutdown)

	pools, err := postgres.Connect(ctx, postgres.Config{
		WriteURL:        cfg.Database.WriteURL,
		ReadURLs:        cfg.Database.ReadURLs,
		MaxConns:        cfg.Database.MaxConns,
		MinConns:        cfg.Database.MinConns,
		MaxConnLifetime: cfg.Database.MaxConnLifetime,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
	})
	if err != nil {
		manager.Run()
		return nil, err
	}
	manager.Register("postgres", func(context.Context) error { pools.Close(); return nil })

	redisConn, err := redis.Connect(ctx, redis.Config{
		URL:        cfg.Redis.URL,
		PoolSize:   cfg.Redis.PoolSize,
		MaxRetries: cfg.Redis.MaxRetries,
	})
	if err != nil {
		manager.Run()
		return nil, err
	}
	manager.Register("redis", func(context.Context) error { return redisConn.Close() })

	bus, err := nats.Connect(nats.Config{
		URL:            cfg.NATS.URL,
		ClientName:     processName,
		StreamReplicas: cfg.NATS.StreamReplicas,
	})
	if err != nil {
		manager.Run()
		return nil, err
	}
	manager.Register("nats", func(context.Context) error { return bus.Close() })

	return &foundation{
		cfg:       cfg,
		log:       log,
		pools:     pools,
		redis:     redisConn,
		bus:       bus,
		providers: providers,
		shutdown:  manager,
	}, nil
}

// opsServer builds the probes-and-metrics server with real dependency
// checks. Both processes serve one; the worker uses MetricsPort+100.
func (f *foundation) opsServer(port int) *health.Server {
	ops := health.NewServer(strconv.Itoa(port))
	ops.Register("postgres", func(ctx context.Context) error { return f.pools.Write.Ping(ctx) })
	ops.Register("redis", func(ctx context.Context) error { return f.redis.Ping(ctx).Err() })
	ops.Register("nats", func(context.Context) error {
		if !f.bus.Conn.IsConnected() {
			return fmt.Errorf("nats not connected")
		}
		return nil
	})
	ops.Mount("/metrics", f.providers.MetricsHandler())
	return ops
}

// RunServer assembles and runs the API server process until SIGINT/SIGTERM
// or a fatal component error.
func RunServer() error {
	f, err := setup("")
	if err != nil {
		return err
	}

	server, err := grpcserver.New(grpcserver.Config{
		Port:                 f.cfg.App.GRPCPort,
		MaxRecvMessageBytes:  f.cfg.Server.MaxRecvMessageBytes,
		MaxConcurrentStreams: f.cfg.Server.MaxConcurrentStreams,
		HandlerTimeout:       f.cfg.Server.HandlerTimeout,
		TLSCertFile:          f.cfg.Server.TLSCertFile,
		TLSKeyFile:           f.cfg.Server.TLSKeyFile,
		TLSClientCAFile:      f.cfg.Server.TLSClientCAFile,
		Reflection:           !f.cfg.IsProduction(),
	}, f.log,
		// Behind the gateway and mTLS, identity arrives as trusted headers.
		// Streams need their own interceptor: they bypass the unary chain.
		grpcserver.WithUnaryInterceptors(auth.TrustedHeaderInterceptor()),
		grpcserver.WithStreamInterceptors(auth.TrustedHeaderStreamInterceptor()),
	)
	if err != nil {
		f.shutdown.Run()
		return err
	}

	modules, err := buildModules(f.cfg, f.log, f.pools, f.redis, f.bus)
	if err != nil {
		f.shutdown.Run()
		return err
	}
	modules.RegisterAll(server.GRPC())

	// Initial policy load. Survivable on failure: the engine denies by
	// default until a refresh succeeds, which is the safe direction.
	startCtx, cancelStart := context.WithTimeout(context.Background(), 10*time.Second)
	if err := modules.Authz.Start(startCtx); err != nil {
		f.log.Warn().Err(err).Msg("authz: initial policy load failed; denying by default until refresh")
	}
	cancelStart()

	ops := f.opsServer(f.cfg.App.MetricsPort)
	f.shutdown.Register("ops-server", ops.Stop)
	f.shutdown.Register("grpc-server", server.Stop)

	f.log.Info().Int("grpc_port", f.cfg.App.GRPCPort).Int("ops_port", f.cfg.App.MetricsPort).Msg("server starting")
	return runUntilSignal(f, server.Start, ops.Start)
}

// runUntilSignal starts each blocking component in its own goroutine and
// waits for a signal or the first component failure, then drains LIFO.
func runUntilSignal(f *foundation, components ...func() error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, len(components))
	for _, component := range components {
		go func() { errCh <- component() }()
	}

	select {
	case <-ctx.Done():
		f.log.Info().Msg("shutdown signal received, draining")
		f.shutdown.Run()
		return nil
	case err := <-errCh:
		if err != nil {
			f.log.Error().Err(err).Msg("component failed, draining")
		}
		f.shutdown.Run()
		return err
	}
}
