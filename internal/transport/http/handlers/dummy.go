package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
	log           *slog.Logger
	observability *observability.ObservabilityInst
	cfg           *config.Config
	handlerCalls  metric.Int64Counter
	basePath      string
}

// NewDummyBusinessLogicHandler creates a new dummy business logic handler
func (d *DummyBusinessLogicHandlerConfig) NewDummyBusinessLogicHandler() (*DummyBusinessLogicHandler, error) {
	handlerCalls, err := d.Observability.MeterProvider.Meter("dummy_business_logic_handler").Int64Counter("dummy_business_logic_handler_calls", metric.WithDescription("Number of calls to the dummy business logic handler"))
	if err != nil {
		d.Log.Error("Failed to create dummy business logic handler counter", "error", err)
		return nil, err
	}

	return &DummyBusinessLogicHandler{
		log:           d.Log,
		observability: d.Observability,
		cfg:           d.Cfg,
		handlerCalls:  handlerCalls,
		basePath:      d.BasePath,
	}, nil
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
	d.log.Info("Received request to dummy business logic endpoint")

	_, span := d.observability.Trace.Tracer("dummy_business_logic_handler").Start(r.Context(), "dummyBusinessLogicHandler")
	defer span.End()

	response := model.OKResponse{
		Status: "ok",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		d.log.Error("Failed to encode response", "error", err)
		d.handlerCalls.Add(r.Context(), 1,
			metric.WithAttributes(attribute.String("method", "GET")),
			metric.WithAttributes(attribute.Int("http_code", http.StatusInternalServerError)),
			metric.WithAttributes(attribute.String("path", fmt.Sprintf("%s/dummyBusinessLogic", d.basePath))),
		)
		return
	}

	d.handlerCalls.Add(r.Context(), 1,
		metric.WithAttributes(attribute.String("method", "GET")),
		metric.WithAttributes(attribute.Int("http_code", http.StatusOK)),
		metric.WithAttributes(attribute.String("path", fmt.Sprintf("%s/dummyBusinessLogic", d.basePath))),
	)
}
