// Package cache provides a read-through cache: a process-local Ristretto L1
// (~nanosecond hits) over an optional Redis L2 (~hundreds of microseconds,
// shared across instances), with singleflight stampede protection so one
// expensive miss loads once no matter how many requests arrive together.
//
// The cache fails open: an unreachable Redis degrades to L1-plus-loader, it
// never fails a request. Invalidation is exposed as a method so an
// event-driven purge (a consumer calling Invalidate on writes) can keep
// instances coherent; for strict coherence keep TTLs short.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// Options tunes a Cache. Zero values take the documented defaults.
type Options struct {
	// L1MaxItems caps the in-process cache size. Default 10_000.
	L1MaxItems int64
	// L1TTL bounds staleness inside one instance. Default 1m.
	L1TTL time.Duration
	// L2 is the shared Redis client; nil disables the L2 tier.
	L2 *redis.Client
	// L2TTL bounds staleness across instances. Default 15m.
	L2TTL time.Duration
	// Prefix namespaces this cache's keys in Redis, e.g. "storefront:".
	Prefix string
}

func (o Options) withDefaults() Options {
	if o.L1MaxItems <= 0 {
		o.L1MaxItems = 10_000
	}
	if o.L1TTL <= 0 {
		o.L1TTL = time.Minute
	}
	if o.L2TTL <= 0 {
		o.L2TTL = 15 * time.Minute
	}
	return o
}

// Cache is a typed two-tier read-through cache. V crosses the Redis tier as
// JSON, so it must marshal cleanly.
type Cache[V any] struct {
	opts  Options
	l1    *ristretto.Cache[string, V]
	group singleflight.Group
}

// New builds a Cache.
func New[V any](opts Options) (*Cache[V], error) {
	opts = opts.withDefaults()

	l1, err := ristretto.NewCache(&ristretto.Config[string, V]{
		// Ristretto's admission policy wants ~10 counters per cached item.
		NumCounters: opts.L1MaxItems * 10,
		MaxCost:     opts.L1MaxItems,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("cache: build l1: %w", err)
	}

	return &Cache[V]{opts: opts, l1: l1}, nil
}

// GetOrLoad returns the cached value for key, loading it at most once per
// instance on concurrent misses: L1, then L2, then load, writing back both
// tiers on the way out.
func (c *Cache[V]) GetOrLoad(ctx context.Context, key string, load func(ctx context.Context) (V, error)) (V, error) {
	if value, ok := c.l1.Get(key); ok {
		return value, nil
	}

	// singleflight collapses concurrent misses for the same key into one
	// execution; the duplicates wait and share the result.
	result, err, _ := c.group.Do(key, func() (any, error) {
		if value, ok := c.fromL2(ctx, key); ok {
			c.l1.SetWithTTL(key, value, 1, c.opts.L1TTL)
			return value, nil
		}

		value, err := load(ctx)
		if err != nil {
			return nil, err
		}

		c.l1.SetWithTTL(key, value, 1, c.opts.L1TTL)
		c.toL2(ctx, key, value)
		return value, nil
	})
	if err != nil {
		var zero V
		return zero, err
	}
	return result.(V), nil //nolint:errcheck // singleflight stores exactly V
}

// Invalidate removes key from both tiers. Call it from write paths or an
// event consumer so all instances converge after a change.
func (c *Cache[V]) Invalidate(ctx context.Context, key string) {
	c.l1.Del(key)
	if c.opts.L2 != nil {
		// Best-effort: a failed Redis delete leaves the L2 TTL as the bound.
		_ = c.opts.L2.Del(ctx, c.opts.Prefix+key).Err()
	}
}

// fromL2 reads and decodes a value from Redis, treating every failure
// (down, miss, corrupt) as a cache miss: fail open, never fail the request.
func (c *Cache[V]) fromL2(ctx context.Context, key string) (V, bool) {
	var zero V
	if c.opts.L2 == nil {
		return zero, false
	}
	raw, err := c.opts.L2.Get(ctx, c.opts.Prefix+key).Bytes()
	if err != nil {
		return zero, false
	}
	var value V
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, false
	}
	return value, true
}

// toL2 encodes and stores a value in Redis, best-effort.
func (c *Cache[V]) toL2(ctx context.Context, key string, value V) {
	if c.opts.L2 == nil {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	_ = c.opts.L2.Set(ctx, c.opts.Prefix+key, raw, c.opts.L2TTL).Err()
}

// Close releases the L1's internal goroutines.
func (c *Cache[V]) Close() {
	c.l1.Close()
}
