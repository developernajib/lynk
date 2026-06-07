package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// SetUserRole reassigns a user's role attribute. The admin guard lives in
// the handler; this use case owns the state change and its event.
type SetUserRole struct {
	users  domain.UserRepository
	events EventPublisher
	uow    UnitOfWork
	clock  Clock
}

// NewSetUserRole wires the use case.
func NewSetUserRole(users domain.UserRepository, events EventPublisher, uow UnitOfWork, clock Clock) *SetUserRole {
	return &SetUserRole{users: users, events: events, uow: uow, clock: clock}
}

// Execute loads the user, changes the role, and persists state + event
// atomically.
func (uc *SetUserRole) Execute(ctx context.Context, userID, role string) (*domain.User, error) {
	id, err := vo.NewUserID(userID)
	if err != nil {
		return nil, domain.ErrUserNotFound
	}

	var updated *domain.User
	err = uc.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		user, err := uc.users.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if err := user.ChangeRole(role, uc.clock.Now()); err != nil {
			return err
		}
		if err := uc.users.Update(ctx, user); err != nil {
			return err
		}
		if err := uc.events.Publish(ctx, user.PullEvents()); err != nil {
			return err
		}
		updated = user
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
