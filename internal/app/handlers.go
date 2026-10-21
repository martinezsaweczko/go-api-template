package app

import (
	"fmt"

	_ "github.com/martinezsaweczko/go-api-template/docs" // This is required to serve swagger docs, it registers the swagger files with the httpSwagger handler
	"github.com/martinezsaweczko/go-api-template/internal/service"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/handlers"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
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
	app.handlers.versionHandler = (&handlers.VersionHandlerConfig{
		Log:          app.log,
		Cfg:          app.config,
		AppBuildInfo: app.buildInfo,
		BasePath:     app.basePath,
	}).NewVersionHandler()

	// Initialize swagger handler
	swaggerHandlerConf := handlers.SwaggerHandlerConfig{
		Log:           app.log,
		Observability: app.observability,
		Cfg:           app.config,
	}
	swaggerHandler := swaggerHandlerConf.NewSwaggerHandler()
	app.handlers.swaggerHandler = swaggerHandler

	// Initialize health handler
	app.handlers.healthHandler = (&handlers.HealthHandlerConfig{
		Log:      app.log,
		Cfg:      app.config,
		BasePath: app.basePath,
		Ready:    app.Ready,
	}).NewHealthHandler()

	// Initialize dummy business logic handler
	app.handlers.dummyBusinessLogicHandler = (&handlers.DummyBusinessLogicHandlerConfig{
		Log:           app.log,
		Observability: app.observability,
		Cfg:           app.config,
		BasePath:      app.basePath,
	}).NewDummyBusinessLogicHandler()

	// Initialize the JWT service once; it is shared by the token handler and the authorization middleware
	app.jwtService, err = service.NewJWTService(service.JWTServiceConfig{
		PrivateKey: app.config.JWT.PrivateKey,
		PublicKey:  app.config.JWT.PublicKey,
		Issuer:     app.config.JWT.Issuer,
		Audience:   app.config.JWT.Audience,
		TTL:        app.config.JWT.TokenTTL,
		Log:        app.log,
	})
	if err != nil {
		app.log.Error("Failed to initialize JWT service", "error", err)
		return fmt.Errorf("failed to initialize JWT service: %w", err)
	}

	// Initialize the token handler only when the development token endpoint is enabled
	if app.config.JWT.DevTokenEndpoint {
		app.handlers.tokenHandler = (&handlers.TokenHandlerConfig{
			Log:        app.log,
			Cfg:        app.config,
			JWTService: app.jwtService,
			BasePath:   app.basePath,
		}).NewTokenHandler()
	}

	return nil
}

// registerRoutes installs the global middleware chain and registers all routes
func (app *App) registerRoutes() error {
	metricsPath := app.config.Observability.MetricsPath

	// Probes and metrics scrapes are logged at Debug to keep the logs readable.
	quietPaths := []string{handlers.LivenessPath, handlers.ReadinessPath, app.basePath + "/health"}
	if metricsPath != "" {
		quietPaths = append(quietPaths, metricsPath)
	}

	metrics, err := middleware.Metrics(app.observability.MeterProvider)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics middleware: %w", err)
	}

	// Global chain, outermost first: every request (including unmatched
	// routes) gets a server span continuing any propagated trace, request
	// count and duration metrics, a request ID, an access log entry, panic
	// recovery, CORS and a body size limit. Probes and scrapes are not traced.
	app.server.Use(
		middleware.Tracing(app.observability.Trace, app.observability.Propagator, quietPaths...),
		metrics,
		middleware.RequestID,
		middleware.Logging(app.log, quietPaths...),
		middleware.Recovery(app.log),
		middleware.CORS(app.config.Server.CORSAllowedOrigins),
		middleware.BodyLimit(app.config.Server.MaxBodyBytes),
	)

	// Create authorization middleware for protected endpoints
	authorizationMiddleware := middleware.AuthorizationMiddleware(app.log, app.jwtService)

	if metricsPath != "" {
		app.log.Info("Registering metrics handler", "path", metricsPath)
		app.handlers.metricsHandler.RegisterRoutes(app.server, promhttp.Handler())
	}

	// Liveness, readiness and legacy health routes
	app.handlers.healthHandler.RegisterRoutes(app.server)

	app.handlers.versionHandler.RegisterRoutes(app.server)

	// Register the unauthenticated token handler only when explicitly enabled for development
	if app.handlers.tokenHandler != nil {
		app.log.Warn("Development token endpoint enabled: anyone can mint tokens; never enable it in production",
			"path", app.basePath+"/token")
		app.handlers.tokenHandler.RegisterRoutes(app.server)
	}

	// Protected routes add the authorization middleware on top of the global chain
	app.handlers.dummyBusinessLogicHandler.RegisterRoutes(app.server, authorizationMiddleware)

	app.handlers.swaggerHandler.RegisterRoutes(app.server, httpSwagger.WrapHandler)
	return nil
}
