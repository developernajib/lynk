package authz

import (
	"context"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	authzv1 "github.com/developernajib/lynk/services/core/internal/gen/proto/authz/v1"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// Dependencies is everything the module needs from the platform.
type Dependencies struct {
	Pools *postgres.Pools
	Log   zerolog.Logger
}

// Module is the assembled authorization module.
type Module struct {
	checker *Checker
	handler *handler
}

// New assembles the module.
func New(deps Dependencies) (*Module, error) {
	checker, err := NewChecker(deps.Pools, deps.Log)
	if err != nil {
		return nil, err
	}
	return &Module{
		checker: checker,
		handler: &handler{pools: deps.Pools, checker: checker},
	}, nil
}

// Start performs the initial policy load. A failure is survivable: the
// engine denies by default until a refresh succeeds, which is the safe
// direction, so bootstrap logs it instead of refusing to boot.
func (m *Module) Start(ctx context.Context) error {
	return m.checker.Refresh(ctx)
}

// Register mounts the gRPC service.
func (m *Module) Register(server *grpc.Server) {
	authzv1.RegisterAuthzServiceServer(server, m.handler)
}

// Checker exposes the decision engine for in-process guards in other
// modules: handler code calls Decide directly instead of a gRPC round-trip.
func (m *Module) Checker() *Checker {
	return m.checker
}
