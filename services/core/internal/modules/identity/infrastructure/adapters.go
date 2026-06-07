package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/application"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/platform/jwt"
	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

// Argon2idHasher adapts the platform's password hashing (argon2id with
// bcrypt verify fallback) to the PasswordHasher port.
type Argon2idHasher struct{}

// Hash produces an argon2id PHC string.
func (Argon2idHasher) Hash(password string) (string, error) {
	return secure.HashPassword(password)
}

// Verify checks against argon2id or legacy bcrypt hashes.
func (Argon2idHasher) Verify(stored, password string) (bool, error) {
	return secure.VerifyPassword(stored, password)
}

// AccessTokenSigner adapts the platform RS256 signer to the TokenSigner
// port, stamping the user token type.
type AccessTokenSigner struct {
	signer *jwt.Signer
}

// NewAccessTokenSigner wraps the platform signer.
func NewAccessTokenSigner(signer *jwt.Signer) AccessTokenSigner {
	return AccessTokenSigner{signer: signer}
}

// IssueAccessToken mints a user access token.
func (s AccessTokenSigner) IssueAccessToken(userID, role string) (application.AccessToken, error) {
	issued, err := s.signer.IssueAccessToken(userID, jwt.Claims{Role: role, TokenType: jwt.TokenTypeUser})
	if err != nil {
		return application.AccessToken{}, err
	}
	return application.AccessToken{Token: issued.Token, ID: issued.ID, ExpiresAt: issued.ExpiresAt}, nil
}

// OpaqueTokens generates 32-byte crypto/rand refresh tokens and their
// SHA-256 lookup hashes.
type OpaqueTokens struct{}

// Generate returns the raw token (for the client, once) and its hash (for
// storage).
func (OpaqueTokens) Generate() (string, string, error) {
	raw, err := secure.Token(32)
	if err != nil {
		return "", "", err
	}
	return raw, secure.HashToken(raw), nil
}

// Hash re-derives the lookup hash for a presented token.
func (OpaqueTokens) Hash(raw string) string { return secure.HashToken(raw) }

// RedisBlacklist revokes access tokens: the key is what the gateway's
// authoritative check reads, and the pub/sub message is what updates every
// gateway instance's in-memory Bloom filter within milliseconds.
type RedisBlacklist struct {
	client *redis.Client
}

// NewRedisBlacklist builds the adapter.
func NewRedisBlacklist(client *redis.Client) *RedisBlacklist {
	return &RedisBlacklist{client: client}
}

// Revoke blacklists a jti with a TTL matching the token's natural expiry,
// so entries clean themselves up.
func (b *RedisBlacklist) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if err := b.client.Set(ctx, "jwt:blacklist:"+jti, "1", ttl).Err(); err != nil {
		return fmt.Errorf("identity: blacklist token: %w", err)
	}
	// Best-effort fan-out to gateway Bloom filters; the Redis key above is
	// the authoritative record either way.
	_ = b.client.Publish(ctx, "jwt:blacklist:events", jti).Err()
	return nil
}

// OTPCodes adapts the platform's code generation to the OTPCodes port:
// 6-digit uniform-random codes, SHA-256 stored.
type OTPCodes struct{}

// NewCode mints a code and its storage hash.
func (OTPCodes) NewCode() (string, string, error) {
	raw, err := secure.NumericCode(6)
	if err != nil {
		return "", "", err
	}
	return raw, secure.HashToken(raw), nil
}

// Hash re-derives the lookup hash for a presented code.
func (OTPCodes) Hash(raw string) string { return secure.HashToken(raw) }

// LogNotifier is the development Notifier: it logs the code instead of
// sending it. Replace it with a real email adapter behind the same port; the
// WARN level makes "you have not wired email yet" impossible to miss.
type LogNotifier struct {
	log zerolog.Logger
}

// NewLogNotifier builds the stub.
func NewLogNotifier(log zerolog.Logger) LogNotifier {
	return LogNotifier{log: log}
}

// SendOTP logs the code that production would email.
func (n LogNotifier) SendOTP(_ context.Context, email string, purpose domain.OTPPurpose, code string) error {
	n.log.Warn().
		Str("email", email).
		Str("purpose", string(purpose)).
		Str("code", code).
		Msg("identity: LogNotifier active - wire a real email adapter for production")
	return nil
}

// RedisAPIKeyCache caches validated keys for ten minutes, keyed by key hash,
// with a reverse index by key id so revocation can drop entries immediately.
// Positives only and fail-open: a cache problem degrades to database lookups.
type RedisAPIKeyCache struct {
	client *redis.Client
}

// NewRedisAPIKeyCache builds the adapter.
func NewRedisAPIKeyCache(client *redis.Client) *RedisAPIKeyCache {
	return &RedisAPIKeyCache{client: client}
}

const apiKeyCacheTTL = 10 * time.Minute

type cachedKey struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// Lookup returns a cached validation result.
func (c *RedisAPIKeyCache) Lookup(ctx context.Context, keyHash string) (string, string, bool) {
	raw, err := c.client.Get(ctx, "apikey:cache:"+keyHash).Bytes()
	if err != nil {
		return "", "", false
	}
	var entry cachedKey
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", "", false
	}
	return entry.UserID, entry.Role, true
}

// Store caches a positive validation and indexes it by key id.
func (c *RedisAPIKeyCache) Store(ctx context.Context, keyHash, keyID, userID, role string) {
	raw, err := json.Marshal(cachedKey{UserID: userID, Role: role})
	if err != nil {
		return
	}
	pipe := c.client.TxPipeline()
	pipe.Set(ctx, "apikey:cache:"+keyHash, raw, apiKeyCacheTTL)
	pipe.Set(ctx, "apikey:byid:"+keyID, keyHash, apiKeyCacheTTL)
	_, _ = pipe.Exec(ctx)
}

// Drop removes a key's cache entry via the reverse index so revocation is
// immediate instead of waiting out the TTL.
func (c *RedisAPIKeyCache) Drop(ctx context.Context, keyID string) {
	hash, err := c.client.Get(ctx, "apikey:byid:"+keyID).Result()
	if err != nil {
		return
	}
	_ = c.client.Del(ctx, "apikey:cache:"+hash, "apikey:byid:"+keyID).Err()
}

// RedisLoginThrottle implements account lockout: 5 failures in 15 minutes
// locks the identifier for the remainder of the window. Keyed by the
// SUBMITTED identifier, so unknown emails throttle exactly like real ones
// (no enumeration signal). FAILS OPEN by design: Redis being down must not
// lock every user out; the trade-off is documented and the opposite of the
// billing locks, which fail closed.
type RedisLoginThrottle struct {
	client *redis.Client
}

// NewRedisLoginThrottle builds the adapter.
func NewRedisLoginThrottle(client *redis.Client) *RedisLoginThrottle {
	return &RedisLoginThrottle{client: client}
}

const (
	throttleMaxFailures = 5
	throttleWindow      = 15 * time.Minute
)

func throttleKey(identifier string) string {
	// The identifier is hashed so raw emails never appear as Redis keys.
	return "login:fails:" + secure.HashToken(identifier)
}

// Allowed reports whether the identifier may attempt a login.
func (t *RedisLoginThrottle) Allowed(ctx context.Context, identifier string) bool {
	count, err := t.client.Get(ctx, throttleKey(identifier)).Int64()
	if err != nil {
		return true // missing key or Redis down: allow
	}
	return count < throttleMaxFailures
}

// RecordFailure counts a failed attempt, starting the lockout window on the
// first one.
func (t *RedisLoginThrottle) RecordFailure(ctx context.Context, identifier string) {
	key := throttleKey(identifier)
	pipe := t.client.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, throttleWindow)
	if _, err := pipe.Exec(ctx); err != nil {
		return // fail open
	}
	_ = incr
}

// Reset clears the counter after a successful login.
func (t *RedisLoginThrottle) Reset(ctx context.Context, identifier string) {
	_ = t.client.Del(ctx, throttleKey(identifier)).Err()
}
