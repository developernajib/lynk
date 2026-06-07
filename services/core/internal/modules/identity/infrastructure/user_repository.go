package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain/vo"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// uniqueViolation is the Postgres error code for a unique-index conflict.
const uniqueViolation = "23505"

// UserRepository persists User aggregates through sqlc.
type UserRepository struct {
	pools *postgres.Pools
}

// NewUserRepository builds the repository.
func NewUserRepository(pools *postgres.Pools) *UserRepository {
	return &UserRepository{pools: pools}
}

func (r *UserRepository) writeQuerier(ctx context.Context) *db.Queries {
	if tx, ok := postgres.TxFromContext(ctx); ok {
		return db.New(tx)
	}
	return db.New(r.pools.Write)
}

// Auth reads stay on the PRIMARY: a login must see the password set a
// millisecond ago, and replica lag on credentials is a security bug, not a
// performance trade-off.
func (r *UserRepository) authQuerier(ctx context.Context) *db.Queries {
	return r.writeQuerier(ctx)
}

// Create inserts the user, mapping the unique email index to the domain
// sentinel. The index, not a pre-check, is the duplicate guard: pre-checks
// race concurrent registrations.
func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	id, err := uuidFromString(user.ID().String())
	if err != nil {
		return err
	}
	err = r.writeQuerier(ctx).CreateUser(ctx, db.CreateUserParams{
		ID:           id,
		Email:        user.Email().String(),
		PasswordHash: user.PasswordHash(),
		FullName:     user.FullName(),
		Role:         user.Role(),
		Status:       string(user.Status()),
		Version:      user.Version(),
		CreatedAt:    pgtype.Timestamptz{Time: user.CreatedAt(), Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: user.UpdatedAt(), Valid: true},
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("identity: create user: %w", err)
	}
	return nil
}

// GetByEmail loads by canonical email from the primary.
func (r *UserRepository) GetByEmail(ctx context.Context, email vo.Email) (*domain.User, error) {
	row, err := r.authQuerier(ctx).GetUserByEmail(ctx, email.String())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("identity: get user by email: %w", err)
	}
	return userFromRow(row)
}

// GetByID loads by id from the primary.
func (r *UserRepository) GetByID(ctx context.Context, id vo.UserID) (*domain.User, error) {
	pgID, err := uuidFromString(id.String())
	if err != nil {
		return nil, err
	}
	row, err := r.authQuerier(ctx).GetUserByID(ctx, pgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("identity: get user by id: %w", err)
	}
	return userFromRow(row)
}

// Update saves with the optimistic-lock guard.
func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	id, err := uuidFromString(user.ID().String())
	if err != nil {
		return err
	}
	affected, err := r.writeQuerier(ctx).UpdateUser(ctx, db.UpdateUserParams{
		ID:           id,
		PasswordHash: user.PasswordHash(),
		FullName:     user.FullName(),
		Role:         user.Role(),
		Status:       string(user.Status()),
		UpdatedAt:    pgtype.Timestamptz{Time: user.UpdatedAt(), Valid: true},
		Version:      user.Version(),
	})
	if err != nil {
		return fmt.Errorf("identity: update user: %w", err)
	}
	if affected == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

// MarkEmailVerified stamps the verification time; already-verified rows are
// untouched, so the operation is idempotent.
func (r *UserRepository) MarkEmailVerified(ctx context.Context, id vo.UserID, now time.Time) error {
	pgID, err := uuidFromString(id.String())
	if err != nil {
		return err
	}
	err = r.writeQuerier(ctx).SetEmailVerified(ctx, db.SetEmailVerifiedParams{
		ID:              pgID,
		EmailVerifiedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("identity: mark email verified: %w", err)
	}
	return nil
}

func userFromRow(row db.IdentityUser) (*domain.User, error) {
	id, err := vo.NewUserID(uuidToString(row.ID))
	if err != nil {
		return nil, fmt.Errorf("identity: corrupt user id: %w", err)
	}
	email, err := vo.NewEmail(row.Email)
	if err != nil {
		return nil, fmt.Errorf("identity: corrupt user email: %w", err)
	}
	return domain.UserFromState(
		id, email, row.PasswordHash, row.FullName, row.Role,
		domain.Status(row.Status), row.Version,
		row.CreatedAt.Time, row.UpdatedAt.Time,
	), nil
}
