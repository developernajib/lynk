package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// relayBatchSize bounds one claim so a backlog drains in chunks.
const relayBatchSize = 100

// relayInterval is the poll tick. Polling is simple and sufficient; a
// LISTEN/NOTIFY wake-up is a documented optimization seam, not a default.
const relayInterval = time.Second

// OutboxRelay moves committed outbox rows onto JetStream: claim a batch with
// FOR UPDATE SKIP LOCKED (replicas never double-claim), publish, mark
// published. Delivery is at-least-once; consumers pick an idempotency
// strategy accordingly.
type OutboxRelay struct {
	txManager *postgres.TxManager
	bus       *nats.Connection
	log       zerolog.Logger
}

// NewOutboxRelay builds the relay.
func NewOutboxRelay(txManager *postgres.TxManager, bus *nats.Connection, log zerolog.Logger) *OutboxRelay {
	return &OutboxRelay{txManager: txManager, bus: bus, log: log}
}

// Run ticks until ctx is cancelled; wired as a worker runner.
func (r *OutboxRelay) Run(ctx context.Context) error {
	ticker := time.NewTicker(relayInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.relayBatch(ctx); err != nil && ctx.Err() == nil {
				r.log.Error().Err(err).Msg("example outbox relay batch failed")
			}
		}
	}
}

// relayBatch runs one claim-publish-mark cycle in a single transaction so
// the row locks from SKIP LOCKED hold until the marks commit.
func (r *OutboxRelay) relayBatch(ctx context.Context) error {
	return r.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		tx, _ := postgres.TxFromContext(ctx)
		querier := db.New(tx)

		events, err := querier.ClaimUnpublishedOutboxEvents(ctx, relayBatchSize)
		if err != nil {
			return fmt.Errorf("claim outbox events: %w", err)
		}

		for _, event := range events {
			// Publish waits for the stream's ack: the event is durably in
			// JetStream before the row is marked, giving at-least-once.
			if _, err := r.bus.JetStream.Publish(ctx, event.Subject, event.Payload); err != nil {
				return fmt.Errorf("publish %s: %w", event.Subject, err)
			}
			err = querier.MarkOutboxEventPublished(ctx, db.MarkOutboxEventPublishedParams{
				ID:          event.ID,
				PublishedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			})
			if err != nil {
				return fmt.Errorf("mark published: %w", err)
			}
		}
		return nil
	})
}
