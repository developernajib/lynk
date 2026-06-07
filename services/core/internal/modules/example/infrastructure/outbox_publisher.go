package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/modules/example/domain"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

// envelope is the wire format on the bus. Fat events: Data carries the full
// exported event struct so consumers project without calling back. The
// traceparent captured at write time lets one trace span request, outbox,
// bus, and consumer.
type envelope struct {
	Event       string    `json:"event"`
	OccurredAt  time.Time `json:"occurred_at"`
	Traceparent string    `json:"traceparent,omitempty"`
	Data        any       `json:"data"`
}

// OutboxPublisher implements application.EventPublisher by inserting rows in
// the CALLER's transaction: the event cannot be recorded without its state
// change committing, and vice versa.
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

	// Capture the current trace context once; all events of this use case
	// share the request's trace.
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
			return fmt.Errorf("example: marshal event %s: %w", event.Subject(), err)
		}

		err = querier.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{
			ID:         id,
			Subject:    event.Subject(),
			Payload:    payload,
			OccurredAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
		if err != nil {
			return fmt.Errorf("example: insert outbox event: %w", err)
		}
	}
	return nil
}
