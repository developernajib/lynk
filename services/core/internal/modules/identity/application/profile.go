package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// GetProfile loads the authenticated user's profile.
type GetProfile struct {
	users domain.UserRepository
}

// NewGetProfile wires the use case.
func NewGetProfile(users domain.UserRepository) *GetProfile {
	return &GetProfile{users: users}
}

// Execute loads by the principal's id.
func (uc *GetProfile) Execute(ctx context.Context, userID string) (*domain.User, error) {
	id, err := vo.NewUserID(userID)
	if err != nil {
		return nil, domain.ErrUserNotFound
	}
	return uc.users.GetByID(ctx, id)
}
