package handlers

import (
	"log/slog"
	"net/http"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// Handler is an alias for http.Handler
type Handler = http.Handler

// MetricsHandlerConfig holds dependencies for the metrics handler.
type MetricsHandlerConfig struct {
	Log *slog.Logger
	Cfg *config.Config
}

// MetricsHandler registers the Prometheus scrape endpoint.
type MetricsHandler struct {
	log *slog.Logger
	cfg *config.Config
}

// NewMetricsHandler creates a new metrics handler
func (m *MetricsHandlerConfig) NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{
		log: m.Log,
		cfg: m.Cfg,
	}
}

// RegisterRoutes serves handler on the configured metrics path. Nothing is
// registered when the metrics path is empty (metrics disabled).
func (m *MetricsHandler) RegisterRoutes(router Router, handler http.Handler, middlewares ...middleware.Middleware) {
	path := m.cfg.Observability.MetricsPath
	if path == "" {
		return
	}
	if len(middlewares) > 0 {
		router.HandleWithMiddleware(path, handler, middlewares...)
	} else {
		router.Handle(path, handler)
	}
}
