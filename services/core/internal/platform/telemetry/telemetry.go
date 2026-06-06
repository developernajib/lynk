// Package telemetry initializes OpenTelemetry tracing and metrics. It wires
// the global providers once at startup; everything else calls otel.Tracer and
// otel.Meter.
//
// Two design rules:
//   - Fails open. Disabled means no-op tracing with zero overhead and no
//     collector required. Enabled exports batch and drop silently when the
//     collector is down; telemetry never blocks or crashes the request path.
//   - The app knows exactly one address (Endpoint, an OTLP/gRPC collector).
//     Swapping observability backends is collector config, not code.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	clientprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config holds the telemetry settings.
type Config struct {
	// ServiceName, ServiceVersion, and Env identify this process on every
	// span and metric it emits.
	ServiceName    string
	ServiceVersion string
	Env            string
	// Enabled turns OTLP exporting on. Off is the dev default: no collector
	// needed, near-zero overhead.
	Enabled bool
	// Endpoint is the OTLP/gRPC collector address.
	Endpoint string
	// TraceSampleRatio keeps a fraction of traces (0.0 to 1.0) to bound cost
	// at high request volume.
	TraceSampleRatio float64
}

// Providers bundles the constructed providers behind one Shutdown so
// bootstrap registers a single hook for all telemetry.
type Providers struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	metricsHandler http.Handler
}

// MetricsHandler returns the handler for GET /metrics (Prometheus text
// format), the pull-based fallback: when the collector is down, a Prometheus
// server can still scrape every service directly.
func (p *Providers) MetricsHandler() http.Handler {
	return p.metricsHandler
}

// Init builds and registers the global tracer and meter providers. The
// returned Providers must have Shutdown deferred or hooked so pending spans
// and metrics flush before exit.
func Init(ctx context.Context, cfg Config) (*Providers, error) {
	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// The Prometheus bridge is always registered: a pull exporter costs
	// nothing until scraped and means /metrics works even with telemetry off.
	// A private registry (not the process-wide default) guarantees /metrics
	// serves exactly what this process registered.
	promRegistry := clientprometheus.NewRegistry()
	promExporter, err := otelprometheus.New(otelprometheus.WithRegisterer(promRegistry))
	if err != nil {
		return nil, fmt.Errorf("telemetry: prometheus exporter: %w", err)
	}
	metricsHandler := promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{})

	if !cfg.Enabled {
		// No trace exporter: spans record nothing and cost almost nothing,
		// but metrics still flow to the local registry.
		tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		meterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(promExporter),
		)
		register(tracerProvider, meterProvider)
		return &Providers{tracerProvider: tracerProvider, meterProvider: meterProvider, metricsHandler: metricsHandler}, nil
	}

	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		// Plaintext for dev/intra-cluster; TLS belongs at the mesh edge.
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: trace exporter: %w", err)
	}

	metricExporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: metric exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		// Batching keeps export off the hot path; a slow collector drops
		// batches instead of blocking callers.
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(cfg.TraceSampleRatio),
		)),
	)

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		// Push to the collector AND serve scrapes, side by side.
		sdkmetric.WithReader(promExporter),
	)

	register(tracerProvider, meterProvider)
	return &Providers{tracerProvider: tracerProvider, meterProvider: meterProvider, metricsHandler: metricsHandler}, nil
}

// register installs the global providers and, critically, the W3C text-map
// propagator. The propagator is what makes traces distributed: it writes the
// span context into carrier headers (traceparent) and reads it back on the
// receiving side. Without it, otelgrpc has nothing to inject or extract and
// every service starts its own orphan trace.
func register(tracerProvider *sdktrace.TracerProvider, meterProvider *sdkmetric.MeterProvider) {
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// Shutdown flushes and closes both providers. errors.Join runs both
// unconditionally so one failure never skips the other's flush, and callers
// can still errors.Is into either.
func (p *Providers) Shutdown(ctx context.Context) error {
	if err := errors.Join(p.tracerProvider.Shutdown(ctx), p.meterProvider.Shutdown(ctx)); err != nil {
		return fmt.Errorf("telemetry: shutdown: %w", err)
	}
	return nil
}
