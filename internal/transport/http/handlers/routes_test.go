package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/handlers"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// markHeader is a middleware that tags responses, proving it wrapped a route.
func markHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Wrapped", "yes")
		next.ServeHTTP(w, r)
	})
}

// staticHandler answers 200 with a fixed body.
func staticHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
}

func serve(router http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestMetricsHandlerRegistersConfiguredPath(t *testing.T) {
	tests := []struct {
		name        string
		metricsPath string
		middlewares []middleware.Middleware
		wantStatus  int
		wantWrapped bool
	}{
		{name: "metrics disabled", metricsPath: "", wantStatus: http.StatusNotFound},
		{name: "metrics enabled", metricsPath: "/metrics", wantStatus: http.StatusOK},
		{name: "metrics with middleware", metricsPath: "/metrics", middlewares: []middleware.Middleware{markHeader}, wantStatus: http.StatusOK, wantWrapped: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Observability: config.ObservabilityConfig{MetricsPath: tt.metricsPath}}
			h := (&handlers.MetricsHandlerConfig{Log: discardLogger(), Cfg: cfg}).NewMetricsHandler()
			router := newTestRouter()
			h.RegisterRoutes(router, staticHandler("metrics"), tt.middlewares...)

			rec := serve(router, http.MethodGet, "/metrics")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("X-Wrapped") == "yes"; got != tt.wantWrapped {
				t.Errorf("wrapped by middleware = %v, want %v", got, tt.wantWrapped)
			}
		})
	}
}

func TestSwaggerHandlerServesUnderPrefix(t *testing.T) {
	for _, mws := range [][]middleware.Middleware{nil, {markHeader}} {
		h := (&handlers.SwaggerHandlerConfig{Log: discardLogger()}).NewSwaggerHandler()
		router := newTestRouter()
		h.RegisterRoutes(router, staticHandler("swagger"), mws...)

		rec := serve(router, http.MethodGet, handlers.SwaggerPath+"index.html")
		if rec.Code != http.StatusOK || rec.Body.String() != "swagger" {
			t.Errorf("GET %sindex.html = %d %q, want 200 swagger", handlers.SwaggerPath, rec.Code, rec.Body.String())
		}
		if wantWrapped, got := len(mws) > 0, rec.Header().Get("X-Wrapped") == "yes"; got != wantWrapped {
			t.Errorf("wrapped by middleware = %v, want %v", got, wantWrapped)
		}
		if rec := serve(router, http.MethodPost, handlers.SwaggerPath+"index.html"); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST status = %d, want 405", rec.Code)
		}
	}
}

func TestDummyBusinessLogicHandlerTracesWork(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	h := (&handlers.DummyBusinessLogicHandlerConfig{
		Log:           discardLogger(),
		Observability: &observability.ObservabilityInst{Trace: tp},
		BasePath:      testBasePath,
	}).NewDummyBusinessLogicHandler()
	router := newTestRouter()
	h.RegisterRoutes(router, markHeader)

	rec := serve(router, http.MethodGet, testBasePath+"/dummyBusinessLogic")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Wrapped") != "yes" {
		t.Error("route was not wrapped by the given middleware")
	}
	var body model.OKResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Status != "ok" {
		t.Errorf("body = %+v (decode err %v), want status ok", body, err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "dummyBusinessLogic" {
		t.Errorf("ended spans = %v, want one dummyBusinessLogic span", spans)
	}
}

func TestLegacyHealthReturnsStatusAndTimestamp(t *testing.T) {
	h := (&handlers.HealthHandlerConfig{Log: discardLogger(), BasePath: testBasePath}).NewHealthHandler()
	router := newTestRouter()
	h.RegisterRoutes(router, markHeader)

	rec := serve(router, http.MethodGet, testBasePath+"/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body model.HealthInfo
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || body.Timestamp == "" {
		t.Errorf("body = %+v, want status ok and a timestamp", body)
	}
	// A nil Ready func means always ready.
	if rec := serve(router, http.MethodGet, handlers.ReadinessPath); rec.Code != http.StatusOK {
		t.Errorf("readiness status = %d, want 200 with a nil Ready func", rec.Code)
	}
}
