package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

func newItemsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /items", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	return mux
}

type point struct {
	method, route string
	status        int64
}

func pointOf(t *testing.T, attrs attribute.Set) point {
	t.Helper()
	method, _ := attrs.Value("http.request.method")
	route, _ := attrs.Value("http.route")
	status, _ := attrs.Value("http.response.status_code")
	return point{method: method.AsString(), route: route.AsString(), status: status.AsInt64()}
}

func TestMetricsRecordsCountAndDurationByRoute(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	metrics, err := middleware.Metrics(mp)
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	h := middleware.Chain(middleware.RouteTagger(newItemsMux()), metrics, middleware.RequestID)

	requests := []struct{ method, path string }{
		{http.MethodGet, "/items/1"},
		{http.MethodGet, "/items/2"},
		{http.MethodPost, "/items"},
		{http.MethodGet, "/no/such/path"},
	}
	for _, rq := range requests {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(rq.method, rq.path, nil))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	counts := map[point]int64{}
	histCounts := map[point]uint64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "http.server.requests":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s has type %T, want Sum[int64]", m.Name, m.Data)
				}
				for _, dp := range sum.DataPoints {
					counts[pointOf(t, dp.Attributes)] += dp.Value
				}
			case "http.server.request.duration":
				hist, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("%s has type %T, want Histogram[float64]", m.Name, m.Data)
				}
				for _, dp := range hist.DataPoints {
					histCounts[pointOf(t, dp.Attributes)] += dp.Count
				}
			}
		}
	}

	want := map[point]int64{
		{method: "GET", route: "/items/{id}", status: 200}: 2,
		{method: "POST", route: "/items", status: 201}:     1,
		{method: "GET", route: "unmatched", status: 404}:   1,
	}
	if len(counts) != len(want) {
		t.Errorf("request counter series = %v, want %v", counts, want)
	}
	for p, n := range want {
		if counts[p] != n {
			t.Errorf("requests%+v = %d, want %d", p, counts[p], n)
		}
		if histCounts[p] != uint64(n) {
			t.Errorf("duration count%+v = %d, want %d", p, histCounts[p], n)
		}
	}
}

func TestTracingParentsHandlerSpansUnderServerSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	prop := propagation.TraceContext{}

	var ctxValid bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctxValid = trace.SpanContextFromContext(r.Context()).IsValid()
		_, child := tp.Tracer("test").Start(r.Context(), "load item")
		child.End()
		w.WriteHeader(http.StatusOK)
	})
	h := middleware.Chain(middleware.RouteTagger(mux), middleware.Tracing(tp, prop), middleware.RequestID)

	// Incoming W3C trace context from an upstream caller.
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	req := httptest.NewRequest(http.MethodGet, "/items/7", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !ctxValid {
		t.Fatal("handler request context carries no valid span")
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("ended spans = %d, want 2 (server + child)", len(spans))
	}
	child, server := spans[0], spans[1]
	if server.Name() != "GET /items/{id}" {
		t.Errorf("server span name = %q, want route-based name", server.Name())
	}
	if server.SpanKind() != trace.SpanKindServer {
		t.Errorf("server span kind = %v, want server", server.SpanKind())
	}
	if got := server.SpanContext().TraceID().String(); got != traceID {
		t.Errorf("server trace ID = %s, want propagated %s", got, traceID)
	}
	if child.Parent().SpanID() != server.SpanContext().SpanID() {
		t.Error("handler span is not parented under the server span")
	}
	var route string
	for _, kv := range server.Attributes() {
		if kv.Key == "http.route" {
			route = kv.Value.AsString()
		}
	}
	if route != "/items/{id}" {
		t.Errorf("http.route = %q, want /items/{id}", route)
	}
}

func TestTracingNilPropagatorUsesGlobal(t *testing.T) {
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	h := middleware.Tracing(tp, nil)(okHandler())
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := spans[0].SpanContext().TraceID().String(); got != traceID {
		t.Errorf("trace ID = %s, want %s extracted by the global propagator", got, traceID)
	}
}

func TestLoggingCorrelatesWithServerSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	var logs bytes.Buffer
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(&logs, nil)))
	var spanCtx trace.SpanContext
	h := middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanCtx = trace.SpanContextFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}), middleware.Tracing(tp, propagation.TraceContext{}), middleware.Logging(log))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items", nil))

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode access log %q: %v", logs.String(), err)
	}
	if !spanCtx.IsValid() {
		t.Fatal("handler saw no valid span")
	}
	if entry["trace_id"] != spanCtx.TraceID().String() || entry["span_id"] != spanCtx.SpanID().String() {
		t.Errorf("access log trace_id/span_id = %v/%v, want %s/%s",
			entry["trace_id"], entry["span_id"], spanCtx.TraceID(), spanCtx.SpanID())
	}
}

func TestTracingSkipsExcludedPaths(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	h := middleware.Tracing(tp, propagation.TraceContext{}, "/livez")(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/livez", nil))

	if n := len(recorder.Ended()); n != 0 {
		t.Errorf("ended spans = %d, want 0 for an excluded path", n)
	}
}

func TestMetricsNormalizesRoutesAndMethods(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mux := http.NewServeMux()
	// Patterns without a method, and with a host, still label by path only.
	mux.Handle("/metrics", okHandler())
	mux.Handle("GET example.com/hosted", okHandler())

	metrics, err := middleware.Metrics(mp)
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	h := metrics(middleware.RouteTagger(mux))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.com/hosted", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("BREW", "/metrics", nil))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[point]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == "http.server.requests" {
				for _, dp := range sum.DataPoints {
					got[pointOf(t, dp.Attributes)] += dp.Value
				}
			}
		}
	}
	want := map[point]int64{
		{method: "GET", route: "/metrics", status: 200}:    1,
		{method: "GET", route: "/hosted", status: 200}:     1,
		{method: "_OTHER", route: "/metrics", status: 200}: 1,
	}
	if len(got) != len(want) {
		t.Errorf("series = %v, want %v", got, want)
	}
	for p, n := range want {
		if got[p] != n {
			t.Errorf("requests%+v = %d, want %d", p, got[p], n)
		}
	}
}
