package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// HealthHandlerConfig holds dependencies for the health handler
type HealthHandlerConfig struct {
	Log      *slog.Logger
	Cfg      *config.Config
	BasePath string
	// Ready reports whether the application is ready to receive traffic.
	// Nil means always ready.
	Ready func() bool
}

// HealthHandler handles health check requests
type HealthHandler struct {
	log      *slog.Logger
	cfg      *config.Config
	basePath string
	ready    func() bool
}

// NewHealthHandler creates a new health handler
func (h *HealthHandlerConfig) NewHealthHandler() *HealthHandler {
	ready := h.Ready
	if ready == nil {
		ready = func() bool { return true }
	}

	return &HealthHandler{
		log:      h.Log,
		cfg:      h.Cfg,
		basePath: h.BasePath,
		ready:    ready,
	}
}

// Probe paths served outside the API base path.
const (
	LivenessPath  = "/livez"
	ReadinessPath = "/readyz"
)

// RegisterRoutes registers the liveness and readiness probes and the legacy
// health route under the base path.
func (h *HealthHandler) RegisterRoutes(router Router, middlewares ...middleware.Middleware) {
	routes := map[string]func(http.ResponseWriter, *http.Request){
		"GET " + LivenessPath:                    h.livez,
		"GET " + ReadinessPath:                   h.readyz,
		fmt.Sprintf("GET %s/health", h.basePath): h.getHealth,
	}
	for pattern, fn := range routes {
		if len(middlewares) > 0 {
			router.HandleFuncWithMiddleware(pattern, fn, middlewares...)
		} else {
			router.HandleFunc(pattern, fn)
		}
	}
}

// livez reports that the process is serving requests.
func (h *HealthHandler) livez(w http.ResponseWriter, _ *http.Request) {
	writeStatus(w, http.StatusOK, "ok")
}

// readyz reports whether the application is ready to receive traffic.
func (h *HealthHandler) readyz(w http.ResponseWriter, _ *http.Request) {
	if !h.ready() {
		writeStatus(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	writeStatus(w, http.StatusOK, "ready")
}

func writeStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(model.OKResponse{Status: status})
}

// Get health status
//
//	@Summary      Get health status
//	@Description  Get health status of the application
//	@Tags         health
//	@Accept       json
//	@Produce      json
//	@Success      200  {object}   model.HealthInfo
//	@Failure      500  {object}  model.HTTPError
//	@Router       /health [get]
func (h *HealthHandler) getHealth(w http.ResponseWriter, r *http.Request) {
	h.log.DebugContext(r.Context(), "Received request for health status")

	healthInfo := &model.HealthInfo{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(healthInfo); err != nil {
		h.log.ErrorContext(r.Context(), "Failed to encode health information", "error", err)
	}
}
