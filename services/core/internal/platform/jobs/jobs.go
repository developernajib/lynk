// Package jobs runs scheduled background jobs that fire once cluster-wide.
//
// Every job takes a Redis leader lease before running; the API gives no way
// to register a job without one, because "every instance ran every job" is
// how duplicate invoices happen. Two distinct guards exist on purpose:
//
//   - The lease (here) prevents CONCURRENT runs across instances.
//   - A run marker (MarkOnce) prevents REPEATED runs within a period, e.g.
//     one monthly report per month even across restarts.
//
// Each job also declares what to do when Redis itself is down: a cleanup job
// fails open (running twice is annoying), a billing job fails closed (charging
// twice is unacceptable).
package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/developernajib/lynk/services/core/internal/platform/lock"
	"github.com/developernajib/lynk/services/core/internal/platform/safe"
)

// FailureMode says what a job does when the lease cannot be CHECKED because
// Redis is unreachable (distinct from the lease being held by someone else).
type FailureMode int

const (
	// FailClosed skips the run: correctness over liveness (money, billing).
	FailClosed FailureMode = iota
	// FailOpen runs anyway: liveness over single-fire (cleanups, monitors).
	FailOpen
)

// Job is one scheduled task.
type Job struct {
	// Name keys the lease ("job:<name>") and labels logs.
	Name string
	// Every is the tick interval.
	Every time.Duration
	// LeaseTTL must cover the worst-case run so the lease outlives the work.
	// Default: twice the interval, capped at 10m.
	LeaseTTL time.Duration
	// Mode is the Redis-down policy. The zero value is FailClosed: the safe
	// default for anything someone forgot to think about.
	Mode FailureMode
	// Run does the work. It must respect ctx cancellation.
	Run func(ctx context.Context) error
}

// Runner ticks registered jobs until its context is cancelled.
type Runner struct {
	locker *lock.Locker
	log    zerolog.Logger
	jobs   []Job
}

// NewRunner builds a Runner on the shared Redis client.
func NewRunner(client *redis.Client, log zerolog.Logger) *Runner {
	return &Runner{locker: lock.New(client), log: log}
}

// Register adds a job, validating it eagerly so a malformed job fails at
// startup, not at its first tick.
func (r *Runner) Register(job Job) error {
	if job.Name == "" || job.Run == nil || job.Every <= 0 {
		return fmt.Errorf("jobs: job needs a name, an interval, and a Run func")
	}
	if job.LeaseTTL <= 0 {
		job.LeaseTTL = min(2*job.Every, 10*time.Minute)
	}
	r.jobs = append(r.jobs, job)
	return nil
}

// Run starts one ticker goroutine per job and blocks until ctx is cancelled.
// Wired as a worker runner so graceful shutdown stops every ticker.
func (r *Runner) Run(ctx context.Context) error {
	for _, job := range r.jobs {
		safe.Go(r.log, "job:"+job.Name, func() { r.tick(ctx, job) })
	}
	<-ctx.Done()
	return nil
}

func (r *Runner) tick(ctx context.Context, job Job) {
	ticker := time.NewTicker(job.Every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx, job)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context, job Job) {
	lease, ok, err := r.locker.Acquire(ctx, "job:"+job.Name, job.LeaseTTL)
	if err != nil {
		if job.Mode == FailClosed {
			r.log.Warn().Err(err).Str("job", job.Name).Msg("lease check failed, skipping run (fail-closed)")
			return
		}
		r.log.Warn().Err(err).Str("job", job.Name).Msg("lease check failed, running anyway (fail-open)")
	} else if !ok {
		// Another instance holds the lease: working as designed, no log.
		return
	}

	start := time.Now()
	if err := job.Run(ctx); err != nil {
		r.log.Error().Err(err).Str("job", job.Name).Dur("took", time.Since(start)).Msg("job failed")
	} else {
		r.log.Info().Str("job", job.Name).Dur("took", time.Since(start)).Msg("job done")
	}

	if lease != nil {
		// Best-effort: an unreleased lease just expires by TTL.
		if err := lease.Release(ctx); err != nil {
			r.log.Warn().Err(err).Str("job", job.Name).Msg("lease release failed")
		}
	}
}

// MarkOnce records that a named period's work happened (SET NX with ttl) and
// reports whether this caller won the right to do it. Use it INSIDE a job for
// once-per-period semantics, e.g. MarkOnce(ctx, client, "report:2026-06",
// 40*24*time.Hour) before generating June's report.
func MarkOnce(ctx context.Context, client *redis.Client, key string, ttl time.Duration) (bool, error) {
	ok, err := client.SetNX(ctx, key, "done", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("jobs: mark once %s: %w", key, err)
	}
	return ok, nil
}
