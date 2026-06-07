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
