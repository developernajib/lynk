package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txContextKey is the private key type carrying the active tx in a context.
type txContextKey struct{}

// TxManager runs callbacks inside a single database transaction, which is
// what makes the outbox pattern atomic: a use case persists its state change
// and its outbox event in one commit, eliminating the dual-write race.
// Transactions are a write concern, so they always run on the primary pool.
type TxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager builds the manager on the write pool.
func NewTxManager(write *pgxpool.Pool) *TxManager {
	return &TxManager{pool: write}
}

// WithinTransaction runs fn inside a transaction: rollback on error or panic,
// commit otherwise. The context passed to fn carries the tx, so repository
// calls inside fn join it via TxFromContext without signature changes.
//
// Nesting-aware: when the context already carries a transaction, fn joins the
// outer one instead of opening a second, independently-committing
// transaction. An inner use case reused by an outer one therefore stays
// atomic with its caller.
func (m *TxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, active := TxFromContext(ctx); active {
		return fn(ctx)
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}

	// A panic mid-transaction must never leave a connection with an open tx:
	// roll back, then let the panic continue unwinding.
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)
			panic(r)
		}
	}()

	if err := fn(withTx(ctx, tx)); err != nil {
		// The rollback error is secondary to the original cause.
		_ = tx.Rollback(ctx)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit tx: %w", err)
	}
	return nil
}

func withTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// TxFromContext returns the active transaction, if any. Repositories use it
// to pick the tx over the pool so writes join the current unit of work.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(pgx.Tx)
	return tx, ok
}
