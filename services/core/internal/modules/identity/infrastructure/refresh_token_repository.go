package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// RefreshTokenRepository persists sessions. Like all auth state, reads stay
// on the primary: a replica-lagged "still active" answer for a just-revoked
// token would be a security hole.
type RefreshTokenRepository struct {
	pools *postgres.Pools
}

// NewRefreshTokenRepository builds the repository.
func NewRefreshTokenRepository(pools *postgres.Pools) *RefreshTokenRepository {
	return &RefreshTokenRepository{pools: pools}
}

func (r *RefreshTokenRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := postgres.TxFromContext(ctx); ok {
		return db.New(tx)
	}
	return db.New(r.pools.Write)
}

// Create records a freshly issued session.
func (r *RefreshTokenRepository) Create(ctx context.Context, token *domain.RefreshToken) error {
	id, err := uuidFromString(token.ID())
	if err != nil {
		return err
	}
	userID, err := uuidFromString(token.UserID())
	if err != nil {
		return err
	}
	err = r.querier(ctx).CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		ID:        id,
		UserID:    userID,
		TokenHash: token.TokenHash(),
		ExpiresAt: pgtype.Timestamptz{Time: token.ExpiresAt(), Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: token.CreatedAt(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("identity: create refresh token: %w", err)
	}
	return nil
}

// GetByHash loads by token hash.
func (r *RefreshTokenRepository) GetByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	row, err := r.querier(ctx).GetRefreshTokenByHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrRefreshTokenInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("identity: get refresh token: %w", err)
	}

	var revokedAt *time.Time
	if row.RevokedAt.Valid {
		revoked := row.RevokedAt.Time
		revokedAt = &revoked
	}
	return domain.RefreshTokenFromState(
		uuidToString(row.ID), uuidToString(row.UserID), row.TokenHash,
		row.ExpiresAt.Time, revokedAt, row.CreatedAt.Time,
	), nil
}

// Revoke marks one session unusable; revoking twice is a no-op.
func (r *RefreshTokenRepository) Revoke(ctx context.Context, id string, now time.Time) error {
	pgID, err := uuidFromString(id)
	if err != nil {
		return err
	}
	err = r.querier(ctx).RevokeRefreshToken(ctx, db.RevokeRefreshTokenParams{
		ID:        pgID,
		RevokedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("identity: revoke refresh token: %w", err)
	}
	return nil
}

// RevokeAllForUser ends every live session.
func (r *RefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID string, now time.Time) error {
	pgID, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	err = r.querier(ctx).RevokeAllRefreshTokensForUser(ctx, db.RevokeAllRefreshTokensForUserParams{
		UserID:    pgID,
		RevokedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("identity: revoke all refresh tokens: %w", err)
	}
	return nil
}
