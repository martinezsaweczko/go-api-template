package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/martinezsaweczko/go-api-template/internal/transport/http/handlers"
)

func TestHealthProbes(t *testing.T) {
	var ready atomic.Bool
	cfg := handlers.HealthHandlerConfig{
		Log:      discardLogger(),
		BasePath: testBasePath,
		Ready:    ready.Load,
	}
	h := cfg.NewHealthHandler()
	router := newTestRouter()
	h.RegisterRoutes(router)

	tests := []struct {
		name       string
		path       string
		ready      bool
		wantStatus int
	}{
		{name: "liveness while starting", path: "/livez", wantStatus: http.StatusOK},
		{name: "liveness when ready", path: "/livez", ready: true, wantStatus: http.StatusOK},
		{name: "readiness while starting", path: "/readyz", wantStatus: http.StatusServiceUnavailable},
		{name: "readiness when ready", path: "/readyz", ready: true, wantStatus: http.StatusOK},
		{name: "legacy health endpoint", path: testBasePath + "/health", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready.Store(tt.ready)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}
