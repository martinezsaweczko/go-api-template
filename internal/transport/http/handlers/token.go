package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/service"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// TokenHandlerConfig holds dependencies for the token handler
type TokenHandlerConfig struct {
	Log           *slog.Logger
	Observability *observability.ObservabilityInst
	Cfg           *config.Config
	JWTService    *service.JWTService
	BasePath      string
}

// TokenHandler handles JWT token generation requests
type TokenHandler struct {
	log           *slog.Logger
	observability *observability.ObservabilityInst
	cfg           *config.Config
	jwtService    *service.JWTService
	handlerCalls  metric.Int64Counter
	basePath      string
}

// NewTokenHandler creates a new token handler
func (c *TokenHandlerConfig) NewTokenHandler() (*TokenHandler, error) {
	handlerCalls, err := c.Observability.MeterProvider.Meter("token_handler").Int64Counter("token_handler_calls", metric.WithDescription("Number of calls to the token handler"))
	if err != nil {
		c.Log.Error("Failed to create token handler counter", "error", err)
		return nil, err
	}

	return &TokenHandler{
		log:           c.Log,
		observability: c.Observability,
		cfg:           c.Cfg,
		jwtService:    c.JWTService,
		handlerCalls:  handlerCalls,
		basePath:      c.BasePath,
	}, nil
}

// RegisterRoutes registers the token handler route
func (t *TokenHandler) RegisterRoutes(router Router, middlewares ...middleware.Middleware) {
	path := fmt.Sprintf("%s/token", t.basePath)
	if len(middlewares) > 0 {
		router.HandleFuncWithMiddleware("POST "+path, t.createToken, middlewares...)
	} else {
		router.HandleFunc("POST "+path, t.createToken)
	}
}

// Create JWT token
//
//	@Summary      Create JWT token (development only)
//	@Description  Mint a JWT for the given subject without authentication. Development only: the route is registered only when the -jwt-dev-token-endpoint flag is enabled and must never be enabled in production.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        request body model.TokenRequest true "Token request"
//	@Success      200  {object}  model.TokenResponse
//	@Failure      400  {object}  model.ErrorResponse
//	@Failure      413  {object}  model.ErrorResponse
//	@Failure      500  {object}  model.ErrorResponse
//	@Router       /token [post]
func (t *TokenHandler) createToken(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		t.log.Warn("Invalid HTTP method for token endpoint", "method", r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(model.ErrorResponse{Error: "method not allowed"})
		t.handlerCalls.Add(r.Context(), 1,
			metric.WithAttributes(attribute.String("method", r.Method)),
			metric.WithAttributes(attribute.Int("http_code", http.StatusMethodNotAllowed)),
			metric.WithAttributes(attribute.String("path", fmt.Sprintf("%s/token", t.basePath))),
		)
		return
	}

	_, span := t.observability.Trace.Tracer("token_handler").Start(r.Context(), "createTokenHandler")
	defer span.End()

	// Parse the request body
	var req model.TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.log.Warn("Failed to decode token request", "error", err.Error())
		status, message := http.StatusBadRequest, "invalid request body"
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status, message = http.StatusRequestEntityTooLarge, "request body too large"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(model.ErrorResponse{Error: message})
		t.handlerCalls.Add(r.Context(), 1,
			metric.WithAttributes(attribute.String("method", r.Method)),
			metric.WithAttributes(attribute.Int("http_code", status)),
			metric.WithAttributes(attribute.String("path", fmt.Sprintf("%s/token", t.basePath))),
		)
		return
	}

	// Validate the request
	if req.Subject == "" {
		t.log.Warn("Subject is empty in token request")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(model.ErrorResponse{Error: "subject is required"})
		t.handlerCalls.Add(r.Context(), 1,
			metric.WithAttributes(attribute.String("method", r.Method)),
			metric.WithAttributes(attribute.Int("http_code", http.StatusBadRequest)),
			metric.WithAttributes(attribute.String("path", fmt.Sprintf("%s/token", t.basePath))),
		)
		return
	}

	// Generate the token
	token, err := t.jwtService.GenerateToken(req.Subject)
	if err != nil {
		t.log.Error("Failed to generate token", "error", err.Error(), "subject", req.Subject)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(model.ErrorResponse{Error: "failed to generate token"})
		t.handlerCalls.Add(r.Context(), 1,
			metric.WithAttributes(attribute.String("method", r.Method)),
			metric.WithAttributes(attribute.Int("http_code", http.StatusInternalServerError)),
			metric.WithAttributes(attribute.String("path", fmt.Sprintf("%s/token", t.basePath))),
		)
		return
	}

	// Log successful token generation
	t.log.Info("Token generated successfully", "subject", req.Subject)

	// Return the token
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(model.TokenResponse{Token: token})
	t.handlerCalls.Add(r.Context(), 1,
		metric.WithAttributes(attribute.String("method", r.Method)),
		metric.WithAttributes(attribute.Int("http_code", http.StatusOK)),
		metric.WithAttributes(attribute.String("path", fmt.Sprintf("%s/token", t.basePath))),
	)
}
