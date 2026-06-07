package application

import (
	"context"
	"time"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
)

// TokenPair is what a successful authentication returns.
type TokenPair struct {
	Access     AccessToken
	RefreshRaw string
}

// TokenService issues access + refresh pairs. Shared by Login and Refresh so
// the two paths can never drift apart in how they mint sessions.
type TokenService struct {
	signer     TokenSigner
	opaque     OpaqueTokens
	sessions   domain.RefreshTokenRepository
	ids        IDGenerator
	clock      Clock
	refreshTTL time.Duration
}

// NewTokenService wires the service.
func NewTokenService(signer TokenSigner, opaque OpaqueTokens, sessions domain.RefreshTokenRepository, ids IDGenerator, clock Clock, refreshTTL time.Duration) *TokenService {
	return &TokenService{signer: signer, opaque: opaque, sessions: sessions, ids: ids, clock: clock, refreshTTL: refreshTTL}
}

// IssuePair mints a stateless access token and a stored, hashed refresh
// session. The raw refresh value is returned once and never persisted.
func (s *TokenService) IssuePair(ctx context.Context, user *domain.User) (TokenPair, error) {
	access, err := s.signer.IssueAccessToken(user.ID().String(), user.Role())
	if err != nil {
		return TokenPair{}, err
	}

	raw, hash, err := s.opaque.Generate()
	if err != nil {
		return TokenPair{}, err
	}
	sessionID, err := s.ids.NewID()
	if err != nil {
		return TokenPair{}, err
	}

	now := s.clock.Now()
	session := domain.NewRefreshToken(sessionID, user.ID().String(), hash, now.Add(s.refreshTTL), now)
	if err := s.sessions.Create(ctx, session); err != nil {
		return TokenPair{}, err
	}

	return TokenPair{Access: access, RefreshRaw: raw}, nil
}
