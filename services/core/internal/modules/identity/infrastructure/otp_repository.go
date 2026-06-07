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

// OTPRepository persists one-time codes.
type OTPRepository struct {
	pools *postgres.Pools
}

// NewOTPRepository builds the repository.
func NewOTPRepository(pools *postgres.Pools) *OTPRepository {
	return &OTPRepository{pools: pools}
}

func (r *OTPRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := postgres.TxFromContext(ctx); ok {
		return db.New(tx)
	}
	return db.New(r.pools.Write)
}

// Create records a freshly issued code.
func (r *OTPRepository) Create(ctx context.Context, otp *domain.OTP) error {
	id, err := uuidFromString(otp.ID())
	if err != nil {
		return err
	}
	userID, err := uuidFromString(otp.UserID())
	if err != nil {
		return err
	}
	err = r.querier(ctx).CreateOTP(ctx, db.CreateOTPParams{
		ID:        id,
		UserID:    userID,
		Purpose:   string(otp.Purpose()),
		CodeHash:  otp.CodeHash(),
		ExpiresAt: pgtype.Timestamptz{Time: otp.ExpiresAt(), Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: otp.CreatedAt(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("identity: create otp: %w", err)
	}
	return nil
}

// GetActive returns the newest live code for a user and purpose.
func (r *OTPRepository) GetActive(ctx context.Context, userID string, purpose domain.OTPPurpose, now time.Time) (*domain.OTP, error) {
	pgUserID, err := uuidFromString(userID)
	if err != nil {
		return nil, err
	}
	row, err := r.querier(ctx).GetActiveOTP(ctx, db.GetActiveOTPParams{
		UserID:    pgUserID,
		Purpose:   string(purpose),
		ExpiresAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvalidOTP
	}
	if err != nil {
		return nil, fmt.Errorf("identity: get active otp: %w", err)
	}

	var consumedAt *time.Time
	if row.ConsumedAt.Valid {
		consumed := row.ConsumedAt.Time
		consumedAt = &consumed
	}
	return domain.OTPFromState(
		uuidToString(row.ID), uuidToString(row.UserID), domain.OTPPurpose(row.Purpose),
		row.CodeHash, row.ExpiresAt.Time, consumedAt, row.CreatedAt.Time,
	), nil
}

// Consume marks a code used; consuming twice is a no-op at the SQL level and
// surfaces as ErrInvalidOTP on the next GetActive.
func (r *OTPRepository) Consume(ctx context.Context, id string, now time.Time) error {
	pgID, err := uuidFromString(id)
	if err != nil {
		return err
	}
	_, err = r.querier(ctx).ConsumeOTP(ctx, db.ConsumeOTPParams{
		ID:         pgID,
		ConsumedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("identity: consume otp: %w", err)
	}
	return nil
}
