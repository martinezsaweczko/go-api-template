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
		Ready:         app.Ready,
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
		tokenHandlerConf := handlers.TokenHandlerConfig{
			Log:           app.log,
			Observability: app.observability,
			Cfg:           app.config,
			JWTService:    app.jwtService,
			BasePath:      app.basePath,
		}
		app.handlers.tokenHandler, err = tokenHandlerConf.NewTokenHandler()
		if err != nil {
			app.log.Error("Failed to initialize token handler", "error", err)
			return err
		}
	}

	return nil
}

// registerRoutes installs the global middleware chain and registers all routes
func (app *App) registerRoutes() {
	metricsPath := app.config.Observability.MetricsPath

	// Probes and metrics scrapes are logged at Debug to keep the logs readable.
	quietPaths := []string{handlers.LivenessPath, handlers.ReadinessPath, app.basePath + "/health"}
	if metricsPath != "" {
		quietPaths = append(quietPaths, metricsPath)
	}

	// Global chain, outermost first: every request (including unmatched
	// routes) gets a request ID, an access log entry, panic recovery, CORS
	// and a body size limit.
	app.server.Use(
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
}
