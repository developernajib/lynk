package domain

import "time"

// RefreshToken is one session credential. The raw token never exists here:
// the aggregate carries only its SHA-256, so neither the domain nor the
// database can leak something replayable.
type RefreshToken struct {
	id        string
	userID    string
	tokenHash string
	expiresAt time.Time
	revokedAt *time.Time
	createdAt time.Time
}

// NewRefreshToken records a freshly issued session.
func NewRefreshToken(id, userID, tokenHash string, expiresAt, now time.Time) *RefreshToken {
	return &RefreshToken{
		id:        id,
		userID:    userID,
		tokenHash: tokenHash,
		expiresAt: expiresAt,
		createdAt: now,
	}
}

// RefreshTokenFromState rehydrates from storage.
func RefreshTokenFromState(id, userID, tokenHash string, expiresAt time.Time, revokedAt *time.Time, createdAt time.Time) *RefreshToken {
	return &RefreshToken{
		id:        id,
		userID:    userID,
		tokenHash: tokenHash,
		expiresAt: expiresAt,
		revokedAt: revokedAt,
		createdAt: createdAt,
	}
}

// IsActive reports whether the session may still be used: not revoked, not
// expired.
func (t *RefreshToken) IsActive(now time.Time) bool {
	return t.revokedAt == nil && now.Before(t.expiresAt)
}

// ID returns the session id.
func (t *RefreshToken) ID() string { return t.id }

// UserID returns the owning user.
func (t *RefreshToken) UserID() string { return t.userID }

// TokenHash returns the stored hash.
func (t *RefreshToken) TokenHash() string { return t.tokenHash }

// ExpiresAt returns the hard expiry.
func (t *RefreshToken) ExpiresAt() time.Time { return t.expiresAt }

// CreatedAt returns the issue time.
func (t *RefreshToken) CreatedAt() time.Time { return t.createdAt }
