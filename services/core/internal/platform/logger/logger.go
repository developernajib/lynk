// Package logger builds the service's structured logger (zerolog) and carries
// a request-scoped logger through context.
//
// Logs go to stdout only: in a containerized 12-factor service the platform
// captures stdout and ships it to the aggregator, so file rotation is not this
// process's job. zerolog over slog because it allocates almost nothing on the
// hot path, a deliberate dependency for high request volume.
package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Config holds the logger's own settings. The package deliberately does not
// import the central config: it stays independently usable and cycle-free.
type Config struct {
	// Service, Env, and Version are stamped on every line so the aggregator
	// can filter by them.
	Service string
	Env     string
	Version string
	// Level is "debug", "info", "warn", or "error".
	Level string
	// JSON forces machine-readable output even outside production.
	JSON bool
}

// New constructs the base logger: compact JSON in production (or when forced),
// colored human-readable console output in development.
func New(cfg Config) zerolog.Logger {
	return zerolog.New(chooseWriter(cfg)).
		Level(parseLevel(cfg.Level)).
		With().
		Timestamp().
		Str("service", cfg.Service).
		Str("env", cfg.Env).
		Str("version", cfg.Version).
		Logger()
}

func chooseWriter(cfg Config) io.Writer {
	if cfg.Env == "production" || cfg.JSON {
		return os.Stdout
	}
	return zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
}

// parseLevel defaults to Info on unknown input so a typo can never silence
// logging entirely.
func parseLevel(raw string) zerolog.Level {
	switch raw {
	case "debug":
		return zerolog.DebugLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}
