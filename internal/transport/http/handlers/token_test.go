package handlers_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/service"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/handlers"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

func newTestJWTService(t *testing.T) *service.JWTService {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	svc, err := service.NewJWTService(service.JWTServiceConfig{
		PrivateKey: key,
		PublicKey:  &key.PublicKey,
		Issuer:     "go-api-template",
		TTL:        time.Hour,
		Log:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}
	return svc
}

func TestTokenHandler_CreateToken(t *testing.T) {
	jwtService := newTestJWTService(t)
	obs, _ := newTestObservability(t)

	cfg := handlers.TokenHandlerConfig{
		Log:           discardLogger(),
		Observability: obs,
		JWTService:    jwtService,
		BasePath:      testBasePath,
	}
	h, err := cfg.NewTokenHandler()
	if err != nil {
		t.Fatalf("NewTokenHandler: %v", err)
	}
	router := newTestRouter()
	h.RegisterRoutes(router)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{name: "valid subject", body: `{"subject":"user-123"}`, wantStatus: http.StatusOK},
		{name: "empty subject", body: `{"subject":""}`, wantStatus: http.StatusBadRequest, wantError: "subject is required"},
		{name: "malformed body", body: `{`, wantStatus: http.StatusBadRequest, wantError: "invalid request body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, testBasePath+"/token", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantError != "" {
				var got model.ErrorResponse
				if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if got.Error != tt.wantError {
					t.Errorf("error = %q, want %q", got.Error, tt.wantError)
				}
				return
			}

			var got model.TokenResponse
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode token body: %v", err)
			}
			claims, err := jwtService.VerifyToken(got.Token)
			if err != nil {
				t.Fatalf("issued token does not verify: %v", err)
			}
			if sub, _ := claims.GetSubject(); sub != "user-123" {
				t.Errorf("subject = %q, want %q", sub, "user-123")
			}
		})
	}
}

func TestTokenHandler_RejectsOtherMethods(t *testing.T) {
	obs, _ := newTestObservability(t)
	cfg := handlers.TokenHandlerConfig{
		Log:           discardLogger(),
		Observability: obs,
		JWTService:    newTestJWTService(t),
		BasePath:      testBasePath,
	}
	h, err := cfg.NewTokenHandler()
	if err != nil {
		t.Fatalf("NewTokenHandler: %v", err)
	}
	router := newTestRouter()
	h.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, testBasePath+"/token", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestTokenHandler_RejectsOversizedBody(t *testing.T) {
	obs, _ := newTestObservability(t)
	cfg := handlers.TokenHandlerConfig{
		Log:           discardLogger(),
		Observability: obs,
		JWTService:    newTestJWTService(t),
		BasePath:      testBasePath,
	}
	h, err := cfg.NewTokenHandler()
	if err != nil {
		t.Fatalf("NewTokenHandler: %v", err)
	}
	router := newTestRouter()
	h.RegisterRoutes(router)
	limited := middleware.BodyLimit(16)(router)

	body := `{"subject":"` + strings.Repeat("a", 64) + `"}`
	rec := httptest.NewRecorder()
	limited.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, testBasePath+"/token", strings.NewReader(body)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
