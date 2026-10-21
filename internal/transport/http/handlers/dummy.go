package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
	"go.opentelemetry.io/otel/trace"
)

// DummyBusinessLogicHandlerConfig holds dependencies for the dummy business logic handler
type DummyBusinessLogicHandlerConfig struct {
	Log           *slog.Logger
	Observability *observability.ObservabilityInst
	Cfg           *config.Config
	BasePath      string
}

// DummyBusinessLogicHandler handles dummy business logic requests
type DummyBusinessLogicHandler struct {
	log      *slog.Logger
	cfg      *config.Config
	tracer   trace.Tracer
	basePath string
}

// NewDummyBusinessLogicHandler creates a new dummy business logic handler
func (d *DummyBusinessLogicHandlerConfig) NewDummyBusinessLogicHandler() *DummyBusinessLogicHandler {
	return &DummyBusinessLogicHandler{
		log:      d.Log,
		cfg:      d.Cfg,
		tracer:   d.Observability.Trace.Tracer("github.com/martinezsaweczko/go-api-template/internal/transport/http/handlers"),
		basePath: d.BasePath,
	}
}

// RegisterRoutes registers the dummy business logic handler route
func (d *DummyBusinessLogicHandler) RegisterRoutes(router Router, middlewares ...middleware.Middleware) {
	path := fmt.Sprintf("%s/dummyBusinessLogic", d.basePath)
	if len(middlewares) > 0 {
		router.HandleFuncWithMiddleware("GET "+path, d.dummy, middlewares...)
	} else {
		router.HandleFunc("GET "+path, d.dummy)
	}
}

// Dummy business logic endpoint
//
//	@Summary      Dummy business logic endpoint
//	@Description  Dummy business logic endpoint for testing purposes
//	@Tags         dummy
//	@Accept       json
//	@Produce      json
//	@Success      200  {object}   model.OKResponse
//	@Failure      500  {object}  model.HTTPError
//	@Router       /dummyBusinessLogic [get]
//	@Security     Bearer
func (d *DummyBusinessLogicHandler) dummy(w http.ResponseWriter, r *http.Request) {
	// The HTTP middleware already traces the request. Start child spans only
	// for meaningful units of work, and pass the returned ctx downstream so
	// nested spans and logs stay in the same trace.
	ctx, span := d.tracer.Start(r.Context(), "dummyBusinessLogic")
	defer span.End()

	d.log.InfoContext(ctx, "Received request to dummy business logic endpoint")

	response := model.OKResponse{
		Status: "ok",
	}

	if err := writeJSON(w, http.StatusOK, response); err != nil {
		span.RecordError(err)
		d.log.ErrorContext(ctx, "Failed to send response", "error", err)
	}
}
