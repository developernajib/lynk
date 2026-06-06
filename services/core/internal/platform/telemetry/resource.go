package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// buildResource assembles the attributes attached to every span and metric
// this process emits. The semconv constants are the agreed vocabulary across
// the OTel ecosystem, so backends interpret the keys correctly.
func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	built, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Env),
		),
		// host.name locates which instance emitted a signal.
		resource.WithHost(),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}
	return built, nil
}
