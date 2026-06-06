// Package postgres builds the database connection pools and the transaction
// manager. It hands repositories ready pools; repositories own the SQL. This
// package knows nothing about tables or domain types.
//
// The pools implement the per-service read/write split: writes and
// read-your-writes/money/auth reads use Write (the primary), high-volume
// staleness-tolerant reads use Read(), which balances across any number of
// replicas. Scaling reads is therefore configuration (add a replica URL), not
// code. Past a handful of replicas, prefer PgBouncer or a load balancer in
// front of one read URL instead of growing the list.
package postgres

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the pool settings. Zero values take the documented defaults.
type Config struct {
	// WriteURL is the primary (read-write) connection string. Required.
	WriteURL string
	// ReadURLs lists zero or more read-replica connection strings. Empty
	// means reads run on the primary (correct, just less scalable); multiple
	// URLs are balanced round-robin. Pool settings below apply per pool.
	ReadURLs []string
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

// Pools bundles the primary pool with the replica pools so bootstrap wires
// "the database" as one dependency. Repositories choose by intent:
// pools.Write for writes and read-your-writes, pools.Read() for everything
// staleness-tolerant.
type Pools struct {
	// Write is the primary, read-write pool. Always present.
	Write *pgxpool.Pool

	reads []*pgxpool.Pool
	next  atomic.Uint64
}

// Connect builds the primary and every configured replica pool, verifying
// connectivity on each. ctx bounds the initial connections so a dead database
// fails startup fast instead of hanging.
func Connect(ctx context.Context, cfg Config) (*Pools, error) {
	cfg = cfg.withDefaults()

	write, err := newPool(ctx, cfg.WriteURL, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect primary: %w", err)
	}

	reads := make([]*pgxpool.Pool, 0, len(cfg.ReadURLs))
	for i, url := range cfg.ReadURLs {
		read, err := newPool(ctx, url, cfg)
		if err != nil {
			// Never leak already-opened pools on a partial failure.
			write.Close()
			for _, opened := range reads {
				opened.Close()
			}
			return nil, fmt.Errorf("postgres: connect replica %d: %w", i+1, err)
		}
		reads = append(reads, read)
	}

	return &Pools{Write: write, reads: reads}, nil
}

// Read returns the pool for staleness-tolerant reads: the primary when no
// replica is configured, otherwise the next replica in round-robin order.
// Balancing is per call, so one slow query never pins a hot replica.
func (p *Pools) Read() *pgxpool.Pool {
	switch len(p.reads) {
	case 0:
		return p.Write
	case 1:
		return p.reads[0]
	default:
		n := p.next.Add(1)
		// A slice index may be any integer type; staying in uint64 avoids a
		// down-conversion, and the modulo keeps it in range.
		return p.reads[n%uint64(len(p.reads))]
	}
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

// Close releases every pool during graceful shutdown.
func (p *Pools) Close() {
	p.Write.Close()
	for _, read := range p.reads {
		read.Close()
	}
}
