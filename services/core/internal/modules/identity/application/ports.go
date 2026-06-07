// Package application holds the identity module's use cases, one per file,
// plus the ports its adapters implement.
package application

import (
	"context"
	"time"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
)

// Clock supplies time so the use cases stay deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator mints aggregate ids (UUIDv7 in production).
type IDGenerator interface {
	NewID() (string, error)
}

// PasswordHasher abstracts the credential KDF (argon2id in production, with
// bcrypt verify for imported user tables).
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(stored, password string) (bool, error)
}

// AccessToken is a signed access token plus what callers need afterward: the
// jti for blacklisting and the expiry for clients and blacklist TTLs.
type AccessToken struct {
	Token     string
	ID        string
	ExpiresAt time.Time
}

// TokenSigner mints access tokens. Declared here so the application layer
// never imports the JWT library.
type TokenSigner interface {
	IssueAccessToken(userID, role string) (AccessToken, error)
}

// OpaqueTokens generates and hashes refresh tokens. The raw value goes to
// the client exactly once; only the hash is ever stored or looked up.
type OpaqueTokens interface {
	Generate() (raw, hash string, err error)
	Hash(raw string) string
}

// TokenBlacklist revokes access tokens by jti until they would expire
// anyway. The production adapter also notifies gateway instances.
type TokenBlacklist interface {
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
}

// LoginThrottle is the account-lockout guard, keyed by the SUBMITTED
// identifier so attackers cannot tell existing accounts from unknown ones by
// throttle behavior. Implementations fail OPEN: an unavailable throttle
// store must not lock every user out of the platform.
type LoginThrottle interface {
	Allowed(ctx context.Context, identifier string) bool
	RecordFailure(ctx context.Context, identifier string)
	Reset(ctx context.Context, identifier string)
}

// EventPublisher records domain events in the caller's transaction (outbox).
type EventPublisher interface {
	Publish(ctx context.Context, events []domain.Event) error
}

// UnitOfWork runs fn atomically.
type UnitOfWork interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
