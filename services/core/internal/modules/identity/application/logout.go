package application

import (
	"context"
	"time"

	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
)

// Logout ends a session: the refresh token is revoked in the database and
// the access token's jti is blacklisted until it would expire anyway, so
// both halves of the pair die together.
type Logout struct {
	sessions  domain.RefreshTokenRepository
	opaque    OpaqueTokens
	blacklist TokenBlacklist
	clock     Clock
	accessTTL time.Duration
}

// NewLogout wires the use case.
func NewLogout(sessions domain.RefreshTokenRepository, opaque OpaqueTokens, blacklist TokenBlacklist, clock Clock, accessTTL time.Duration) *Logout {
	return &Logout{sessions: sessions, opaque: opaque, blacklist: blacklist, clock: clock, accessTTL: accessTTL}
}

// Execute revokes the session. Logout is idempotent and forgiving: an
// already-invalid refresh token still results in "logged out".
func (uc *Logout) Execute(ctx context.Context, refreshRaw, accessTokenID string) error {
	if session, err := uc.sessions.GetByHash(ctx, uc.opaque.Hash(refreshRaw)); err == nil {
		if err := uc.sessions.Revoke(ctx, session.ID(), uc.clock.Now()); err != nil {
			return err
		}
	}

	if accessTokenID != "" {
		// The blacklist TTL is the full access lifetime: marginally generous
		// (the token may be older), maximally simple, and self-cleaning.
		if err := uc.blacklist.Revoke(ctx, accessTokenID, uc.accessTTL); err != nil {
			return err
		}
	}
	return nil
}
