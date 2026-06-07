// Package bootstrap is the gateway's composition root.
package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/developernajib/lynk/services/gateway/internal/edge"
	"github.com/developernajib/lynk/services/gateway/internal/platform/config"
	"github.com/developernajib/lynk/services/gateway/internal/platform/health"
	"github.com/developernajib/lynk/services/gateway/internal/platform/jwt"
	"github.com/developernajib/lynk/services/gateway/internal/platform/logger"
	"github.com/developernajib/lynk/services/gateway/internal/platform/redis"
	"github.com/developernajib/lynk/services/gateway/internal/platform/runtimeenv"
	"github.com/developernajib/lynk/services/gateway/internal/platform/shutdown"
	"github.com/developernajib/lynk/services/gateway/internal/platform/telemetry"
)

const initTimeout = 30 * time.Second

// Run assembles and serves the public edge until SIGINT/SIGTERM or a fatal
// component error.
func Run() error {
	runtimeenv.Configure()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(logger.Config{
		Service: cfg.App.Name,
		Env:     cfg.App.Env,
		Version: config.Version,
		Level:   cfg.Log.Level,
		JSON:    cfg.Log.JSON,
	})

	manager := shutdown.NewManager(log, 20*time.Second)

	initCtx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()

	providers, err := telemetry.Init(initCtx, telemetry.Config{
		ServiceName:      cfg.App.Name,
		ServiceVersion:   config.Version,
		Env:              cfg.App.Env,
		Enabled:          cfg.Telemetry.Enabled,
		Endpoint:         cfg.Telemetry.Endpoint,
		TraceSampleRatio: cfg.Telemetry.TraceSampleRatio,
	})
	if err != nil {
		return err
	}
	manager.Register("telemetry", providers.Shutdown)

	redisConn, err := redis.Connect(initCtx, redis.Config{
		URL:        cfg.Redis.URL,
		PoolSize:   cfg.Redis.PoolSize,
		MaxRetries: cfg.Redis.MaxRetries,
	})
	if err != nil {
		manager.Run()
		return err
	}
	manager.Register("redis", func(context.Context) error { return redisConn.Close() })

	// Verification is optional in development (no key configured), so the
	// gateway can run against a keyless local stack; production validation
	// already required the key.
	var verifier *jwt.Verifier
	if cfg.JWT.PublicKeyPEM != "" {
		verifier, err = jwt.NewVerifier(jwt.Config{PublicKeyPEM: cfg.JWT.PublicKeyPEM, Issuer: cfg.JWT.Issuer})
		if err != nil {
			manager.Run()
			return err
		}
	} else {
		log.Warn().Msg("JWT_PUBLIC_KEY_PEM not set: requests pass through unauthenticated (development only)")
	}

	// The blacklist subscription lives for the whole process, not just init.
	processCtx, stopProcess := context.WithCancel(context.Background())
	manager.Register("blacklist-subscription", func(context.Context) error { stopProcess(); return nil })

	blacklist := edge.NewTokenBlacklist(redisConn)
	blacklist.Subscribe(processCtx)

	proxy, err := edge.NewProxy(cfg.Upstreams)
	if err != nil {
		manager.Run()
		return err
	}
	manager.Register("proxy-connections", proxy.Close)

	handler := edge.BuildHandler(
		cfg, log,
		edge.NewRateLimiter(redisConn),
		edge.NewAuthenticator(verifier, blacklist),
		proxy,
	)

	public := edge.NewServer(cfg, handler)
	manager.Register("public-server", public.Stop)

	ops := health.NewServer(strconv.Itoa(cfg.App.MetricsPort))
	ops.Register("redis", func(ctx context.Context) error { return redisConn.Ping(ctx).Err() })
	ops.Mount("/metrics", providers.MetricsHandler())
	manager.Register("ops-server", ops.Stop)

	log.Info().Int("http_port", cfg.App.HTTPPort).Int("ops_port", cfg.App.MetricsPort).Msg("gateway starting")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 2)
	go func() { errCh <- public.Start() }()
	go func() { errCh <- ops.Start() }()

	select {
	case <-ctx.Done():
		log.Info().Msg("shutdown signal received, draining")
		manager.Run()
		return nil
	case err := <-errCh:
		if err != nil {
			log.Error().Err(err).Msg("component failed, draining")
		}
		manager.Run()
		if err != nil {
			return fmt.Errorf("gateway: %w", err)
		}
		return nil
	}
}
