// The server binary serves the service's gRPC API. All assembly lives in
// internal/bootstrap; main stays a thin error boundary.
package main

import (
	"fmt"
	"os"

	"github.com/developernajib/lynk/services/core/internal/bootstrap"
)

func main() {
	if err := bootstrap.RunServer(); err != nil {
		fmt.Fprintln(os.Stderr, "core server:", err)
		os.Exit(1)
	}
}
