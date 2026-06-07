// Package example is the copy-me module template: a complete DDD hexagon
// (domain, application, adapter, infrastructure) with a transactional outbox.
// To start a real module, copy this directory, rename the aggregate, and
// register the new module in bootstrap.
package example

import (
	"google.golang.org/grpc"

	"github.com/rs/zerolog"

	examplev1 "github.com/developernajib/lynk/services/core/internal/gen/proto/example/v1"
	exampleadapter "github.com/developernajib/lynk/services/core/internal/modules/example/adapter/grpc"
	"github.com/developernajib/lynk/services/core/internal/modules/example/application"
	"github.com/developernajib/lynk/services/core/internal/modules/example/infrastructure"
	"github.com/developernajib/lynk/services/core/internal/platform/clock"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// Dependencies is everything the module needs from the platform. A struct
// (not positional args) keeps wiring readable as modules grow.
type Dependencies struct {
	Pools *postgres.Pools
	Bus   *nats.Connection
	Log   zerolog.Logger
	// Access is the ABAC engine adapted onto this module's port.
	Access exampleadapter.AccessChecker
}

// Module is the assembled example module.
type Module struct {
	handler *exampleadapter.Handler
	relay   *infrastructure.OutboxRelay
}

// New assembles the module: infrastructure adapters into application ports
// into the gRPC handler. This is the module's composition root.
func New(deps Dependencies) *Module {
	notes := infrastructure.NewNoteRepository(deps.Pools)
	events := infrastructure.NewOutboxPublisher(deps.Pools)
	uow := postgres.NewTxManager(deps.Pools.Write)
	systemClock := clock.System{}
	ids := infrastructure.UUIDGenerator{}

	handler := exampleadapter.NewHandler(
		application.NewCreateNote(notes, events, uow, systemClock, ids),
		application.NewGetNote(notes),
		application.NewUpdateNote(notes, events, uow, systemClock),
		application.NewListNotes(notes),
		deps.Access,
	)

	return &Module{
		handler: handler,
		relay:   infrastructure.NewOutboxRelay(uow, deps.Bus, deps.Log),
	}
}

// Register mounts the module's gRPC service; called by bootstrap on the
// server process.
func (m *Module) Register(server *grpc.Server) {
	examplev1.RegisterExampleServiceServer(server, m.handler)
}

// Relay returns the outbox relay; the worker process runs it.
func (m *Module) Relay() *infrastructure.OutboxRelay {
	return m.relay
}
