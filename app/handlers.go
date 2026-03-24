package app

import (
	"fmt"
	"time"

	_ "github.com/martinezsaweczko/go-api-template/docs" // This is required to serve swagger docs, it registers the swagger files with the httpSwagger handler
	"github.com/martinezsaweczko/go-api-template/http/handlers"
	"github.com/martinezsaweczko/go-api-template/http/middleware"
	"github.com/martinezsaweczko/go-api-template/services"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"
)

// initHandlers initializes all HTTP handlers and registers them with the server
func (app *App) initHandlers() error {

	var err error
	// Initialize metrics handler
	metricsHandlerConf := handlers.MetricsHandlerConfig{
		Log:           app.log,
		Observability: app.observability,
		Cfg:           app.config,
	}

	app.handlers = &Handlers{}
	app.handlers.metricsHandler = metricsHandlerConf.NewMetricsHandler()

	// Initialize version handler
	versionHandlerConf := handlers.VersionHandlerConfig{
		Log:           app.log,
		Observability: app.observability,
		Cfg:           app.config,
		AppBuildInfo:  app.buildInfo,
		BasePath:      app.basePath,
	}
	app.handlers.versionHandler, err = versionHandlerConf.NewVersionHandler()
	if err != nil {
		app.log.Error("Failed to initialize version handler", "error", err)
		return err
	}

	// Initialize swagger handler
	swaggerHandlerConf := handlers.SwaggerHandlerConfig{
		Log:           app.log,
		Observability: app.observability,
		Cfg:           app.config,
	}
	swaggerHandler := swaggerHandlerConf.NewSwaggerHandler()
	app.handlers.swaggerHandler = swaggerHandler

	// Initialize health handler
	healthHandlerConf := handlers.HealthHandlerConfig{
		Log:           app.log,
		Observability: app.observability,
		Cfg:           app.config,
		BasePath:      app.basePath,
	}
	app.handlers.healthHandler, err = healthHandlerConf.NewHealthHandler()
	if err != nil {
		app.log.Error("Failed to initialize health handler", "error", err)
		return err
	}

	// Initialize dummy business logic handler
	dummyBusinessLogicHandlerConf := handlers.DummyBusinessLogicHandlerConfig{
		Log:           app.log,
		Observability: app.observability,
		Cfg:           app.config,
		BasePath:      app.basePath,
	}
	app.handlers.dummyBusinessLogicHandler, err = dummyBusinessLogicHandlerConf.NewDummyBusinessLogicHandler()
	if err != nil {
		app.log.Error("Failed to initialize dummy business logic handler", "error", err)
		return err
	}

	// Initialize JWT service
	if app.config.JWT.PublicKey == nil || app.config.JWT.PrivateKey == nil {
		app.log.Error("JWT keys not loaded from configuration")
		return fmt.Errorf("JWT keys not available")
	}

	jwtService := services.NewJWTService(
		app.config.JWT.PrivateKey,
		app.config.JWT.PublicKey,
		"go-api-template",
		24*time.Hour, // Token expiry
	)

	// Initialize token handler
	tokenHandlerConf := handlers.TokenHandlerConfig{
		Log:           app.log,
		Observability: app.observability,
		Cfg:           app.config,
		JWTService:    jwtService,
		BasePath:      app.basePath,
	}
	app.handlers.tokenHandler, err = tokenHandlerConf.NewTokenHandler()
	if err != nil {
		app.log.Error("Failed to initialize token handler", "error", err)
		return err
	}

	return nil
}

// registerRoutes registers all HTTP routes to the server's mux with appropriate middleware
func (app *App) registerRoutes() {
	// Define middleware chains for different endpoints
	loggingMiddleware := middleware.LoggingMiddleware(app.log)

	// Create JWT service from config (same as in initHandlers)
	jwtService := services.NewJWTService(
		app.config.JWT.PrivateKey,
		app.config.JWT.PublicKey,
		"go-api-template",
		24*time.Hour,
	)
	// Create authorization middleware for protected endpoints
	authorizationMiddleware := middleware.AuthorizationMiddleware(app.log, jwtService)

	// Register metrics handler (no middleware)
	if app.config.O11.PrometheusPath != "" {
		app.log.Info("Registering metrics handler", "path", app.config.O11.PrometheusPath)
		app.handlers.metricsHandler.RegisterRoutes(app.server, promhttp.Handler())
	}

	// Register health handler (no middleware)
	app.handlers.healthHandler.RegisterRoutes(app.server)

	// Register version handler with logging middleware
	app.handlers.versionHandler.RegisterRoutes(app.server, loggingMiddleware)

	// Register token handler with logging middleware (no authorization required for token creation)
	app.handlers.tokenHandler.RegisterRoutes(app.server, loggingMiddleware)

	// Register dummy business logic handler with logging and authorization middleware
	app.handlers.dummyBusinessLogicHandler.RegisterRoutes(app.server, loggingMiddleware, authorizationMiddleware)

	// Register swagger handler (no middleware)
	app.handlers.swaggerHandler.RegisterRoutes(app.server, httpSwagger.WrapHandler)
}
