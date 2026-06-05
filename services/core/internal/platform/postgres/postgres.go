// Package postgres builds the database connection pools and the transaction
// manager. It hands repositories ready pools; repositories own the SQL. This
// package knows nothing about tables or domain types.
//
// Two pools implement the per-service read/write split: writes and
// read-your-writes/money/auth reads use Write (the primary), high-volume
// staleness-tolerant reads use Read (the replica).
package postgres

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the pool settings. Zero values take the documented defaults.
type Config struct {
	// WriteURL is the primary (read-write) connection string. Required.
	WriteURL string
	// ReadURL is the replica connection string. Empty means no replica; the
	// read pool then aliases the primary, which is correct, just less
	// scalable.
	ReadURL string
	// MaxConns caps connections per pool. Default 4 per CPU.
	MaxConns int32
	// MinConns keeps a warm floor of idle connections against cold-start
	// latency. Default 5.
	MinConns int32
	// MaxConnLifetime recycles connections so load balancers can rebalance.
	// Default 30m.
	MaxConnLifetime time.Duration
	// MaxConnIdleTime releases idle connections in quiet periods. Default 5m.
	MaxConnIdleTime time.Duration
}

func (c Config) withDefaults() Config {
	if c.MaxConns <= 0 {
		c.MaxConns = int32(4 * runtime.GOMAXPROCS(0)) //nolint:gosec // GOMAXPROCS is small
	}
	if c.MinConns <= 0 {
		c.MinConns = 5
	}
	if c.MaxConnLifetime <= 0 {
		c.MaxConnLifetime = 30 * time.Minute
	}
	if c.MaxConnIdleTime <= 0 {
		c.MaxConnIdleTime = 5 * time.Minute
	}
	return c
}

// Pools bundles the primary and replica pools so bootstrap wires "the
// database" as one dependency and the pool choice reads explicitly at every
// call site (pools.Read vs pools.Write).
type Pools struct {
	// Write is the primary, read-write pool. Always present.
	Write *pgxpool.Pool
	// Read is the replica pool, or the same primary pool when no replica is
	// configured, so callers never nil-check.
	Read *pgxpool.Pool
	// readIsPrimary prevents Close from closing the aliased pool twice.
	readIsPrimary bool
}

// Connect builds both pools and verifies connectivity. ctx bounds the initial
// connection so a dead database fails startup fast instead of hanging.
func Connect(ctx context.Context, cfg Config) (*Pools, error) {
	cfg = cfg.withDefaults()

	write, err := newPool(ctx, cfg.WriteURL, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect primary: %w", err)
	}

	if cfg.ReadURL == "" {
		return &Pools{Write: write, Read: write, readIsPrimary: true}, nil
	}

	read, err := newPool(ctx, cfg.ReadURL, cfg)
	if err != nil {
		// Never leak the primary on a partial failure.
		write.Close()
		return nil, fmt.Errorf("postgres: connect replica: %w", err)
	}

	return &Pools{Write: write, Read: read, readIsPrimary: false}, nil
}

func newPool(ctx context.Context, url string, cfg Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime

	// otelpgx records every query as a child span of the calling request, so
	// a slow endpoint shows which SQL statement ate the time. No-op cost when
	// telemetry is disabled.
	poolConfig.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Ping turns bad credentials or address into a clear startup error
	// instead of a first-request crash.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return pool, nil
}

// Close releases both pools during graceful shutdown.
func (p *Pools) Close() {
	p.Write.Close()
	if !p.readIsPrimary {
		p.Read.Close()
	}
}
