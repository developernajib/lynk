// Package audit records every core event in an append-only ledger via a
// durable JetStream consumer: audit-by-subscription, so no module can forget
// to audit. It is also the boilerplate's worked consumer example
// (EnsureConsumer bind, ack/nak/term decisions, trace continuity off the
// bus). Like authz, it is an engine-style module, not a hexagon.
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/developernajib/lynk/services/core/internal/gen/db"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
	"github.com/developernajib/lynk/services/core/internal/platform/secure"
)

// envelope mirrors the producers' outbox wire format; Payload is stored raw,
// these fields are just the ones the ledger indexes.
type envelope struct {
	Event       string    `json:"event"`
	OccurredAt  time.Time `json:"occurred_at"`
	Traceparent string    `json:"traceparent"`
}

// Consumer is the worker runner that writes the ledger.
type Consumer struct {
	pools  *postgres.Pools
	bus    *nats.Connection
	log    zerolog.Logger
	stream string
}

// NewConsumer builds the runner. stream names the stream to bind to; this
// module consumes, it never declares stream config.
func NewConsumer(pools *postgres.Pools, bus *nats.Connection, log zerolog.Logger, stream string) *Consumer {
	return &Consumer{pools: pools, bus: bus, log: log, stream: stream}
}

// Run binds the durable consumer and records messages until ctx is
// cancelled. Durable means fire-once across worker replicas and resume from
// the last acked position after a restart.
func (c *Consumer) Run(ctx context.Context) error {
	consumer, err := c.bus.EnsureConsumer(ctx, c.stream, jetstream.ConsumerConfig{
		Durable:   "audit-ledger",
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return err
	}

	consume, err := consumer.Consume(func(msg jetstream.Msg) {
		c.record(ctx, msg)
	})
	if err != nil {
		return err
	}
	defer consume.Stop()

	<-ctx.Done()
	return nil
}

// record writes one entry. Ack on success, Term on poison (unparseable JSON
// can never succeed on redelivery), Nak on transient failure so JetStream
// redelivers. Delivery is at-least-once; a crash between insert and ack can
// produce a rare duplicate entry, accepted for an append-only ledger.
func (c *Consumer) record(parent context.Context, msg jetstream.Msg) {
	var env envelope
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		c.log.Error().Err(err).Str("subject", msg.Subject()).Msg("audit: poison message, terminating")
		_ = msg.Term()
		return
	}

	// Continue the producer's trace: the traceparent captured at outbox
	// write time makes request, relay, and this consumer one trace.
	carrier := propagation.MapCarrier{}
	if env.Traceparent != "" {
		carrier.Set("traceparent", env.Traceparent)
	}
	ctx := otel.GetTextMapPropagator().Extract(parent, carrier)
	ctx, span := otel.Tracer("audit").Start(ctx, "audit.record",
		trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rawID, err := secure.UUIDv7()
	if err != nil {
		_ = msg.Nak()
		return
	}
	var id pgtype.UUID
	if err := id.Scan(rawID); err != nil {
		_ = msg.Nak()
		return
	}

	occurredAt := env.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}

	err = db.New(c.pools.Write).InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id,
		Subject:    msg.Subject(),
		Payload:    msg.Data(),
		OccurredAt: pgtype.Timestamptz{Time: occurredAt, Valid: true},
		RecordedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		c.log.Error().Err(err).Str("subject", msg.Subject()).Msg("audit: insert failed, redelivering")
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}
