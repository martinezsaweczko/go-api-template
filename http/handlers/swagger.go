package handlers

import (
	"log/slog"
	"net/http"

	"github.com/martinezsaweczko/go-api-template/config"
	"github.com/martinezsaweczko/go-api-template/http/middleware"
	"github.com/martinezsaweczko/go-api-template/o11"
)

// Router defines the interface for registering routes
type SwaggerHandlerConfig struct {
	Log           *slog.Logger
	Observability *o11.ObservabilityInst
	Cfg           *config.Config
}

type SwaggerHandler struct {
	log           *slog.Logger
	observability *o11.ObservabilityInst
	cfg           *config.Config
}

// NewSwaggerHandler creates a new swagger handler
func (s *SwaggerHandlerConfig) NewSwaggerHandler() *SwaggerHandler {
	// This is a wrapper to simplify imports in the app package
	swaggerHandler := &SwaggerHandler{
		log:           s.Log,
		observability: s.Observability,
		cfg:           s.Cfg,
	}
	return swaggerHandler
}

// Register the routes for the swagger handler
func (s *SwaggerHandler) RegisterRoutes(router Router, handler http.Handler, middlewares ...middleware.Middleware) {
	// Register the swagger handler route with wildcard to serve swagger files
	if len(middlewares) > 0 {
		router.HandleWithMiddleware("GET /swagger/", handler, middlewares...)
	} else {
		router.Handle("GET /swagger/", handler)
	}
}
