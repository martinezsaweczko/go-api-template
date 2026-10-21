package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/handlers"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

const testBasePath = "/api/v1"

// testRouter adapts http.ServeMux to the handlers.Router interface.
type testRouter struct {
	*http.ServeMux
}

func newTestRouter() *testRouter {
	return &testRouter{ServeMux: http.NewServeMux()}
}

func (r *testRouter) HandleWithMiddleware(pattern string, h http.Handler, mws ...middleware.Middleware) {
	r.Handle(pattern, middleware.Chain(h, mws...))
}

func (r *testRouter) HandleFuncWithMiddleware(pattern string, h func(http.ResponseWriter, *http.Request), mws ...middleware.Middleware) {
	r.Handle(pattern, middleware.Chain(http.HandlerFunc(h), mws...))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestObservability returns an observability instance backed by an
// in-memory metric reader so tests can assert recorded metrics.
func newTestObservability(t *testing.T) (*observability.ObservabilityInst, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	})
	return &observability.ObservabilityInst{Trace: tp, MeterProvider: mp}, reader
}

// counterTotal returns the sum of all data points of the named Int64 counter.
func counterTotal(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q has unexpected type %T", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

// failingWriter is a ResponseWriter whose body writes always fail.
type failingWriter struct {
	header      http.Header
	statusCodes []int
}

func (w *failingWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *failingWriter) WriteHeader(code int) { w.statusCodes = append(w.statusCodes, code) }

func (w *failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func newVersionHandler(t *testing.T, obs *observability.ObservabilityInst) *handlers.VersionHandler {
	t.Helper()
	cfg := handlers.VersionHandlerConfig{
		Log:           discardLogger(),
		Observability: obs,
		AppBuildInfo:  model.BuildInfo{Version: "v1.2.3", BuildTime: "2026-01-01T00:00:00Z", AppName: "test-app"},
		BasePath:      testBasePath,
	}
	h, err := cfg.NewVersionHandler()
	if err != nil {
		t.Fatalf("NewVersionHandler: %v", err)
	}
	return h
}

func TestVersionHandler_Success(t *testing.T) {
	obs, reader := newTestObservability(t)
	router := newTestRouter()
	newVersionHandler(t, obs).RegisterRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, testBasePath+"/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got model.VersionInfo
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Version != "v1.2.3" || got.AppName != "test-app" {
		t.Errorf("body = %+v, want version v1.2.3 and app test-app", got)
	}
	if total := counterTotal(t, reader, "version_handler_calls"); total != 1 {
		t.Errorf("version_handler_calls = %d, want 1", total)
	}
}

func TestVersionHandler_EncodeErrorCountsOnce(t *testing.T) {
	obs, reader := newTestObservability(t)
	router := newTestRouter()
	newVersionHandler(t, obs).RegisterRoutes(router)

	w := &failingWriter{}
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, testBasePath+"/version", nil))

	if total := counterTotal(t, reader, "version_handler_calls"); total != 1 {
		t.Errorf("version_handler_calls = %d, want 1 (error path must not also record success)", total)
	}
}
