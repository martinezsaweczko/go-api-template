package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/martinezsaweczko/go-api-template/internal/config"
)

func TestNewResourceDescribesService(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		wantVersion bool
	}{
		{name: "with build version", version: "v1.2.3", wantVersion: true},
		{name: "without build version", version: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := newResource(context.Background(), "orders", tt.version)
			if err != nil {
				t.Fatalf("newResource() error = %v", err)
			}
			set := res.Set()
			if got, _ := set.Value(semconv.ServiceNameKey); got.AsString() != "orders" {
				t.Errorf("service.name = %q, want orders", got.AsString())
			}
			got, ok := set.Value(semconv.ServiceVersionKey)
			if ok != tt.wantVersion || (ok && got.AsString() != tt.version) {
				t.Errorf("service.version = %q (present %v), want %q (present %v)", got.AsString(), ok, tt.version, tt.wantVersion)
			}
		})
	}
}

func TestNewResourceConfigOverridesEnv(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "from-env")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=staging")

	res, err := newResource(context.Background(), "orders", "")
	if err != nil {
		t.Fatalf("newResource() error = %v", err)
	}
	set := res.Set()
	if got, _ := set.Value(semconv.ServiceNameKey); got.AsString() != "orders" {
		t.Errorf("service.name = %q, want configured orders", got.AsString())
	}
	if got, _ := set.Value(attribute.Key("deployment.environment.name")); got.AsString() != "staging" {
		t.Errorf("deployment.environment.name = %q, want staging from OTEL_RESOURCE_ATTRIBUTES", got.AsString())
	}
}

func TestSetupWithoutEndpointsIsNoop(t *testing.T) {
	shutdown, inst, err := SetupOTelSDK(context.Background(), &config.ObservabilityConfig{ServiceName: "test"}, "v0")
	if err != nil {
		t.Fatalf("SetupOTelSDK() error = %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	// A propagated parent must survive the no-op tracer so downstream calls
	// keep the caller's trace context.
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
	_, span := inst.Trace.Tracer("test").Start(ctx, "op")
	defer span.End()

	if span.IsRecording() {
		t.Error("span is recording without an OTLP endpoint, want no-op tracing")
	}
	if got := span.SpanContext().TraceID(); got != parent.TraceID() {
		t.Errorf("trace ID = %s, want propagated %s", got, parent.TraceID())
	}
	if _, ok := inst.MeterProvider.(metricnoop.MeterProvider); !ok {
		t.Errorf("MeterProvider = %T, want no-op without a metrics path", inst.MeterProvider)
	}
	if inst.Propagator == nil {
		t.Error("Propagator is nil")
	}
}
