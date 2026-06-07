// Package redis builds the Redis client used for caching, rate-limit
// counters, distributed locks, and the token blacklist. It returns a ready
// client; higher layers decide what keys to store.
package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// Config holds the client settings. Zero values take the documented defaults.
type Config struct {
	// URL is a redis:// connection string. Required.
	URL string
	// PoolSize caps pooled connections. Default 50.
	PoolSize int
	// MaxRetries bounds per-command retries. Default 3.
	MaxRetries int
}

// Connect parses the URL, applies pool settings, instruments tracing, and
// verifies the server with a PING so a bad address is a startup error rather
// than a surprise on the first cache call.
//
// It returns the concrete *redis.Client; consumers that want testability
// declare their own narrow interface over the methods they use.
func Connect(ctx context.Context, cfg Config) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("redis: parse url: %w", err)
	}

	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	} else {
		opts.PoolSize = 50
	}
	if cfg.MaxRetries > 0 {
		opts.MaxRetries = cfg.MaxRetries
	} else {
		opts.MaxRetries = 3
	}

	client := redis.NewClient(opts)

	// redisotel records every command as a child span next to the SQL spans.
	// It only fails on misuse, so treat failure as a startup error.
	if err := redisotel.InstrumentTracing(client); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: instrument tracing: %w", err)
	}

	if err := client.Ping(ctx).Err(); err != nil {
		// Close so the pool's background goroutines don't leak.
		_ = client.Close()
		return nil, fmt.Errorf("redis: ping: %w", err)
	}

	return client, nil
}
