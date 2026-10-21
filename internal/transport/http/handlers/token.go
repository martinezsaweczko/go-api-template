package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/service"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// TokenHandlerConfig holds dependencies for the token handler
type TokenHandlerConfig struct {
	Log        *slog.Logger
	Cfg        *config.Config
	JWTService *service.JWTService
	BasePath   string
}

// TokenHandler handles JWT token generation requests
type TokenHandler struct {
	log        *slog.Logger
	cfg        *config.Config
	jwtService *service.JWTService
	basePath   string
}

// NewTokenHandler creates a new token handler
func (c *TokenHandlerConfig) NewTokenHandler() *TokenHandler {
	return &TokenHandler{
		log:        c.Log,
		cfg:        c.Cfg,
		jwtService: c.JWTService,
		basePath:   c.BasePath,
	}
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
	ctx := r.Context()

	// Only allow POST requests
	if r.Method != http.MethodPost {
		t.log.WarnContext(ctx, "Invalid HTTP method for token endpoint", "method", r.Method)
		t.respond(ctx, w, http.StatusMethodNotAllowed, model.ErrorResponse{Error: "method not allowed"})
		return
	}

	// Parse the request body
	var req model.TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.log.WarnContext(ctx, "Failed to decode token request", "error", err.Error())
		status, message := http.StatusBadRequest, "invalid request body"
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status, message = http.StatusRequestEntityTooLarge, "request body too large"
		}
		t.respond(ctx, w, status, model.ErrorResponse{Error: message})
		return
	}

	// Validate the request
	if req.Subject == "" {
		t.log.WarnContext(ctx, "Subject is empty in token request")
		t.respond(ctx, w, http.StatusBadRequest, model.ErrorResponse{Error: "subject is required"})
		return
	}

	// Generate the token
	token, err := t.jwtService.GenerateToken(req.Subject)
	if err != nil {
		t.log.ErrorContext(ctx, "Failed to generate token", "error", err.Error(), "subject", req.Subject)
		t.respond(ctx, w, http.StatusInternalServerError, model.ErrorResponse{Error: "failed to generate token"})
		return
	}

	t.log.InfoContext(ctx, "Token generated successfully", "subject", req.Subject)
	t.respond(ctx, w, http.StatusOK, model.TokenResponse{Token: token})
}

// respond writes v as JSON and logs a failure to send it.
func (t *TokenHandler) respond(ctx context.Context, w http.ResponseWriter, status int, v any) {
	if err := writeJSON(w, status, v); err != nil {
		t.log.ErrorContext(ctx, "Failed to send token response", "error", err)
	}
}
