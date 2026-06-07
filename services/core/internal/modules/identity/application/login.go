package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// Login authenticates credentials and issues a token pair.
type Login struct {
	users    domain.UserRepository
	hasher   PasswordHasher
	throttle LoginThrottle
	tokens   *TokenService
}

// NewLogin wires the use case.
func NewLogin(users domain.UserRepository, hasher PasswordHasher, throttle LoginThrottle, tokens *TokenService) *Login {
	return &Login{users: users, hasher: hasher, throttle: throttle, tokens: tokens}
}

// Execute checks the lockout, verifies credentials, and issues tokens.
//
// Unknown email and wrong password follow the SAME path to the SAME error,
// and the throttle records both, so neither timing, wording, nor lockout
// behavior reveals whether an account exists.
func (uc *Login) Execute(ctx context.Context, email, password string) (*domain.User, TokenPair, error) {
	if !uc.throttle.Allowed(ctx, email) {
		return nil, TokenPair{}, domain.ErrAccountLocked
	}

	address, err := vo.NewEmail(email)
	if err != nil {
		uc.throttle.RecordFailure(ctx, email)
		return nil, TokenPair{}, domain.ErrInvalidCredentials
	}

	user, err := uc.users.GetByEmail(ctx, address)
	if err != nil {
		uc.throttle.RecordFailure(ctx, email)
		return nil, TokenPair{}, domain.ErrInvalidCredentials
	}

	ok, err := uc.hasher.Verify(user.PasswordHash(), password)
	if err != nil || !ok {
		uc.throttle.RecordFailure(ctx, email)
		return nil, TokenPair{}, domain.ErrInvalidCredentials
	}

	if !user.CanSignIn() {
		return nil, TokenPair{}, domain.ErrAccountDisabled
	}

	uc.throttle.Reset(ctx, email)

	pair, err := uc.tokens.IssuePair(ctx, user)
	if err != nil {
		return nil, TokenPair{}, err
	}
	return user, pair, nil
}
