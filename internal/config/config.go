// Package config loads the application configuration from command-line flags
// and APP_-prefixed environment variables.
//
// Every flag has a matching environment variable: the flag name upper-cased,
// with dashes replaced by underscores and the APP_ prefix added (for example
// -jwt-token-ttl is APP_JWT_TOKEN_TTL). Precedence is flag > environment >
// default. A variable that is set but empty is honoured, so APP_METRICS_PATH=
// disables metrics; an empty value for a numeric, duration or boolean setting
// is rejected as invalid. Unset the variable to keep the default.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// EnvPrefix is prepended to every environment variable name.
const EnvPrefix = "APP_"

// Config is the complete application configuration.
type Config struct {
	Server        ServerConfig
	Log           LogConfig
	Observability ObservabilityConfig
	JWT           JWTConfig
}

// ConfigError aggregates every validation error found while loading.
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

// Unwrap exposes the individual validation errors to errors.Is and errors.As.
func (ce *ConfigError) Unwrap() []error {
	return ce.AppendedErrors
}

// EnvName returns the environment variable that sets the given flag.
func EnvName(flagName string) string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// newFlagSet declares every setting, with its default, bound to cfg.
func newFlagSet(cfg *Config) *flag.FlagSet {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	// HTTP server
	fs.StringVar(&cfg.Server.Address, "address", DefaultAddress, "HTTP listen address")
	fs.IntVar(&cfg.Server.Port, "port", DefaultPort, "HTTP listen port")
	fs.DurationVar(&cfg.Server.ReadHeaderTimeout, "read-header-timeout", DefaultReadHeaderTimeout, "Maximum time to read request headers")
	fs.DurationVar(&cfg.Server.ReadTimeout, "read-timeout", DefaultReadTimeout, "Maximum time to read the whole request (0 disables)")
	fs.DurationVar(&cfg.Server.WriteTimeout, "write-timeout", DefaultWriteTimeout, "Maximum time to write the response (0 disables)")
	fs.DurationVar(&cfg.Server.IdleTimeout, "idle-timeout", DefaultIdleTimeout, "Keep-alive idle timeout (0 uses the read timeout)")
	fs.DurationVar(&cfg.Server.ShutdownTimeout, "shutdown-timeout", DefaultShutdownTimeout, "Graceful shutdown deadline")
	fs.Int64Var(&cfg.Server.MaxBodyBytes, "max-body-bytes", DefaultMaxBodyBytes, "Maximum request body size in bytes")
	fs.Func("cors-allowed-origins", "Comma-separated CORS allowed origins, or * (empty disables CORS)", func(v string) error {
		cfg.Server.CORSAllowedOrigins = splitList(v)
		return nil
	})

	// Logging
	fs.StringVar(&cfg.Log.Level, "log-level", DefaultLogLevel, "Log level (debug, info, warn, error)")
	fs.StringVar(&cfg.Log.FilePath, "log-file-path", LogStdout, `Log file path, or "stdout"`)

	// Observability
	fs.StringVar(&cfg.Observability.ServiceName, "service-name", DefaultServiceName, "service.name reported in traces and metrics")
	fs.StringVar(&cfg.Observability.OTLPEndpoint, "otel-endpoint", "", "OTLP gRPC trace endpoint as host:port (empty disables tracing export)")
	fs.StringVar(&cfg.Observability.MetricsPath, "metrics-path", DefaultMetricsPath, "Prometheus metrics path (empty disables metrics)")

	// JWT
	fs.StringVar(&cfg.JWT.PublicKeyPath, "jwt-public-key-path", "keys/public.pem", "Path to the JWT RSA public key")
	fs.StringVar(&cfg.JWT.PrivateKeyPath, "jwt-private-key-path", "keys/private.pem", "Path to the JWT RSA private key")
	fs.StringVar(&cfg.JWT.Issuer, "jwt-issuer", DefaultJWTIssuer, "JWT issuer set on and required from tokens")
	fs.StringVar(&cfg.JWT.Audience, "jwt-audience", "", "JWT audience set on and required from tokens (empty disables the audience check)")
	fs.DurationVar(&cfg.JWT.TokenTTL, "jwt-token-ttl", DefaultJWTTokenTTL, "Lifetime of issued JWT tokens")
	fs.BoolVar(&cfg.JWT.DevTokenEndpoint, "jwt-dev-token-endpoint", false, "Enable the unauthenticated POST /token endpoint (development only)")

	return fs
}

// Load builds the configuration from defaults, then the environment read
// through lookupEnv (typically os.LookupEnv), then args (typically
// os.Args[1:]), and validates it. It returns flag.ErrHelp when help was
// requested.
func Load(args []string, lookupEnv func(string) (string, bool)) (*Config, error) {
	cfg := &Config{}
	fs := newFlagSet(cfg)

	var errs []error
	fs.VisitAll(func(f *flag.Flag) {
		name := EnvName(f.Name)
		v, ok := lookupEnv(name)
		if !ok {
			return
		}
		if err := fs.Set(f.Name, v); err != nil {
			errs = append(errs, fmt.Errorf("invalid value %q for %s: %w", v, name, err))
		}
	})

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		return nil, &ConfigError{Message: "Invalid configuration", AppendedErrors: []error{err}}
	}
	if fs.NArg() > 0 {
		errs = append(errs, fmt.Errorf("unexpected argument %q", fs.Arg(0)))
	}

	if err := cfg.Server.validate(); err != nil {
		errs = append(errs, err)
	}
	if err := cfg.Log.validate(); err != nil {
		errs = append(errs, err)
	}
	if err := cfg.Observability.validate(); err != nil {
		errs = append(errs, err)
	}
	if err := cfg.JWT.LoadKeys(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, &ConfigError{Message: "Invalid configuration", AppendedErrors: errs}
	}
	return cfg, nil
}

// Usage writes every flag with its environment variable and default to w.
func Usage(w io.Writer) {
	fs := newFlagSet(&Config{})
	fmt.Fprintf(w, "Usage: api [flags]\n\nEvery flag can also be set through its environment variable (flag > env > default).\n\n")
	fs.VisitAll(func(f *flag.Flag) {
		fmt.Fprintf(w, "  -%s (env %s)\n    \t%s", f.Name, EnvName(f.Name), f.Usage)
		if f.DefValue != "" {
			fmt.Fprintf(w, " (default %q)", f.DefValue)
		}
		fmt.Fprintln(w)
	})
}

// splitList splits a comma-separated list, trimming spaces and dropping empty items.
func splitList(v string) []string {
	var out []string
	for item := range strings.SplitSeq(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
