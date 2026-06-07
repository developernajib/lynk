package config

import (
	"strings"
	"time"
)

// Version is stamped at build time via the linker:
// go build -ldflags "-X .../config.Version=v1.2.3". A var, not a const,
// because the linker can only patch variables.
var Version = "dev"

// Config is the service's complete, typed configuration, loaded once in
// bootstrap and validated before anything starts. Subsystem packages keep
// their own small Config types; bootstrap maps these fields onto them, so
// platform packages never depend on this central struct.
type Config struct {
	App       AppConfig
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	NATS      NATSConfig
	JWT       JWTConfig
	Log       LogConfig
	Telemetry TelemetryConfig
}

// AppConfig is process identity and ports.
type AppConfig struct {
	// Env is "development", "staging", or "production".
	Env string
	// Name labels this service in logs, traces, and metrics.
	Name string
	// GRPCPort is the public API port.
	GRPCPort int
	// MetricsPort serves health probes and /metrics, firewalled separately
	// from the API. The worker process uses MetricsPort+100.
	MetricsPort int
}

// ServerConfig is the gRPC hardening and TLS surface.
type ServerConfig struct {
	MaxRecvMessageBytes  int
	MaxConcurrentStreams uint32
	HandlerTimeout       time.Duration
	TLSCertFile          string
	TLSKeyFile           string
	TLSClientCAFile      string
}

// DatabaseConfig is the read/write split plus pool tuning.
type DatabaseConfig struct {
	WriteURL string
	// ReadURLs is comma-separated in DB_READ_URLS; multiple replicas are
	// balanced round-robin by the postgres package.
	ReadURLs        []string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// RedisConfig is the cache/lock/blacklist store.
type RedisConfig struct {
	URL        string
	PoolSize   int
	MaxRetries int
}

// NATSConfig is the event bus.
type NATSConfig struct {
	URL            string
	StreamReplicas int
}

// JWTConfig feeds both the token signer and every verifier; see the jwt
// package for why one config serves both sides.
type JWTConfig struct {
	PrivateKeyPEM   string
	PublicKeyPEM    string
	Issuer          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// LogConfig controls structured logging.
type LogConfig struct {
	Level string
	JSON  bool
}

// TelemetryConfig controls OpenTelemetry export.
type TelemetryConfig struct {
	Enabled          bool
	Endpoint         string
	TraceSampleRatio float64
}

// IsProduction keeps the "what counts as prod" rule in one place.
func (c *Config) IsProduction() bool { return c.App.Env == "production" }

// IsDevelopment reports development mode.
func (c *Config) IsDevelopment() bool { return c.App.Env == "development" }

// Load reads every setting from the environment (after a best-effort .env
// load for development) and validates the result so the process fails fast
// on a bad deployment instead of crashing at first use.
func Load() (*Config, error) {
	if err := LoadDotenv(); err != nil {
		return nil, err
	}

	cfg := &Config{
		App: AppConfig{
			Env:         String("APP_ENV", "development"),
			Name:        String("APP_NAME", "core"),
			GRPCPort:    Int("GRPC_PORT", 50051),
			MetricsPort: Int("METRICS_PORT", 9091),
		},
		Server: ServerConfig{
			// 4 MiB: generous for API messages, far below a memory-DoS size.
			MaxRecvMessageBytes:  Int("GRPC_MAX_RECV_BYTES", 4<<20),
			MaxConcurrentStreams: uint32(Int("GRPC_MAX_CONCURRENT_STREAMS", 1000)), //nolint:gosec // bounded operator config
			HandlerTimeout:       Duration("GRPC_HANDLER_TIMEOUT", 30*time.Second),
			TLSCertFile:          String("GRPC_TLS_CERT_FILE", ""),
			TLSKeyFile:           String("GRPC_TLS_KEY_FILE", ""),
			TLSClientCAFile:      String("GRPC_TLS_CLIENT_CA_FILE", ""),
		},
		Database: DatabaseConfig{
			WriteURL:        String("DB_WRITE_URL", ""),
			ReadURLs:        splitList(String("DB_READ_URLS", "")),
			MaxConns:        Int32("DB_MAX_CONNS", 0), // 0 = postgres default (4 per CPU)
			MinConns:        Int32("DB_MIN_CONNS", 5),
			MaxConnLifetime: Duration("DB_MAX_CONN_LIFETIME", 30*time.Minute),
			MaxConnIdleTime: Duration("DB_MAX_CONN_IDLE_TIME", 5*time.Minute),
		},
		Redis: RedisConfig{
			URL:        String("REDIS_URL", ""),
			PoolSize:   Int("REDIS_POOL_SIZE", 50),
			MaxRetries: Int("REDIS_MAX_RETRIES", 3),
		},
		NATS: NATSConfig{
			URL:            String("NATS_URL", ""),
			StreamReplicas: Int("NATS_STREAM_REPLICAS", 1),
		},
		JWT: JWTConfig{
			PrivateKeyPEM:   String("JWT_PRIVATE_KEY_PEM", ""),
			PublicKeyPEM:    String("JWT_PUBLIC_KEY_PEM", ""),
			Issuer:          String("JWT_ISSUER", "lynk"),
			AccessTokenTTL:  Duration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTokenTTL: Duration("JWT_REFRESH_TTL", 7*24*time.Hour),
		},
		Log: LogConfig{
			Level: String("LOG_LEVEL", "info"),
			JSON:  Bool("LOG_JSON", false),
		},
		Telemetry: TelemetryConfig{
			Enabled:          Bool("OTEL_ENABLED", false),
			Endpoint:         String("OTEL_ENDPOINT", "localhost:4317"),
			TraceSampleRatio: Float64("OTEL_TRACE_SAMPLE_RATIO", 1.0),
		},
	}

	v := NewValidation()
	v.Require("DB_WRITE_URL", cfg.Database.WriteURL)
	v.Require("REDIS_URL", cfg.Redis.URL)
	v.Require("NATS_URL", cfg.NATS.URL)
	if cfg.IsProduction() {
		// JWT keys may stay empty in development so the service boots for
		// non-auth work without generating keys first.
		v.Require("JWT_PRIVATE_KEY_PEM", cfg.JWT.PrivateKeyPEM)
		v.Require("JWT_PUBLIC_KEY_PEM", cfg.JWT.PublicKeyPEM)
	}
	v.Check(cfg.Telemetry.TraceSampleRatio >= 0 && cfg.Telemetry.TraceSampleRatio <= 1,
		"OTEL_TRACE_SAMPLE_RATIO must be between 0.0 and 1.0")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// splitList parses a comma-separated env value into trimmed, non-empty items.
func splitList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}
