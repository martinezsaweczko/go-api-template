// Package observability bootstraps the OpenTelemetry tracer and meter
// providers. Logs are not routed through OpenTelemetry: the application logs
// JSON to stdout with log/slog.
package observability

import (
	"context"
	"errors"
	"fmt"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// ObservabilityInst holds the providers the HTTP layer instruments with.
type ObservabilityInst struct {
	// Trace is an SDK provider exporting over OTLP, or a no-op provider when
	// no OTLP endpoint is configured.
	Trace trace.TracerProvider
	// MeterProvider feeds the Prometheus exporter, or is a no-op provider
	// when metrics are disabled.
	MeterProvider metric.MeterProvider
	// Propagator extracts and injects W3C trace context and baggage.
	Propagator propagation.TextMapPropagator
}

// SetupOTelSDK bootstraps the OpenTelemetry pipeline and registers the
// providers globally. serviceVersion is reported as service.version.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func SetupOTelSDK(ctx context.Context, cfg *config.ObservabilityConfig, serviceVersion string) (func(context.Context) error, ObservabilityInst, error) {
	var shutdownFuncs []func(context.Context) error

	// shutdown calls the registered cleanup functions once and joins their errors.
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	inst := ObservabilityInst{Propagator: newPropagator()}
	otel.SetTextMapPropagator(inst.Propagator)

	res, err := newResource(ctx, cfg.ServiceName, serviceVersion)
	if err != nil {
		return shutdown, inst, err
	}

	// Without an OTLP endpoint tracing is a no-op: spans are neither recorded
	// nor exported, while propagated trace context still flows through.
	if cfg.OTLPEndpoint != "" {
		tracerProvider, err := newTracerProvider(ctx, cfg.OTLPEndpoint, res)
		if err != nil {
			return shutdown, inst, errors.Join(err, shutdown(ctx))
		}
		shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
		inst.Trace = tracerProvider
	} else {
		inst.Trace = tracenoop.NewTracerProvider()
	}
	otel.SetTracerProvider(inst.Trace)

	// Metrics are exposed only through the Prometheus endpoint, so they are
	// a no-op when no metrics path is configured.
	if cfg.MetricsPath != "" {
		meterProvider, err := newMeterProvider(res)
		if err != nil {
			return shutdown, inst, errors.Join(err, shutdown(ctx))
		}
		shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
		inst.MeterProvider = meterProvider
	} else {
		inst.MeterProvider = metricnoop.NewMeterProvider()
	}
	otel.SetMeterProvider(inst.MeterProvider)

	return shutdown, inst, nil
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// newResource describes this service. Standard OTEL_RESOURCE_ATTRIBUTES are
// honoured, but the configured service name and version take precedence.
func newResource(ctx context.Context, serviceName, serviceVersion string) (*resource.Resource, error) {
	attrs := []resource.Option{
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	}
	if serviceVersion != "" {
		attrs = append(attrs, resource.WithAttributes(semconv.ServiceVersion(serviceVersion)))
	}
	res, err := resource.New(ctx, attrs...)
	if err != nil {
		return nil, fmt.Errorf("create OpenTelemetry resource: %w", err)
	}
	return res, nil
}

func newTracerProvider(ctx context.Context, endpoint string, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	), nil
}

func newMeterProvider(res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	// The Prometheus exporter is both a metric Reader and a
	// prometheus.Collector registered on the default registry served by
	// promhttp.Handler().
	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("create Prometheus exporter: %w", err)
	}
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	), nil
}
