package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// ChangePassword swaps the credential after re-authentication and revokes
// every session: a password change is exactly when existing sessions (a
// possibly-compromised one among them) must die.
type ChangePassword struct {
	users    domain.UserRepository
	sessions domain.RefreshTokenRepository
	hasher   PasswordHasher
	events   EventPublisher
	uow      UnitOfWork
	clock    Clock
}

// NewChangePassword wires the use case.
func NewChangePassword(users domain.UserRepository, sessions domain.RefreshTokenRepository, hasher PasswordHasher, events EventPublisher, uow UnitOfWork, clock Clock) *ChangePassword {
	return &ChangePassword{users: users, sessions: sessions, hasher: hasher, events: events, uow: uow, clock: clock}
}

// Execute re-authenticates with the current password before changing it, so
// a hijacked logged-in session cannot lock the real owner out.
func (uc *ChangePassword) Execute(ctx context.Context, userID, currentPassword, newPassword string) error {
	id, err := vo.NewUserID(userID)
	if err != nil {
		return domain.ErrUserNotFound
	}
	user, err := uc.users.GetByID(ctx, id)
	if err != nil {
		return err
	}

	ok, err := uc.hasher.Verify(user.PasswordHash(), currentPassword)
	if err != nil || !ok {
		return domain.ErrInvalidCredentials
	}

	newHash, err := uc.hasher.Hash(newPassword)
	if err != nil {
		return err
	}

	user.ChangePassword(newHash, uc.clock.Now())

	return uc.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.users.Update(ctx, user); err != nil {
			return err
		}
		if err := uc.sessions.RevokeAllForUser(ctx, user.ID().String(), uc.clock.Now()); err != nil {
			return err
		}
		return uc.events.Publish(ctx, user.PullEvents())
	})
}
