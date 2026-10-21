package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/martinezsaweczko/go-api-template/config"
	"github.com/martinezsaweczko/go-api-template/http/handlers"
	"github.com/martinezsaweczko/go-api-template/http/middleware"
	"github.com/martinezsaweczko/go-api-template/model"
	"github.com/martinezsaweczko/go-api-template/o11"
	"github.com/martinezsaweczko/go-api-template/services"
)

// Server defines the interface for any HTTP server implementation
// This interface is defined in the consumer (app) package
type Server interface {
	// Start binds the listener synchronously, returning any listen error, and serves in the background.
	Start() error
	// Errors reports a fatal error from the background serve loop.
	Errors() <-chan error
	Stop(ctx context.Context) error
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
	HandleWithMiddleware(pattern string, handler http.Handler, middlewares ...middleware.Middleware)
	HandleFuncWithMiddleware(pattern string, handler func(http.ResponseWriter, *http.Request), middlewares ...middleware.Middleware)
}

type Handlers struct {
	metricsHandler            *handlers.MetricsHandler
	versionHandler            *handlers.VersionHandler
	swaggerHandler            *handlers.SwaggerHandler
	tokenHandler              *handlers.TokenHandler
	healthHandler             *handlers.HealthHandler
	dummyBusinessLogicHandler *handlers.DummyBusinessLogicHandler
}

type App struct {
	ctx           context.Context
	config        *config.Config
	server        Server
	log           *slog.Logger
	observability *o11.ObservabilityInst
	handlers      *Handlers
	jwtService    *services.JWTService
	buildInfo     model.BuildInfo
	basePath      string
}

func NewApp(cfg *config.Config) *App {
	return &App{
		config: cfg,
		ctx:    context.Background(),
	}
}

func (app *App) WithServer(server Server) *App {
	app.server = server
	return app
}

func (app *App) WithLog(log *slog.Logger) *App {
	app.log = log
	return app
}

// WithObservability allows setting up observability providers (tracing, metrics) and returns the app instance for chaining
func (app *App) WithObservability(observability *o11.ObservabilityInst) *App {
	// Set up OpenTelemetry providers here if needed
	app.observability = observability
	return app
}

// WithBuildInfo sets the build information and returns the app instance for chaining
func (app *App) WithBuildInfo(buildInfo model.BuildInfo) *App {
	app.buildInfo = buildInfo
	return app
}

// WithBasePath sets the base path for all routes and returns the app instance for chaining
func (app *App) WithBasePath(basePath string) *App {
	app.basePath = basePath
	return app
}

func (app *App) Run() error {
	// Validate required dependencies
	if app.server == nil {
		return fmt.Errorf("server dependency is required: use WithServer() to inject it")
	}
	if app.log == nil {
		return fmt.Errorf("logger dependency is required: use WithLog() to inject it")
	}

	// Initialize handlers and register routes before starting the server
	if err := app.initHandlers(); err != nil {
		return fmt.Errorf("failed to initialize handlers: %w", err)
	}
	app.registerRoutes()

	// Setup signal handling for graceful shutdown before starting to serve
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	if err := app.server.Start(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	// Wait for a termination signal or a fatal server error
	select {
	case sig := <-sigChan:
		app.log.Info("Received shutdown signal", "signal", sig)
	case err := <-app.server.Errors():
		return fmt.Errorf("server error: %w", err)
	}

	// Perform graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(app.ctx, 30*time.Second)
	defer cancel()

	app.log.Info("Starting graceful server shutdown")
	if err := app.server.Stop(shutdownCtx); err != nil {
		app.log.Error("Error during server shutdown", "error", err)
		return err
	}

	app.log.Info("Server shut down gracefully")
	return nil
}
