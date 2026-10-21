package middleware_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/martinezsaweczko/go-api-template/internal/service"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRecoveryTurnsPanicInto500JSON(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := middleware.Recovery(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "internal server error" {
		t.Errorf("body = %v, want internal server error", body)
	}
	for _, want := range []string{"boom", `"stack"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log output %q does not contain %q", logs.String(), want)
		}
	}
}

func TestRecoveryAfterResponseStartedAbortsWithoutSecondBody(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := middleware.Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "partial")
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	func() {
		defer func() {
			// The response cannot be fixed once started, so the connection is
			// aborted instead of appending an error body.
			if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
				t.Errorf("recovered %v, want http.ErrAbortHandler", err)
			}
		}()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want original 202", rec.Code)
	}
	if body := rec.Body.String(); body != "partial" {
		t.Errorf("body = %q, want only the handler output", body)
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Errorf("panic was not logged: %q", logs.String())
	}
}

func TestRecoveryRepanicsAbortHandler(t *testing.T) {
	h := middleware.Recovery(discardLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("recovered %v, want http.ErrAbortHandler to propagate", err)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRequestID(t *testing.T) {
	tests := []struct {
		name       string
		incoming   string
		wantReused bool
	}{
		{name: "missing header generates one", incoming: ""},
		{name: "sane header is reused", incoming: "req-123_abc.DEF:1", wantReused: true},
		{name: "header with spaces is replaced", incoming: "bad id"},
		{name: "overlong header is replaced", incoming: strings.Repeat("a", 129)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fromCtx string
			h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				fromCtx = middleware.RequestIDFromContext(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.incoming != "" {
				req.Header.Set(middleware.RequestIDHeader, tt.incoming)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			got := rec.Header().Get(middleware.RequestIDHeader)
			if got == "" {
				t.Fatal("response has no request ID header")
			}
			if got != fromCtx {
				t.Errorf("header %q != context %q", got, fromCtx)
			}
			if tt.wantReused && got != tt.incoming {
				t.Errorf("request ID = %q, want incoming %q", got, tt.incoming)
			}
			if !tt.wantReused && got == tt.incoming {
				t.Errorf("request ID %q was reused, want a generated one", got)
			}
		})
	}
}

func TestLoggingIncludesRequestIDAndQuietsProbes(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := middleware.Chain(okHandler(), middleware.RequestID, middleware.Logging(log, "/livez"))

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set(middleware.RequestIDHeader, "trace-me")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(logs.String(), `"request_id":"trace-me"`) {
		t.Errorf("log %q does not include the request ID", logs.String())
	}

	logs.Reset()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/livez", nil))
	if logs.Len() != 0 {
		t.Errorf("probe request logged at info: %q", logs.String())
	}
}

func TestLoggingWriterSupportsResponseController(t *testing.T) {
	var flushErr error
	h := middleware.Logging(discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "chunk")
		flushErr = http.NewResponseController(w).Flush()
		if _, ok := w.(http.Flusher); !ok {
			t.Error("wrapped writer does not implement http.Flusher")
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if flushErr != nil {
		t.Fatalf("ResponseController.Flush() = %v", flushErr)
	}
	if !rec.Flushed {
		t.Error("underlying recorder was not flushed")
	}
}

func TestCORS(t *testing.T) {
	const allowed = "https://app.example"
	tests := []struct {
		name        string
		origins     []string
		method      string
		origin      string
		preflight   bool
		wantStatus  int
		wantAllowed string
		wantNext    bool
	}{
		{name: "disabled passes through", method: http.MethodGet, origin: allowed, wantStatus: http.StatusOK, wantNext: true},
		{name: "disabled does not answer preflight", method: http.MethodOptions, origin: allowed, preflight: true, wantStatus: http.StatusOK, wantNext: true},
		{name: "allowed origin", origins: []string{allowed}, method: http.MethodGet, origin: allowed, wantStatus: http.StatusOK, wantAllowed: allowed, wantNext: true},
		{name: "disallowed origin", origins: []string{allowed}, method: http.MethodGet, origin: "https://evil.example", wantStatus: http.StatusOK, wantNext: true},
		{name: "wildcard", origins: []string{"*"}, method: http.MethodGet, origin: "https://any.example", wantStatus: http.StatusOK, wantAllowed: "*", wantNext: true},
		{name: "allowed preflight", origins: []string{allowed}, method: http.MethodOptions, origin: allowed, preflight: true, wantStatus: http.StatusNoContent, wantAllowed: allowed},
		{name: "disallowed preflight", origins: []string{allowed}, method: http.MethodOptions, origin: "https://evil.example", preflight: true, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			h := middleware.CORS(tt.origins)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(tt.method, "/", nil)
			req.Header.Set("Origin", tt.origin)
			if tt.preflight {
				req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantAllowed {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantAllowed)
			}
			if nextCalled != tt.wantNext {
				t.Errorf("next called = %v, want %v", nextCalled, tt.wantNext)
			}
			if tt.preflight && tt.wantAllowed != "" && rec.Header().Get("Access-Control-Allow-Methods") == "" {
				t.Error("preflight response has no Access-Control-Allow-Methods")
			}
		})
	}
}

func TestBodyLimit(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "within limit", body: "1234"},
		{name: "over limit", body: "12345", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var readErr error
			h := middleware.BodyLimit(4)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, readErr = io.ReadAll(r.Body)
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body)))

			var maxErr *http.MaxBytesError
			if got := errors.As(readErr, &maxErr); got != tt.wantErr {
				t.Errorf("MaxBytesError = %v (err %v), want %v", got, readErr, tt.wantErr)
			}
		})
	}
}

func newJWTService(t *testing.T) *service.JWTService {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	svc, err := service.NewJWTService(service.JWTServiceConfig{
		PrivateKey: key,
		PublicKey:  &key.PublicKey,
		Issuer:     "test",
		TTL:        time.Minute,
		Log:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}
	return svc
}

func TestAuthorization(t *testing.T) {
	svc := newJWTService(t)
	token, err := svc.GenerateToken("user-1")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	tests := []struct {
		name        string
		header      string
		wantStatus  int
		wantSubject string
	}{
		{name: "missing header", wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic abc", wantStatus: http.StatusUnauthorized},
		{name: "invalid token", header: "Bearer not-a-jwt", wantStatus: http.StatusUnauthorized},
		{name: "valid token", header: "Bearer " + token, wantStatus: http.StatusOK, wantSubject: "user-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var subject string
			h := middleware.AuthorizationMiddleware(discardLogger(), svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				claims, ok := middleware.ClaimsFromContext(r.Context())
				if !ok {
					t.Error("claims missing from request context")
				} else {
					subject, _ = claims.GetSubject()
				}
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
				var body map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body["error"] == "" {
					t.Errorf("error body = %v (decode err %v), want JSON with error", body, err)
				}
				return
			}
			if subject != tt.wantSubject {
				t.Errorf("subject = %q, want %q", subject, tt.wantSubject)
			}
		})
	}
}

func TestClaimsFromContextEmpty(t *testing.T) {
	if _, ok := middleware.ClaimsFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Error("ClaimsFromContext reported claims on a bare context")
	}
}
