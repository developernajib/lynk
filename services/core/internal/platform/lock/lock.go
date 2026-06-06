// Package lock provides a Redis-backed distributed lock: SET NX PX with an
// ownership token, released by a Lua compare-and-delete so a holder whose
// lease expired can never release the next holder's lock.
package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

// releaseScript deletes the key only when it still holds our token. GET then
// DEL without the script races: the lease could expire and be re-acquired by
// another holder between the two commands.
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`)

// Locker acquires leases on a shared Redis.
type Locker struct {
	client *redis.Client
}

// New builds a Locker.
func New(client *redis.Client) *Locker {
	return &Locker{client: client}
}

// Lease is one held lock. Release it when done; if the process dies instead,
// the TTL frees the lock on its own.
type Lease struct {
	client *redis.Client
	key    string
	token  string
}

// Acquire attempts to take the lock for ttl. ok=false means another holder
// has it (not an error). The error reports Redis being unreachable, which the
// caller maps to its own fail-open or fail-closed policy.
func (l *Locker) Acquire(ctx context.Context, key string, ttl time.Duration) (*Lease, bool, error) {
	// The random token marks ownership: only the holder that set the key may
	// delete it.
	token, err := secure.Token(16)
	if err != nil {
		return nil, false, err
	}

	ok, err := l.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, false, fmt.Errorf("lock: acquire %s: %w", key, err)
	}
	if !ok {
		return nil, false, nil
	}
	return &Lease{client: l.client, key: key, token: token}, true, nil
}

// Release frees the lock if this lease still owns it. Releasing an expired
// lease is a no-op, not an error: the TTL already did the work.
func (le *Lease) Release(ctx context.Context) error {
	if err := releaseScript.Run(ctx, le.client, []string{le.key}, le.token).Err(); err != nil {
		return fmt.Errorf("lock: release %s: %w", le.key, err)
	}
	return nil
}
