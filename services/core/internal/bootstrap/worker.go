package bootstrap

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/developernajib/lynk/services/core/internal/platform/jobs"
)

// RunWorker assembles and runs the background process: outbox relays, event
// consumers, and scheduled jobs. Same module, separate binary, so API
// latency and background throughput scale independently.
func RunWorker() error {
	f, err := setup("-worker")
	if err != nil {
		return err
	}

	// This service OWNS its stream: the worker is the single declarer; every
	// consumer everywhere binds (see the nats package's ownership rule).
	streamCtx, cancel := context.WithTimeout(context.Background(), initTimeout)
	err = f.bus.EnsureStream(streamCtx, coreStream, coreStreamSubjects)
	cancel()
	if err != nil {
		f.shutdown.Run()
		return err
	}

	modules := buildModules(f.cfg, f.log, f.pools, f.redis, f.bus)

	// Scheduled jobs: register them here. Every job takes a leader lease by
	// construction (the jobs package enforces it).
	jobRunner := jobs.NewRunner(f.redis, f.log)

	runners := modules.Runners()
	runners = append(runners, jobRunner)

	// The worker's ops port is offset so server and worker run side by side
	// on one host.
	ops := f.opsServer(f.cfg.App.MetricsPort + 100)
	f.shutdown.Register("ops-server", ops.Stop)

	f.log.Info().Int("ops_port", f.cfg.App.MetricsPort+100).Int("runners", len(runners)).Msg("worker starting")

	// Runners respect context cancellation; components (ops server) block.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, len(runners)+1)
	for _, r := range runners {
		go func() { errCh <- r.Run(ctx) }()
	}
	go func() { errCh <- ops.Start() }()

	select {
	case <-ctx.Done():
		f.log.Info().Msg("shutdown signal received, draining")
	case err := <-errCh:
		if err != nil {
			f.log.Error().Err(err).Msg("runner failed, draining")
			f.shutdown.Run()
			return err
		}
	}
	f.shutdown.Run()
	return nil
}
