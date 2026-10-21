package main

// @title           Go API Template
// @version         1.0
// @description     Production-ready Go API template.

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @BasePath  /api/v1

// @securityDefinitions.apiKey Bearer
// @in header
// @name Authorization

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/martinezsaweczko/go-api-template/docs"
	"github.com/martinezsaweczko/go-api-template/internal/app"
	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/model"
	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/server"
)

// Buildinfo variables set via LDFlags in the Makefile
var (
	version   string
	buildTime string
	appName   string
)

const basePath = "/api/v1"

func main() {
	os.Exit(run())
}

// run wires and runs the application and returns the process exit code, so
// deferred cleanups run before main exits.
func run() int {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		config.Usage(os.Stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		return 1
	}

	out, closeLog, err := config.OpenLogOutput(cfg.Log.FilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log file: %v\n", err)
		return 1
	}
	defer closeLog()

	log := slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: config.ParseLogLevel(cfg.Log.Level)}))

	log.Info("Build info", "version", version, "build_time", buildTime, "app_name", appName)
	log.Info("Configuration loaded",
		"address", cfg.Server.Address,
		"port", cfg.Server.Port,
		"shutdown_timeout", cfg.Server.ShutdownTimeout,
		"max_body_bytes", cfg.Server.MaxBodyBytes,
		"cors_allowed_origins", cfg.Server.CORSAllowedOrigins,
		"log_level", cfg.Log.Level,
		"log_file_path", cfg.Log.FilePath,
		"service_name", cfg.Observability.ServiceName,
		"metrics_path", cfg.Observability.MetricsPath,
		"otel_endpoint", cfg.Observability.OTLPEndpoint,
	)

	// SIGINT and SIGTERM cancel ctx, which starts a graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	otelShutdown, observabilityInst, err := observability.SetupOTelSDK(ctx, &cfg.Observability, version)
	if err != nil {
		log.Error("OpenTelemetry setup error", "error", err)
		return 1
	}
	defer func() {
		// ctx is cancelled by now, so flushing telemetry gets its own deadline.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			log.Error("OpenTelemetry shutdown error", "error", err)
		}
	}()

	// Leave Host empty so Swagger UI targets the host serving the page,
	// instead of the bind address (0.0.0.0 is not reachable from a browser).
	docs.SwaggerInfo.Host = ""
	docs.SwaggerInfo.BasePath = basePath

	httpServer := (&server.HTTPServerConfig{
		Address:           cfg.Server.Address,
		Port:              cfg.Server.Port,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		Log:               log,
	}).New()

	application := app.NewApp(cfg).
		WithServer(httpServer).
		WithLog(log).
		WithObservability(&observabilityInst).
		WithBuildInfo(model.BuildInfo{
			Version:   version,
			BuildTime: buildTime,
			AppName:   appName,
		}).
		WithBasePath(basePath)

	if err := application.Run(ctx); err != nil {
		log.Error("Application error", "error", err)
		return 1
	}

	log.Info("Application terminated")
	return 0
}
