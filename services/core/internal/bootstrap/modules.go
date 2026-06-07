// Package bootstrap is the service's composition root: it builds the
// platform pieces, assembles modules, and owns the two process lifecycles
// (server and worker). Adding a module touches exactly this file plus the
// stream subject list.
package bootstrap

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	"github.com/developernajib/lynk/services/core/internal/modules/example"
	"github.com/developernajib/lynk/services/core/internal/platform/config"
	"github.com/developernajib/lynk/services/core/internal/platform/nats"
	"github.com/developernajib/lynk/services/core/internal/platform/postgres"
)

// coreStream is the JetStream stream this service OWNS: only this service's
// worker declares it (EnsureStream); every consumer binds.
const coreStream = "CORE_EVENTS"

// coreStreamSubjects must list every module's subject prefix. The worker
// converges the stream to exactly this list on boot.
var coreStreamSubjects = []string{
	"example.>",
}

// Modules holds every assembled module so the server can register handlers
// and the worker can collect relays from one place.
type Modules struct {
	Example *example.Module
}

// buildModules assembles all modules. redisClient is part of the standard
// module diet (caches, throttles) even though the example does not use it.
func buildModules(
	_ *config.Config,
	log zerolog.Logger,
	pools *postgres.Pools,
	_ *redis.Client,
	bus *nats.Connection,
) *Modules {
	return &Modules{
		Example: example.New(example.Dependencies{Pools: pools, Bus: bus, Log: log}),
	}
}

// RegisterAll mounts every module's gRPC service onto the server.
func (m *Modules) RegisterAll(server *grpc.Server) {
	m.Example.Register(server)
}

// Runners returns every background loop the worker must run: outbox relays,
// consumers, and scheduled jobs.
func (m *Modules) Runners() []runner {
	return []runner{
		m.Example.Relay(),
	}
}

// runner is anything that blocks in Run until its context is cancelled.
type runner interface {
	Run(ctx context.Context) error
}
