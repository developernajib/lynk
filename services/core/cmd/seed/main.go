// The seed binary creates the development admin account so a fresh clone
// has an admin principal without hand-editing the database. Idempotent: an
// existing user with the seed email is promoted to admin instead of failing.
//
// It writes through sqlc directly rather than the identity use cases: a
// seeder is operator tooling, not a request path, and must not depend on
// Redis or NATS being up.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/platform/config"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotenv(); err != nil {
		return err
	}

	env := config.String("APP_ENV", "development")
	email := config.String("SEED_ADMIN_EMAIL", "admin@example.com")
	password := config.String("SEED_ADMIN_PASSWORD", "")
	name := config.String("SEED_ADMIN_NAME", "Admin")
	writeURL := config.String("DB_WRITE_URL", "")
	if writeURL == "" {
		return fmt.Errorf("DB_WRITE_URL is required")
	}

	// A generated default password is fine for development; production must
	// state one explicitly so no deploy ever ships a guessable admin.
	defaultPassword := password == ""
	if defaultPassword {
		if env == "production" {
			return fmt.Errorf("SEED_ADMIN_PASSWORD is required in production")
		}
		generated, err := secure.Token(12)
		if err != nil {
			return err
		}
		password = generated
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pools, err := postgres.Connect(ctx, postgres.Config{WriteURL: writeURL})
	if err != nil {
		return err
	}
	defer pools.Close()

	queries := db.New(pools.Write)
	now := time.Now()

	// Existing account: promote, never overwrite the password.
	if _, err := queries.GetUserByEmail(ctx, email); err == nil {
		affected, err := queries.SetUserRoleByEmail(ctx, db.SetUserRoleByEmailParams{
			Email:     email,
			Role:      "admin",
			UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("promote %s: %w", email, err)
		}
		fmt.Printf("seed: %s already exists, promoted to admin (%d row)\n", email, affected)
		return nil
	}

	hash, err := secure.HashPassword(password)
	if err != nil {
		return err
	}
	rawID, err := secure.UUIDv7()
	if err != nil {
		return err
	}
	var id pgtype.UUID
	if err := id.Scan(rawID); err != nil {
		return err
	}

	err = queries.CreateUser(ctx, db.CreateUserParams{
		ID:           id,
		Email:        email,
		PasswordHash: hash,
		FullName:     name,
		Role:         "admin",
		Status:       "active",
		Version:      1,
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}

	fmt.Printf("seed: created admin %s (id %s)\n", email, rawID)
	if defaultPassword {
		// Printed once on creation; it is never stored in plaintext anywhere.
		fmt.Printf("seed: generated password: %s  (set SEED_ADMIN_PASSWORD to choose your own)\n", password)
	}
	return nil
}
