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

// failingWriter is a ResponseWriter whose body writes always fail. Like
// net/http, the first Write sends an implicit 200 when no status was written,
// and every WriteHeader call is recorded so superfluous ones are visible.
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

func (w *failingWriter) Write([]byte) (int, error) {
	if len(w.statusCodes) == 0 {
		w.statusCodes = append(w.statusCodes, http.StatusOK)
	}
	return 0, errors.New("write failed")
}

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
	handlers.SetMarshalJSON(t, func(any) ([]byte, error) { return nil, errors.New("marshal failed") })
	router := newTestRouter()
	newVersionHandler().RegisterRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, testBasePath+"/version", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body model.HTTPError
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Message == "" {
		t.Errorf("body = %+v (decode err %v), want a JSON error message", body, err)
	}
}

func TestVersionHandler_WriteErrorSendsNoSecondStatus(t *testing.T) {
	router := newTestRouter()
	newVersionHandler().RegisterRoutes(router)

	w := &failingWriter{}
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, testBasePath+"/version", nil))

	// Once the body write started the status is on the wire; a later
	// WriteHeader(500) would be superfluous and never reach the client.
	if !slices.Equal(w.statusCodes, []int{http.StatusOK}) {
		t.Errorf("status codes written = %v, want [200]", w.statusCodes)
	}
}
