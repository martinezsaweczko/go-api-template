package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// VersionHandlerConfig holds dependencies for the version handler.
type VersionHandlerConfig struct {
	Log          *slog.Logger
	Cfg          *config.Config
	AppBuildInfo model.BuildInfo
	BasePath     string
}

// VersionHandler serves the build information.
type VersionHandler struct {
	log          *slog.Logger
	cfg          *config.Config
	appBuildInfo model.BuildInfo
	basePath     string
}

// NewVersionHandler creates a new version handler.
func (v *VersionHandlerConfig) NewVersionHandler() *VersionHandler {
	return &VersionHandler{
		log:          v.Log,
		cfg:          v.Cfg,
		appBuildInfo: v.AppBuildInfo,
		basePath:     v.BasePath,
	}
}

// RegisterRoutes registers the version handler route.
func (v *VersionHandler) RegisterRoutes(router Router, middlewares ...middleware.Middleware) {
	path := fmt.Sprintf("%s/version", v.basePath)
	if len(middlewares) > 0 {
		router.HandleFuncWithMiddleware("GET "+path, v.getVersion, middlewares...)
	} else {
		router.HandleFunc("GET "+path, v.getVersion)
	}
}

// Get version information
//
//	@Summary      Get version information
//	@Description  Get version information
//	@Tags         version
//	@Accept       json
//	@Produce      json
//	@Success      200  {object}   model.VersionInfo
//	@Failure      400  {object}  model.HTTPError
//	@Failure      404  {object}  model.HTTPError
//	@Failure      500  {object}  model.HTTPError
//	@Router       /version [get]
func (v *VersionHandler) getVersion(w http.ResponseWriter, r *http.Request) {
	v.log.InfoContext(r.Context(), "Received request for version information")

	versionInfo := &model.VersionInfo{
		Version:   v.appBuildInfo.Version,
		BuildTime: v.appBuildInfo.BuildTime,
		AppName:   v.appBuildInfo.AppName,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(versionInfo); err != nil {
		v.log.ErrorContext(r.Context(), "Failed to encode version information", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
