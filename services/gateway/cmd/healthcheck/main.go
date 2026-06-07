// The healthcheck binary probes an HTTP endpoint and exits 0 or 1. The
// distroless runtime image has no shell or curl, so container healthchecks
// exec this instead: HEALTHCHECK CMD ["/healthcheck", "http://localhost:9090/healthz"].
package main

import (
	"context"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// #nosec G704 -- the URL is the container's own HEALTHCHECK argument
	// (localhost ops port), not request input; there is no server here.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, os.Args[1], nil)
	if err != nil {
		os.Exit(2)
	}
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- see above
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
