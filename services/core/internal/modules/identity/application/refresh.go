package application

import (
	"context"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
)

// Refresh rotates a refresh token: the presented one is revoked and a new
// pair is issued, atomically. Rotation is the defense that makes a stolen
// refresh token short-lived: whichever party (victim or thief) uses it
// second presents a revoked token and is thrown out.
type Refresh struct {
	users    domain.UserRepository
	sessions domain.RefreshTokenRepository
	opaque   OpaqueTokens
	tokens   *TokenService
	clock    Clock
	uow      UnitOfWork
}

// NewRefresh wires the use case.
func NewRefresh(users domain.UserRepository, sessions domain.RefreshTokenRepository, opaque OpaqueTokens, tokens *TokenService, clock Clock, uow UnitOfWork) *Refresh {
	return &Refresh{users: users, sessions: sessions, opaque: opaque, tokens: tokens, clock: clock, uow: uow}
}

// Execute validates the presented token and rotates it in one transaction.
func (uc *Refresh) Execute(ctx context.Context, refreshRaw string) (TokenPair, error) {
	session, err := uc.sessions.GetByHash(ctx, uc.opaque.Hash(refreshRaw))
	if err != nil {
		return TokenPair{}, err
	}
	if !session.IsActive(uc.clock.Now()) {
		return TokenPair{}, domain.ErrRefreshTokenInvalid
	}

	userID, err := vo.NewUserID(session.UserID())
	if err != nil {
		return TokenPair{}, domain.ErrRefreshTokenInvalid
	}
	user, err := uc.users.GetByID(ctx, userID)
	if err != nil {
		return TokenPair{}, err
	}
	if !user.CanSignIn() {
		return TokenPair{}, domain.ErrAccountDisabled
	}

	var pair TokenPair
	err = uc.uow.WithinTransaction(ctx, func(ctx context.Context) error {
		// Revoke first: if issuing fails the transaction rolls back and the
		// old token stays valid, never the reverse (two live tokens).
		if err := uc.sessions.Revoke(ctx, session.ID(), uc.clock.Now()); err != nil {
			return err
		}
		issued, err := uc.tokens.IssuePair(ctx, user)
		if err != nil {
			return err
		}
		pair = issued
		return nil
	})
	if err != nil {
		return TokenPair{}, err
	}
	return pair, nil
}
