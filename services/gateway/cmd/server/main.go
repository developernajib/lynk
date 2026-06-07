// The gateway binary serves the public edge: the hardened middleware chain
// and the gRPC-Web to gRPC bridge in front of the backend services.
package main

import (
	"fmt"
	"os"

	"github.com/developernajib/lynk/services/gateway/internal/bootstrap"
)

func main() {
	if err := bootstrap.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gateway:", err)
		os.Exit(1)
	}
}
