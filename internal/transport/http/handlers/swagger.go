package handlers

import (
	"log/slog"
	"net/http"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// SwaggerPath is the route prefix serving the Swagger UI and OpenAPI document.
const SwaggerPath = "/swagger/"

// SwaggerHandlerConfig holds dependencies for the swagger handler.
type SwaggerHandlerConfig struct {
	Log *slog.Logger
	Cfg *config.Config
}

// SwaggerHandler registers the Swagger UI route.
type SwaggerHandler struct {
	log *slog.Logger
	cfg *config.Config
}

// NewSwaggerHandler creates a new swagger handler
func (s *SwaggerHandlerConfig) NewSwaggerHandler() *SwaggerHandler {
	return &SwaggerHandler{
		log: s.Log,
		cfg: s.Cfg,
	}
}

// RegisterRoutes serves handler for every path under SwaggerPath.
func (s *SwaggerHandler) RegisterRoutes(router Router, handler http.Handler, middlewares ...middleware.Middleware) {
	if len(middlewares) > 0 {
		router.HandleWithMiddleware("GET "+SwaggerPath, handler, middlewares...)
	} else {
		router.Handle("GET "+SwaggerPath, handler)
	}
}
