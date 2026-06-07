package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// Register creates a new account.
type Register struct {
	users  domain.UserRepository
	hasher PasswordHasher
	events EventPublisher
	uow    UnitOfWork
	clock  Clock
	ids    IDGenerator
}

// NewRegister wires the use case.
func NewRegister(users domain.UserRepository, hasher PasswordHasher, events EventPublisher, uow UnitOfWork, clock Clock, ids IDGenerator) *Register {
	return &Register{users: users, hasher: hasher, events: events, uow: uow, clock: clock, ids: ids}
}

// Execute validates, hashes the password, and persists user + event in one
// transaction. The unique email index is the real duplicate guard: a
// look-before-insert check would race concurrent registrations.
func (uc *Register) Execute(ctx context.Context, email, password, fullName string) (*domain.User, error) {
	address, err := vo.NewEmail(email)
	if err != nil {
		return nil, err
	}

	rawID, err := uc.ids.NewID()
	if err != nil {
		return nil, err
	}
	userID, err := vo.NewUserID(rawID)
	if err != nil {
		return nil, err
	}

	hash, err := uc.hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	user, err := domain.NewUser(userID, address, hash, fullName, uc.clock.Now())
	if err != nil {
		return nil, err
	}

	err = uc.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.users.Create(ctx, user); err != nil {
			return err
		}
		return uc.events.Publish(ctx, user.PullEvents())
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}
