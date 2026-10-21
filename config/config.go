package config

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

type Config struct {
	HttpServer ServerConfig
	Log        LogConfig
	O11        O11Config
	JWT        JWTConfig
}

type ConfigError struct {
	Message        string
	AppendedErrors []error
}

// Error implements the error interface for ConfigError
// It formats all validation errors into a single error message
func (ce *ConfigError) Error() string {
	if len(ce.AppendedErrors) == 0 {
		return ce.Message
	}

	var errMessages []string
	for _, err := range ce.AppendedErrors {
		errMessages = append(errMessages, err.Error())
	}

	return fmt.Sprintf("%s:\n  - %s", ce.Message, strings.Join(errMessages, "\n  - "))
}

// Load configuration from file or environment variables
// Parse for stdin
// Return the configuration struct
func LoadConfig() (*Config, *ConfigError) {
	config := &Config{}
	// Initialize HttpServerConfig fields
	config.HttpServer = ServerConfig{}
	listErrors := []error{}

	// Define command-line flags for HTTP server configuration
	var serverAddress string
	var serverPort int
	var serverTimeout int
	var serverReadTimeout int
	var serverWriteTimeout int

	// Define command-line flags for LOG configuration
	var logLevel string
	var logFilePath string

	// Define command-line flags for O11 configuration
	var o11TracerEndpoint string
	var o11PrometheusPath string

	// Define command-line flags for JWT configuration
	var jwtPublicKeyPath string
	var jwtPrivateKeyPath string
	var jwtIssuer string
	var jwtAudience string
	var jwtTokenTTL time.Duration
	var jwtDevTokenEndpoint bool

	// Flags server
	flag.StringVar(&serverAddress, "http-address", "localhost", "HTTP server address")
	flag.IntVar(&serverPort, "http-port", 8080, "HTTP server port")
	flag.IntVar(&serverTimeout, "http-timeout", 30, "HTTP server timeout in seconds")
	flag.IntVar(&serverReadTimeout, "http-read-timeout", 10, "HTTP server read timeout in seconds")
	flag.IntVar(&serverWriteTimeout, "http-write-timeout", 10, "HTTP server write timeout in seconds")

	// Flags log
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&logFilePath, "log-file-path", "stdout", "Log file path (default is stdout)")

	//O11 flags
	flag.StringVar(&o11TracerEndpoint, "o11-tracer-endpoint", "", "OpenTelemetry tracer endpoint (gRPC, without http:// or https://)")
	flag.StringVar(&o11PrometheusPath, "o11-prometheus-path", "", "OpenTelemetry Prometheus metrics path (default is /metrics)")

	// JWT flags
	flag.StringVar(&jwtPublicKeyPath, "jwt-public-key-path", "keys/public.pem", "Path to JWT public key file")
	flag.StringVar(&jwtPrivateKeyPath, "jwt-private-key-path", "keys/private.pem", "Path to JWT private key file")
	flag.StringVar(&jwtIssuer, "jwt-issuer", DefaultJWTIssuer, "JWT issuer set on and required from tokens")
	flag.StringVar(&jwtAudience, "jwt-audience", "", "JWT audience set on and required from tokens (empty disables the audience check)")
	flag.DurationVar(&jwtTokenTTL, "jwt-token-ttl", DefaultJWTTokenTTL, "Lifetime of issued JWT tokens")
	flag.BoolVar(&jwtDevTokenEndpoint, "jwt-dev-token-endpoint", false, "Enable the unauthenticated POST /token endpoint (development only)")

	flag.Parse()

	config.HttpServer.Address = serverAddress
	config.HttpServer.Port = serverPort
	config.HttpServer.Timeout = serverTimeout
	config.HttpServer.ReadTimeout = serverReadTimeout
	config.HttpServer.WriteTimeout = serverWriteTimeout

	if err := config.HttpServer.validate(); err != nil {
		listErrors = append(listErrors, err)
	}

	config.Log.Level = logLevel
	config.Log.FilePath = logFilePath

	if err := config.Log.validate(); err != nil {
		listErrors = append(listErrors, err)
	}

	// Set O11 configuration
	o11Conf := &O11Config{
		TracerEndpoint: o11TracerEndpoint,
		PrometheusPath: o11PrometheusPath,
	}

	config.O11 = *o11Conf

	if err := o11Conf.Validate(); err != nil {
		listErrors = append(listErrors, err)
	}

	// Set JWT configuration
	config.JWT = JWTConfig{
		PublicKeyPath:    jwtPublicKeyPath,
		PrivateKeyPath:   jwtPrivateKeyPath,
		Issuer:           jwtIssuer,
		Audience:         jwtAudience,
		TokenTTL:         jwtTokenTTL,
		DevTokenEndpoint: jwtDevTokenEndpoint,
	}

	if err := config.JWT.LoadKeys(); err != nil {
		listErrors = append(listErrors, err)
	}

	if len(listErrors) > 0 {
		return nil, &ConfigError{Message: "Invalid configuration", AppendedErrors: listErrors}
	}

	return config, nil
}
