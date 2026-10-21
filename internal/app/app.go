package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/service"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/handlers"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// Server defines the interface for any HTTP server implementation
// This interface is defined in the consumer (app) package
type Server interface {
	// Start binds the listener synchronously, returning any listen error, and serves in the background.
	Start() error
	// Errors reports a fatal error from the background serve loop.
	Errors() <-chan error
	Stop(ctx context.Context) error
	// Use appends middlewares to the global chain wrapping every route.
	Use(middlewares ...middleware.Middleware)
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
	config        *config.Config
	server        Server
	log           *slog.Logger
	observability *observability.ObservabilityInst
	handlers      *Handlers
	jwtService    *service.JWTService
	buildInfo     model.BuildInfo
	basePath      string
	ready         atomic.Bool
}

func NewApp(cfg *config.Config) *App {
	return &App{config: cfg}
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
func (app *App) WithObservability(inst *observability.ObservabilityInst) *App {
	app.observability = inst
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

// Ready reports whether the app is serving and ready for traffic. It becomes
// true once the server has started and false as soon as shutdown begins.
func (app *App) Ready() bool {
	return app.ready.Load()
}

// Run registers the routes, starts the server and blocks until ctx is
// cancelled (for example by SIGINT or SIGTERM through signal.NotifyContext)
// or the server fails. On cancellation it clears readiness and shuts the
// server down gracefully within the configured shutdown timeout.
func (app *App) Run(ctx context.Context) error {
	// Validate required dependencies
	if app.server == nil {
		return fmt.Errorf("server dependency is required: use WithServer() to inject it")
	}
	if app.log == nil {
		return fmt.Errorf("logger dependency is required: use WithLog() to inject it")
	}
	if app.observability == nil {
		return fmt.Errorf("observability dependency is required: use WithObservability() to inject it")
	}

	// Initialize handlers and register routes before starting the server
	if err := app.initHandlers(); err != nil {
		return fmt.Errorf("failed to initialize handlers: %w", err)
	}
	if err := app.registerRoutes(); err != nil {
		return err
	}

	if err := app.server.Start(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	app.ready.Store(true)

	// Wait for cancellation or a fatal server error
	select {
	case <-ctx.Done():
		app.log.Info("Shutdown requested", "cause", context.Cause(ctx))
	case err := <-app.server.Errors():
		app.ready.Store(false)
		return fmt.Errorf("server error: %w", err)
	}

	// Stop advertising readiness before draining connections.
	app.ready.Store(false)

	// ctx is already cancelled, so the shutdown deadline needs a fresh context.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), app.config.Server.ShutdownTimeout)
	defer cancel()

	app.log.Info("Starting graceful server shutdown", "timeout", app.config.Server.ShutdownTimeout)
	if err := app.server.Stop(shutdownCtx); err != nil {
		app.log.Error("Error during server shutdown", "error", err)
		return err
	}

	app.log.Info("Server shut down gracefully")
	return nil
}
