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

// APIKeyRepository persists machine credentials. Auth state reads stay on
// the primary, same as users and sessions.
type APIKeyRepository struct {
	pools *postgres.Pools
}

// NewAPIKeyRepository builds the repository.
func NewAPIKeyRepository(pools *postgres.Pools) *APIKeyRepository {
	return &APIKeyRepository{pools: pools}
}

func (r *APIKeyRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := postgres.TxFromContext(ctx); ok {
		return db.New(tx)
	}
	return db.New(r.pools.Write)
}

// Create records a freshly minted key.
func (r *APIKeyRepository) Create(ctx context.Context, key *domain.APIKey) error {
	id, err := uuidFromString(key.ID())
	if err != nil {
		return err
	}
	userID, err := uuidFromString(key.UserID())
	if err != nil {
		return err
	}
	err = r.querier(ctx).CreateAPIKey(ctx, db.CreateAPIKeyParams{
		ID:        id,
		UserID:    userID,
		Name:      key.Name(),
		KeyHash:   key.KeyHash(),
		Prefix:    key.Prefix(),
		CreatedAt: pgtype.Timestamptz{Time: key.CreatedAt(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("identity: create api key: %w", err)
	}
	return nil
}

// ListForUser returns the owner's keys, newest first.
func (r *APIKeyRepository) ListForUser(ctx context.Context, userID string) ([]*domain.APIKey, error) {
	pgUserID, err := uuidFromString(userID)
	if err != nil {
		return nil, err
	}
	rows, err := r.querier(ctx).ListAPIKeysForUser(ctx, pgUserID)
	if err != nil {
		return nil, fmt.Errorf("identity: list api keys: %w", err)
	}

	keys := make([]*domain.APIKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, apiKeyFromRow(row))
	}
	return keys, nil
}

// GetByHash loads by key hash.
func (r *APIKeyRepository) GetByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	row, err := r.querier(ctx).GetAPIKeyByHash(ctx, keyHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("identity: get api key: %w", err)
	}
	return apiKeyFromRow(row), nil
}

// Revoke disables one of the owner's keys; zero rows means not theirs or
// already revoked.
func (r *APIKeyRepository) Revoke(ctx context.Context, id, userID string, now time.Time) error {
	pgID, err := uuidFromString(id)
	if err != nil {
		return err
	}
	pgUserID, err := uuidFromString(userID)
	if err != nil {
		return err
	}
	affected, err := r.querier(ctx).RevokeAPIKey(ctx, db.RevokeAPIKeyParams{
		ID:        pgID,
		UserID:    pgUserID,
		RevokedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("identity: revoke api key: %w", err)
	}
	if affected == 0 {
		return domain.ErrAPIKeyNotFound
	}
	return nil
}

func apiKeyFromRow(row db.IdentityApiKey) *domain.APIKey {
	var revokedAt *time.Time
	if row.RevokedAt.Valid {
		revoked := row.RevokedAt.Time
		revokedAt = &revoked
	}
	return domain.APIKeyFromState(
		uuidToString(row.ID), uuidToString(row.UserID), row.Name,
		row.KeyHash, row.Prefix, row.CreatedAt.Time, revokedAt,
	)
}
