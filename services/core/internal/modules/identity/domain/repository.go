package domain

import (
	"context"
	"time"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// UserRepository is the persistence port for the User aggregate.
type UserRepository interface {
	// Create inserts a new user, returning ErrEmailTaken on the unique
	// email constraint.
	Create(ctx context.Context, user *User) error
	// GetByEmail loads by canonical email, or ErrUserNotFound.
	GetByEmail(ctx context.Context, email vo.Email) (*User, error)
	// GetByID loads by id, or ErrUserNotFound.
	GetByID(ctx context.Context, id vo.UserID) (*User, error)
	// Update persists changes with the optimistic-lock guard, returning
	// ErrConcurrentUpdate when another writer won.
	Update(ctx context.Context, user *User) error
	// MarkEmailVerified stamps the verification time once; idempotent.
	MarkEmailVerified(ctx context.Context, id vo.UserID, now time.Time) error
}

// APIKeyRepository is the persistence port for machine credentials.
type APIKeyRepository interface {
	// Create records a freshly minted key.
	Create(ctx context.Context, key *APIKey) error
	// ListForUser returns the owner's keys, newest first.
	ListForUser(ctx context.Context, userID string) ([]*APIKey, error)
	// GetByHash loads by key hash, or ErrAPIKeyNotFound.
	GetByHash(ctx context.Context, keyHash string) (*APIKey, error)
	// Revoke disables one of the owner's keys; owner-scoped, returning
	// ErrAPIKeyNotFound when the id is not theirs or already revoked.
	Revoke(ctx context.Context, id, userID string, now time.Time) error
}

// OTPRepository is the persistence port for one-time codes.
type OTPRepository interface {
	// Create records a freshly issued code.
	Create(ctx context.Context, otp *OTP) error
	// GetActive returns the newest live code for a user and purpose, or
	// ErrInvalidOTP.
	GetActive(ctx context.Context, userID string, purpose OTPPurpose, now time.Time) (*OTP, error)
	// Consume marks a code used; a consumed code never validates again.
	Consume(ctx context.Context, id string, now time.Time) error
}

// RefreshTokenRepository is the persistence port for sessions.
type RefreshTokenRepository interface {
	// Create records a freshly issued session.
	Create(ctx context.Context, token *RefreshToken) error
	// GetByHash loads by token hash, or ErrRefreshTokenInvalid.
	GetByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	// Revoke marks one session unusable (idempotent).
	Revoke(ctx context.Context, id string, now time.Time) error
	// RevokeAllForUser ends every live session, e.g. on password change.
	RevokeAllForUser(ctx context.Context, userID string, now time.Time) error
}
