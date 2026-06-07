// The worker binary runs the service's background loops: outbox relays,
// event consumers, and scheduled jobs.
package main

import (
	"fmt"
	"os"

	"github.com/developernajib/lynk/services/core/internal/bootstrap"
)

func main() {
	if err := bootstrap.RunWorker(); err != nil {
		fmt.Fprintln(os.Stderr, "core worker:", err)
		os.Exit(1)
	}
}
