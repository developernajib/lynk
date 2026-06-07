// ratelimit.go implements three-level distributed rate limiting:
//
//   - Global per-instance (in-memory, always active): the last-resort
//     capacity guard. In-memory on purpose: it must keep working when Redis
//     is down or saturated, and an atomic counter sustains millions of
//     increments per second.
//   - Per-IP (Redis sliding window): bots, scrapers, single-source floods.
//   - Per-endpoint per-IP (Redis sliding window): much tighter limits on
//     auth paths so credential stuffing is economically infeasible.
//
// The Redis levels use the sliding-window-counter algorithm (current window
// count plus the previous window weighted by overlap) in one atomic Lua
// script per check. Unlike a fixed window it cannot be gamed with a 2x burst
// at the window boundary, and it costs two small keys instead of a sorted
// set per caller.
//
// Redis-backed levels FAIL OPEN: a transient Redis outage degrades
// enforcement instead of taking the whole API down; the in-memory global
// level keeps the hard ceiling. Operators should alert on Redis downtime.
package edge

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// slidingWindowScript counts a request against the current window and
// returns the weighted total: curr + prev * (remaining fraction of the
// window). Runs atomically server-side; steady state is one EVALSHA
// round-trip per check.
var slidingWindowScript = redis.NewScript(`
local curr = redis.call('INCR', KEYS[1])
if curr == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1] * 2)
end
local prev = tonumber(redis.call('GET', KEYS[2]) or '0')
local weight = (ARGV[1] - ARGV[2]) / ARGV[1]
return math.floor(prev * weight + curr)
`)

// RateLimiter holds the shared Redis client plus the per-instance in-memory
// global counter (atomics, not a mutex: lock-free under the contention a
// limiter exists to survive).
type RateLimiter struct {
	redis        *redis.Client
	globalCount  atomic.Int64
	globalWindow atomic.Int64
}

// NewRateLimiter builds the limiter on the shared Redis client.
func NewRateLimiter(client *redis.Client) *RateLimiter {
	return &RateLimiter{redis: client}
}

// allow runs one sliding-window check, failing open on Redis errors.
func (rl *RateLimiter) allow(ctx context.Context, key string, limit int, window time.Duration) bool {
	now := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	currWindow := now / windowMs
	elapsed := now % windowMs

	currKey := "rl:" + key + ":" + strconv.FormatInt(currWindow, 10)
	prevKey := "rl:" + key + ":" + strconv.FormatInt(currWindow-1, 10)

	count, err := slidingWindowScript.Run(ctx, rl.redis,
		[]string{currKey, prevKey}, windowMs, elapsed).Int64()
	if err != nil {
		return true // fail open; the global level still bounds capacity
	}
	return count <= int64(limit)
}

// Global enforces a per-instance, per-second ceiling with an in-memory fixed
// window: a CAS claims each new second's reset (exactly one goroutine wins),
// then an atomic add checks the count. The brief reset race is acceptable;
// the goal is preventing a catastrophic burst, not a hard per-second SLA.
func (rl *RateLimiter) Global(perSecond int) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now().Unix()
			window := rl.globalWindow.Load()
			if now != window {
				if rl.globalWindow.CompareAndSwap(window, now) {
					rl.globalCount.Store(0)
				}
			}
			if rl.globalCount.Add(1) > int64(perSecond) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "server capacity exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PerIP limits each client IP per minute: above human-plausible rates, below
// automated-attack rates.
func (rl *RateLimiter) PerIP(perMinute int) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(r.Context(), "ip:"+clientIP(r), perMinute, time.Minute) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PerEndpoint applies tighter path-specific limits per IP on top of PerIP.
// Keys are path suffixes like "Login"; the first match wins. An attacker at
// 10 req/min needs ~70 days for a million guesses, and the identity service
// locks the account long before that.
func (rl *RateLimiter) PerEndpoint(limits map[string]int) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			for endpoint, limit := range limits {
				if strings.HasSuffix(path, endpoint) {
					key := "ep:" + endpoint + ":" + clientIP(r)
					if !rl.allow(r.Context(), key, limit, time.Minute) {
						w.Header().Set("Retry-After", "60")
						http.Error(w, "endpoint rate limit exceeded", http.StatusTooManyRequests)
						return
					}
					break
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts the real client IP: the leftmost X-Forwarded-For entry
// when behind a trusted proxy, otherwise the TCP peer. If the gateway is NOT
// behind a trusted proxy, remove the X-Forwarded-For branch: a direct client
// can forge the header to dodge per-IP limits.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if leftmost, _, found := strings.Cut(fwd, ","); found {
			return strings.TrimSpace(leftmost)
		}
		return strings.TrimSpace(fwd)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
