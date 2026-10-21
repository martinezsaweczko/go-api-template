package config

import (
	"fmt"
	"strings"
)

const (
	// DefaultServiceName is the service.name reported when none is configured.
	DefaultServiceName = "go-api-template"
	// DefaultMetricsPath is the path serving Prometheus metrics.
	DefaultMetricsPath = "/metrics"
)

// ObservabilityConfig configures tracing and metrics.
type ObservabilityConfig struct {
	// ServiceName is reported as the service.name resource attribute.
	ServiceName string
	// OTLPEndpoint is the OTLP gRPC trace collector as host:port. Empty
	// disables trace export, so tracing is a no-op.
	OTLPEndpoint string
	// MetricsPath serves Prometheus metrics; empty disables metrics.
	MetricsPath string
}

func (c *ObservabilityConfig) validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("observability service name cannot be empty")
	}
	// The OTLP exporter speaks gRPC and expects host:port without a scheme.
	if strings.HasPrefix(c.OTLPEndpoint, "http://") || strings.HasPrefix(c.OTLPEndpoint, "https://") {
		return fmt.Errorf("OTLP endpoint must be host:port without http:// or https://")
	}
	if c.MetricsPath != "" && !strings.HasPrefix(c.MetricsPath, "/") {
		return fmt.Errorf("metrics path must start with /")
	}
	return nil
}
