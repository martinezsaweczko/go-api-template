package main

// @title           Swagger Example API
// @version         1.0
// @description     This is a sample server celler server.
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.url    http://www.swagger.io/support
// @contact.email  support@swagger.io

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8080
// @BasePath  /api/v1

// @securityDefinitions.apiKey Bearer
// @in header
// @name Authorization

// @externalDocs.description  OpenAPI
// @externalDocs.url          https://swagger.io/resources/open-api/

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/gofrs/uuid"

	"github.com/martinezsaweczko/go-api-template/app"
	"github.com/martinezsaweczko/go-api-template/config"
	"github.com/martinezsaweczko/go-api-template/docs"
	"github.com/martinezsaweczko/go-api-template/http_server"
	"github.com/martinezsaweczko/go-api-template/model"
	"github.com/martinezsaweczko/go-api-template/o11"
	_ "github.com/mattn/go-sqlite3"
)

// Buildinfo variables set via LDFlags in the Makefile
var (
	version   string
	buildTime string
	appName   string
)

func main() {

	// Create context for the application
	ctx := context.Background()
	// Create an instance of the application
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Create logger with config options
	file, errFile := config.LogFilePath(cfg.Log.FilePath)
	if errFile != nil {
		fmt.Printf("Error opening log file: %v\n", errFile)
		os.Exit(1)
	}

	defer file.Close()

	logLevel := config.ParseLogLevel(cfg.Log.Level)
	log := slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{Level: logLevel}))

	// Group execution UUID for all logs in this run
	executionID, _ := uuid.NewV4()
	log = log.With("execution_id", executionID)

	httpConf := &http_server.HTTPServerConfig{
		Addr:         cfg.HttpServer.Address,
		Port:         cfg.HttpServer.Port,
		Timeout:      cfg.HttpServer.Timeout,
		ReadTimeout:  cfg.HttpServer.ReadTimeout,
		WriteTimeout: cfg.HttpServer.WriteTimeout,
		Log:          log,
	}

	// Print build info and configuration details to the log
	log.Info("Build Info information", "version", version, "build_time", buildTime, "app_name", appName)
	log.Info("Configuration loaded", "http_address", httpConf.Addr, "http_port", httpConf.Port, "http_timeout", httpConf.Timeout, "http_read_timeout", httpConf.ReadTimeout, "http_write_timeout", httpConf.WriteTimeout, "log_level", cfg.Log.Level, "log_file_path", cfg.Log.FilePath)

	// Set up OpenTelemetry providers.
	otelShutdown, observabilityInst, otelErr := o11.SetupOTelSDK(ctx, &cfg.O11)
	if otelErr != nil {
		log.Error("OpenTelemetry setup error", "error", otelErr)
		os.Exit(1)
	}

	defer otelShutdown(ctx)

	// Set programatically swagger info
	// programmatically set swagger info
	docs.SwaggerInfo.Title = "Go Api Template"
	docs.SwaggerInfo.Description = "Template API"
	docs.SwaggerInfo.Version = "1.0"
	docs.SwaggerInfo.Host = fmt.Sprintf("%s:%d", httpConf.Addr, httpConf.Port)
	docs.SwaggerInfo.BasePath = "/api/v1"
	docs.SwaggerInfo.Schemes = []string{"http", "https"}

	//httpServer is an implementation of the Server interface defined in the app package
	httpServer := httpConf.New()

	// Build app with all dependencies injected
	app := app.NewApp(cfg).
		WithServer(httpServer).
		WithLog(log).
		WithObservability(&observabilityInst).
		WithBuildInfo(model.BuildInfo{
			Version:   version,
			BuildTime: buildTime,
			AppName:   appName,
		}).
		WithBasePath("/api/v1")

	// Run the application
	errApp := app.Run()
	if errApp != nil {
		log.Error("Application error", "error", errApp)
		os.Exit(1)
	}

	log.Info("Application terminated")

	// ctx := context.Background()
	// // Create a device store to store session data in SQLite
	// dbLog := waLog.Stdout("Database", "DEBUG", true)
	// container, err := sqlstore.New(ctx, "sqlite3", "file:whatsapp.db?_foreign_keys=on", dbLog)
	// if err != nil {
	// 	panic(err)
	// }
	// // If you want multiple sessions, remember their JIDs and use .GetDevice(jid) or .GetAllDevices() instead.
	// deviceStore, err := container.GetFirstDevice(ctx)
	// if err != nil {
	// 	panic(err)
	// }
	// clientLog := waLog.Stdout("Client", "DEBUG", true)
	// client := whatsmeow.NewClient(deviceStore, clientLog)
	// client.AddEventHandler(eventHandler)

	// if client.Store.ID == nil {
	// 	// No ID stored, new login
	// 	qrChan, _ := client.GetQRChannel(context.Background())
	// 	err = client.Connect()
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	for evt := range qrChan {
	// 		if evt.Event == "code" {
	// 			// Render the QR code here
	// 			// e.g. qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
	// 			// or just manually `echo 2@... | qrencode -t ansiutf8` in a terminal
	// 			fmt.Println("QR code:", evt.Code)
	//             qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
	// 		} else {
	// 			fmt.Println("Login event:", evt.Event)
	// 		}
	// 	}
	// } else {
	// 	// Already logged in, just connect
	// 	err = client.Connect()
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// }

	// // Listen to Ctrl+C (you can also do something else that prevents the program from exiting)
	// c := make(chan os.Signal, 1)
	// signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	// <-c

	// client.Disconnect()
}
