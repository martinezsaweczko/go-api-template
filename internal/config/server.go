package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Server defaults.
const (
	DefaultAddress           = "0.0.0.0"
	DefaultPort              = 8080
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 10 * time.Second
	DefaultWriteTimeout      = 10 * time.Second
	DefaultIdleTimeout       = 60 * time.Second
	DefaultShutdownTimeout   = 30 * time.Second
	DefaultMaxBodyBytes      = 1 << 20 // 1 MiB
)

// ServerConfig configures the HTTP server.
type ServerConfig struct {
	Address           string
	Port              int
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	// MaxBodyBytes caps the size of every request body.
	MaxBodyBytes int64
	// CORSAllowedOrigins lists the origins allowed by CORS; empty disables CORS.
	CORSAllowedOrigins []string
}

func (s *ServerConfig) validate() error {
	var errs []error
	if s.Address == "" {
		errs = append(errs, fmt.Errorf("server address cannot be empty"))
	}
	if s.Port <= 0 || s.Port > 65535 {
		errs = append(errs, fmt.Errorf("server port must be between 1 and 65535"))
	}
	if s.ReadHeaderTimeout <= 0 {
		errs = append(errs, fmt.Errorf("server read header timeout must be positive"))
	}
	if s.ReadTimeout < 0 || s.WriteTimeout < 0 || s.IdleTimeout < 0 {
		errs = append(errs, fmt.Errorf("server read, write and idle timeouts cannot be negative"))
	}
	if s.ShutdownTimeout <= 0 {
		errs = append(errs, fmt.Errorf("server shutdown timeout must be positive"))
	}
	if s.MaxBodyBytes <= 0 {
		errs = append(errs, fmt.Errorf("server max body bytes must be positive"))
	}
	for _, origin := range s.CORSAllowedOrigins {
		if origin != "*" && !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
			errs = append(errs, fmt.Errorf("invalid CORS origin %q: use * or a full http(s) origin", origin))
		}
	}
	return errors.Join(errs...)
}
