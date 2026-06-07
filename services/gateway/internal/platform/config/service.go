package config

import (
	"strings"
	"time"
)

// Version is stamped at build time via the linker.
var Version = "dev"

// Config is the gateway's complete, typed configuration.
type Config struct {
	App       AppConfig
	Server    ServerConfig
	CORS      CORSConfig
	RateLimit RateLimitConfig
	Upstreams UpstreamsConfig
	JWT       JWTConfig
	Redis     RedisConfig
	Log       LogConfig
	Telemetry TelemetryConfig
}

// AppConfig is process identity and ports.
type AppConfig struct {
	Env  string
	Name string
	// HTTPPort is the public edge port (gRPC-Web + any HTTP).
	HTTPPort    int
	MetricsPort int
}

// ServerConfig hardens the public HTTP server. Timeouts and body limits ship
// ON: a server without them is a slowloris target.
type ServerConfig struct {
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	MaxBodyBytes      int64
	HandlerTimeout    time.Duration
	// TLSCertFile/TLSKeyFile terminate public TLS at the gateway itself;
	// leave empty when a fronting proxy terminates TLS.
	TLSCertFile string
	TLSKeyFile  string
}

// CORSConfig is the exact-match origin allowlist. Never "*" with credentials.
type CORSConfig struct {
	AllowedOrigins []string
}

// RateLimitConfig tunes the three limiter levels (global per-instance,
// per-IP, per-endpoint). Per-endpoint limits live in code (edge.EndpointLimits).
type RateLimitConfig struct {
	GlobalPerSecond int
	PerIPPerMinute  int
}

// UpstreamsConfig lists the backend gRPC addresses and the internal mTLS
// material for gateway-to-backend connections.
type UpstreamsConfig struct {
	Core string
	TLS  InternalTLSConfig
}

// InternalTLSConfig is the mTLS trio for internal traffic. All three set =
// mutual TLS 1.3; all empty = plaintext (development); a partial trio fails
// validation rather than silently downgrading.
type InternalTLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

// Enabled reports whether the full trio is configured.
func (t InternalTLSConfig) Enabled() bool {
	return t.CertFile != "" && t.KeyFile != "" && t.CAFile != ""
}

func (t InternalTLSConfig) partial() bool {
	set := 0
	for _, v := range []string{t.CertFile, t.KeyFile, t.CAFile} {
		if v != "" {
			set++
		}
	}
	return set > 0 && set < 3
}

// JWTConfig holds verification-only material: the gateway never signs.
type JWTConfig struct {
	PublicKeyPEM string
	Issuer       string
}

// RedisConfig backs rate limiting and the token blacklist.
type RedisConfig struct {
	URL        string
	PoolSize   int
	MaxRetries int
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

// Load reads every setting from the environment and validates fail-fast.
func Load() (*Config, error) {
	if err := LoadDotenv(); err != nil {
		return nil, err
	}

	cfg := &Config{
		App: AppConfig{
			Env:         String("APP_ENV", "development"),
			Name:        String("APP_NAME", "gateway"),
			HTTPPort:    Int("HTTP_PORT", 8080),
			MetricsPort: Int("METRICS_PORT", 9090),
		},
		Server: ServerConfig{
			ReadTimeout:       Duration("HTTP_READ_TIMEOUT", 10*time.Second),
			ReadHeaderTimeout: Duration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			WriteTimeout:      Duration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       Duration("HTTP_IDLE_TIMEOUT", 120*time.Second),
			MaxHeaderBytes:    Int("HTTP_MAX_HEADER_BYTES", 1<<20),
			MaxBodyBytes:      int64(Int("HTTP_MAX_BODY_BYTES", 10<<20)),
			HandlerTimeout:    Duration("HTTP_HANDLER_TIMEOUT", 30*time.Second),
			TLSCertFile:       String("HTTP_TLS_CERT_FILE", ""),
			TLSKeyFile:        String("HTTP_TLS_KEY_FILE", ""),
		},
		CORS: CORSConfig{
			AllowedOrigins: splitList(String("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000")),
		},
		RateLimit: RateLimitConfig{
			GlobalPerSecond: Int("RATE_LIMIT_GLOBAL_PER_SECOND", 10000),
			PerIPPerMinute:  Int("RATE_LIMIT_PER_IP_PER_MINUTE", 600),
		},
		Upstreams: UpstreamsConfig{
			Core: String("UPSTREAM_CORE", "localhost:50051"),
			TLS: InternalTLSConfig{
				CertFile: String("INTERNAL_TLS_CERT_FILE", ""),
				KeyFile:  String("INTERNAL_TLS_KEY_FILE", ""),
				CAFile:   String("INTERNAL_TLS_CA_FILE", ""),
			},
		},
		JWT: JWTConfig{
			PublicKeyPEM: String("JWT_PUBLIC_KEY_PEM", ""),
			Issuer:       String("JWT_ISSUER", "lynk"),
		},
		Redis: RedisConfig{
			URL:        String("REDIS_URL", ""),
			PoolSize:   Int("REDIS_POOL_SIZE", 50),
			MaxRetries: Int("REDIS_MAX_RETRIES", 3),
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
	v.Require("REDIS_URL", cfg.Redis.URL)
	v.Require("UPSTREAM_CORE", cfg.Upstreams.Core)
	if cfg.IsProduction() {
		// Without the public key the gateway cannot verify tokens; tolerable
		// in development, never in production.
		v.Require("JWT_PUBLIC_KEY_PEM", cfg.JWT.PublicKeyPEM)
	}
	v.Check(!cfg.Upstreams.TLS.partial(),
		"INTERNAL_TLS_CERT_FILE, INTERNAL_TLS_KEY_FILE, INTERNAL_TLS_CA_FILE must be set together or not at all")
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
