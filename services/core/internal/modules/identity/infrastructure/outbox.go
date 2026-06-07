package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/modules/identity/domain"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

// envelope is the wire format on the bus (fat events + traceparent).
type envelope struct {
	Event       string    `json:"event"`
	OccurredAt  time.Time `json:"occurred_at"`
	Traceparent string    `json:"traceparent,omitempty"`
	Data        any       `json:"data"`
}

// OutboxPublisher writes event rows inside the caller's transaction.
type OutboxPublisher struct {
	pools *postgres.Pools
}

// NewOutboxPublisher builds the publisher.
func NewOutboxPublisher(pools *postgres.Pools) *OutboxPublisher {
	return &OutboxPublisher{pools: pools}
}

// Publish writes one outbox row per event.
func (p *OutboxPublisher) Publish(ctx context.Context, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}

	querier := db.New(p.pools.Write)
	if tx, ok := postgres.TxFromContext(ctx); ok {
		querier = db.New(tx)
	}

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	traceparent := carrier.Get("traceparent")

	for _, event := range events {
		rawID, err := secure.UUIDv7()
		if err != nil {
			return err
		}
		id, err := uuidFromString(rawID)
		if err != nil {
			return err
		}

		payload, err := json.Marshal(envelope{
			Event:       event.Subject(),
			OccurredAt:  time.Now(),
			Traceparent: traceparent,
			Data:        event,
		})
		if err != nil {
			return fmt.Errorf("identity: marshal event %s: %w", event.Subject(), err)
		}

		err = querier.InsertIdentityOutboxEvent(ctx, db.InsertIdentityOutboxEventParams{
			ID:         id,
			Subject:    event.Subject(),
			Payload:    payload,
			OccurredAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
		if err != nil {
			return fmt.Errorf("identity: insert outbox event: %w", err)
		}
	}
	return nil
}

const (
	relayBatchSize = 100
	relayInterval  = time.Second
)

// OutboxRelay moves committed identity events onto JetStream (claim with
// FOR UPDATE SKIP LOCKED, publish, mark), at-least-once.
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
				r.log.Error().Err(err).Msg("identity outbox relay batch failed")
			}
		}
	}
}

func (r *OutboxRelay) relayBatch(ctx context.Context) error {
	return r.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		tx, _ := postgres.TxFromContext(ctx)
		querier := db.New(tx)

		events, err := querier.ClaimUnpublishedIdentityOutboxEvents(ctx, relayBatchSize)
		if err != nil {
			return fmt.Errorf("claim outbox events: %w", err)
		}

		for _, event := range events {
			if _, err := r.bus.JetStream.Publish(ctx, event.Subject, event.Payload); err != nil {
				return fmt.Errorf("publish %s: %w", event.Subject, err)
			}
			err = querier.MarkIdentityOutboxEventPublished(ctx, db.MarkIdentityOutboxEventPublishedParams{
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
