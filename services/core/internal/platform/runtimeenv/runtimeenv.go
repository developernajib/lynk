// Package runtimeenv aligns the Go runtime with the container's real CPU and
// memory limits, called once at the top of main.
//
// GOMAXPROCS defaults to the host CPU count: in a container quota'd to 2 cores
// on a 64-core node that means 64 OS threads thrashing the scheduler and GC.
// GOMEMLIMIT gives the GC a soft ceiling so it collects harder near the limit
// instead of letting the kernel OOM-kill the container.
package runtimeenv

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"go.uber.org/automaxprocs/maxprocs"
)

// Configure is best-effort: a tuning failure must never stop the service from
// starting, so errors fall back to Go's defaults silently.
func Configure() {
	// automaxprocs reads the cgroup CPU quota; there is no recovery beyond
	// keeping the default, so the undo func and error are discarded.
	_, _ = maxprocs.Set()

	configureMemoryLimit()
}

// configureMemoryLimit sets GOMEMLIMIT from, in priority order: the GOMEMLIMIT
// env var (the runtime honors it natively, so leave it alone), an explicit
// MEMORY_LIMIT_MB, or the detected cgroup v2 limit. The target is 90% of the
// hard limit, leaving headroom for non-heap memory.
func configureMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}

	if raw := os.Getenv("MEMORY_LIMIT_MB"); raw != "" {
		if mb, err := strconv.ParseInt(raw, 10, 64); err == nil && mb > 0 {
			debug.SetMemoryLimit(mb * 1024 * 1024 * 9 / 10)
			return
		}
	}

	if limit := cgroupV2MemoryLimit(); limit > 0 {
		debug.SetMemoryLimit(limit * 9 / 10)
	}
}

// cgroupV2MemoryLimit returns the container memory limit in bytes, or 0 when
// unknown or unlimited ("max").
func cgroupV2MemoryLimit() int64 {
	raw, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		return 0
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "max" {
		return 0
	}
	limit, err := strconv.ParseInt(text, 10, 64)
	if err != nil || limit <= 0 {
		return 0
	}
	return limit
}
