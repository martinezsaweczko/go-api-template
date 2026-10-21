package handlers_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/martinezsaweczko/go-api-template/internal/model"
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

func newVersionHandler() *handlers.VersionHandler {
	cfg := handlers.VersionHandlerConfig{
		Log:          discardLogger(),
		AppBuildInfo: model.BuildInfo{Version: "v1.2.3", BuildTime: "2026-01-01T00:00:00Z", AppName: "test-app"},
		BasePath:     testBasePath,
	}
	return cfg.NewVersionHandler()
}

func TestVersionHandler_Success(t *testing.T) {
	router := newTestRouter()
	newVersionHandler().RegisterRoutes(router)

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
}

func TestVersionHandler_EncodeErrorReturns500(t *testing.T) {
	router := newTestRouter()
	newVersionHandler().RegisterRoutes(router)

	w := &failingWriter{}
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, testBasePath+"/version", nil))

	if !slices.Equal(w.statusCodes, []int{http.StatusInternalServerError}) {
		t.Errorf("status codes written = %v, want [500]", w.statusCodes)
	}
}
