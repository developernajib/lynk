// Package application holds the example module's use cases, one per file.
// It depends on the domain and on the ports declared here; infrastructure
// supplies the adapters at assembly time (module.go).
package application

import (
	"context"
	"time"

	"github.com/developernajib/lynk/services/core/internal/modules/example/domain"
)

// Ports are declared on the CONSUMER side (here), sized to exactly what the
// use cases need: accept interfaces, return structs.

// Clock supplies time so use cases stay deterministic and testable.
type Clock interface {
	Now() time.Time
}

// IDGenerator mints new aggregate ids (UUIDv7 in production). The domain
// only validates shape; generation is an infrastructure concern.
type IDGenerator interface {
	NewID() (string, error)
}

// EventPublisher records domain events. The production adapter writes outbox
// rows inside the caller's transaction, which is what makes event publishing
// atomic with the state change.
type EventPublisher interface {
	Publish(ctx context.Context, events []domain.Event) error
}

// UnitOfWork runs fn atomically. The platform TxManager satisfies it; the
// indirection keeps the application layer free of pgx.
type UnitOfWork interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
