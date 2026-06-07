// Package config provides the building blocks for 12-factor configuration:
// typed environment readers, a minimal .env loader for development, and a
// validation collector for fail-fast startup checks.
//
// There is deliberately no central Config struct. Each lynk subsystem owns a
// small config struct loaded from the environment with these helpers, and an
// application composes only the subsystems it uses.
package config

import (
	"os"
	"strconv"
	"time"
)

// String returns the environment variable named key, or fallback when it is
// unset or empty.
func String(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Int parses the environment variable as a base-10 int, returning fallback
// when it is unset or malformed. Validation of values that must be present
// and sane belongs in a Validation, not here.
func Int(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// Int32 is Int clamped to the int32 range, for APIs sized in int32 such as
// pgx pool limits.
func Int32(key string, fallback int32) int32 {
	n := Int(key, int(fallback))
	if n > int(int32(^uint32(0)>>1)) {
		return int32(^uint32(0) >> 1)
	}
	if n < int(^int32(^uint32(0)>>1)) {
		return ^int32(^uint32(0) >> 1)
	}
	return int32(n)
}

// Bool parses the environment variable with strconv.ParseBool rules
// ("true", "false", "1", "0", ...), returning fallback when unset or malformed.
func Bool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

// Float64 parses the environment variable as a float, returning fallback when
// unset or malformed.
func Float64(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

// Duration parses the environment variable as a Go duration string such as
// "15m" or "750ms", returning fallback when unset or malformed. Duration
// strings are self-documenting where a bare number of seconds is not.
func Duration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
