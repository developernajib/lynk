package audit

import (
	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	auditv1 "github.com/developernajib/lynk/services/core/internal/gen/proto/audit/v1"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// Dependencies is everything the module needs from the platform.
type Dependencies struct {
	Pools *postgres.Pools
	Bus   *nats.Connection
	Log   zerolog.Logger
	// Stream names the JetStream stream to consume; the owner declares it.
	Stream string
}

// Module is the assembled audit module.
type Module struct {
	handler  *handler
	consumer *Consumer
}

// New assembles the module.
func New(deps Dependencies) *Module {
	return &Module{
		handler:  &handler{pools: deps.Pools},
		consumer: NewConsumer(deps.Pools, deps.Bus, deps.Log, deps.Stream),
	}
}

// Register mounts the gRPC service.
func (m *Module) Register(server *grpc.Server) {
	auditv1.RegisterAuditServiceServer(server, m.handler)
}

// Runner returns the ledger consumer; the worker runs it.
func (m *Module) Runner() *Consumer {
	return m.consumer
}
