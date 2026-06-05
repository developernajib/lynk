// Package nats connects to NATS and exposes JetStream, the durable event
// bus. Plain NATS pub/sub stays available on Conn for ephemeral live fan-out
// (chat, dashboards); JetStream adds persistence and at-least-once delivery
// for the outbox and event-driven flows.
//
// Stream ownership rule: exactly ONE service (the producer) declares a
// stream's configuration via EnsureStream. Every consumer, in this service or
// another, attaches with EnsureConsumer, which binds to the existing stream
// and can never rewrite its subjects. Two services both "ensuring" one stream
// with different subject lists silently diverges on every restart; the API
// split makes that mistake impossible.
package nats

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config holds the connection settings.
type Config struct {
	// URL is the nats:// address. Required.
	URL string
	// ClientName labels this client in NATS monitoring.
	ClientName string
	// StreamReplicas is the replica count for streams this service owns:
	// 1 in development, 3 in production for quorum durability. Default 1.
	StreamReplicas int
}

// Connection bundles the live connection with its JetStream handle so
// bootstrap wires "the bus" as one dependency.
type Connection struct {
	// Conn is the underlying connection, also used for plain pub/sub fan-out.
	Conn *nats.Conn
	// JetStream is the durable stream API used by relays and consumers.
	JetStream jetstream.JetStream
	replicas  int
}

// Connect dials NATS and initializes JetStream. The bus is core
// infrastructure, so the client retries forever rather than giving up.
func Connect(cfg Config) (*Connection, error) {
	conn, err := nats.Connect(
		cfg.URL,
		nats.MaxReconnects(-1),
		nats.RetryOnFailedConnect(true),
		nats.Name(cfg.ClientName),
	)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("nats: init jetstream: %w", err)
	}

	replicas := cfg.StreamReplicas
	if replicas <= 0 {
		replicas = 1
	}
	return &Connection{Conn: conn, JetStream: js, replicas: replicas}, nil
}

// Close drains in-flight messages before closing, so queued publishes and
// acks finish as part of graceful shutdown.
func (c *Connection) Close() error {
	if err := c.Conn.Drain(); err != nil {
		return fmt.Errorf("nats: drain: %w", err)
	}
	return nil
}

// EnsureStream declares a stream this service OWNS. Idempotent: it converges
// the stream to this configuration on every boot, which is exactly why only
// the owning producer may call it. JetStream publish requires a stream bound
// to the subject, so owners call this before their relay starts.
func (c *Connection) EnsureStream(ctx context.Context, name string, subjects []string) error {
	_, err := c.JetStream.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     name,
		Subjects: subjects,
		// File storage persists events across restarts.
		Storage:  jetstream.FileStorage,
		Replicas: c.replicas,
	})
	if err != nil {
		return fmt.Errorf("nats: ensure stream %s: %w", name, err)
	}
	return nil
}

// EnsureConsumer attaches a durable consumer to a stream WITHOUT touching the
// stream's configuration. Binding fails when the stream does not exist yet,
// which is the correct startup error: the owner has not run, and quietly
// creating the stream here with this consumer's idea of its config is how
// subject lists diverge.
func (c *Connection) EnsureConsumer(ctx context.Context, stream string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	consumer, err := c.JetStream.CreateOrUpdateConsumer(ctx, stream, cfg)
	if err != nil {
		return nil, fmt.Errorf("nats: ensure consumer %s on stream %s: %w", cfg.Durable, stream, err)
	}
	return consumer, nil
}
