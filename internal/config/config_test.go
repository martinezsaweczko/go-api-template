package config_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/martinezsaweczko/go-api-template/internal/config"
)

// writeKeys writes a fresh RSA key pair to a temporary directory and returns
// an environment that points the JWT settings at it.
func writeKeys(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	priv := filepath.Join(dir, "private.pem")
	pub := filepath.Join(dir, "public.pem")
	if err := os.WriteFile(priv, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(pub, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	return map[string]string{
		"APP_JWT_PRIVATE_KEY_PATH": priv,
		"APP_JWT_PUBLIC_KEY_PATH":  pub,
	}
}

func getenvFrom(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func withEnv(base map[string]string, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(nil, getenvFrom(writeKeys(t)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"address", cfg.Server.Address, "0.0.0.0"},
		{"port", cfg.Server.Port, 8080},
		{"read header timeout", cfg.Server.ReadHeaderTimeout, 5 * time.Second},
		{"read timeout", cfg.Server.ReadTimeout, 10 * time.Second},
		{"write timeout", cfg.Server.WriteTimeout, 10 * time.Second},
		{"idle timeout", cfg.Server.IdleTimeout, 60 * time.Second},
		{"shutdown timeout", cfg.Server.ShutdownTimeout, 30 * time.Second},
		{"max body bytes", cfg.Server.MaxBodyBytes, int64(1 << 20)},
		{"cors origins", len(cfg.Server.CORSAllowedOrigins), 0},
		{"log level", cfg.Log.Level, "info"},
		{"log file", cfg.Log.FilePath, config.LogStdout},
		{"service name", cfg.Observability.ServiceName, "go-api-template"},
		{"otlp endpoint", cfg.Observability.OTLPEndpoint, ""},
		{"metrics path", cfg.Observability.MetricsPath, "/metrics"},
		{"jwt issuer", cfg.JWT.Issuer, config.DefaultJWTIssuer},
		{"jwt ttl", cfg.JWT.TokenTTL, config.DefaultJWTTokenTTL},
		{"jwt dev endpoint", cfg.JWT.DevTokenEndpoint, false},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if cfg.JWT.PrivateKey == nil || cfg.JWT.PublicKey == nil {
		t.Error("JWT keys were not loaded")
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	env := withEnv(writeKeys(t), map[string]string{
		"APP_ADDRESS":                "127.0.0.1",
		"APP_PORT":                   "9090",
		"APP_SHUTDOWN_TIMEOUT":       "10s",
		"APP_MAX_BODY_BYTES":         "2048",
		"APP_CORS_ALLOWED_ORIGINS":   "https://a.example, https://b.example",
		"APP_LOG_LEVEL":              "debug",
		"APP_SERVICE_NAME":           "orders",
		"APP_OTEL_ENDPOINT":          "collector:4317",
		"APP_METRICS_PATH":           "",
		"APP_JWT_AUDIENCE":           "api",
		"APP_JWT_TOKEN_TTL":          "15m",
		"APP_JWT_DEV_TOKEN_ENDPOINT": "true",
	})

	cfg, err := config.Load(nil, getenvFrom(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Address != "127.0.0.1" || cfg.Server.Port != 9090 {
		t.Errorf("listen = %s:%d, want 127.0.0.1:9090", cfg.Server.Address, cfg.Server.Port)
	}
	if cfg.Server.ShutdownTimeout != 10*time.Second {
		t.Errorf("shutdown timeout = %v, want 10s", cfg.Server.ShutdownTimeout)
	}
	if cfg.Server.MaxBodyBytes != 2048 {
		t.Errorf("max body bytes = %d, want 2048", cfg.Server.MaxBodyBytes)
	}
	wantOrigins := []string{"https://a.example", "https://b.example"}
	if !reflect.DeepEqual(cfg.Server.CORSAllowedOrigins, wantOrigins) {
		t.Errorf("cors origins = %v, want %v", cfg.Server.CORSAllowedOrigins, wantOrigins)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log level = %q, want debug", cfg.Log.Level)
	}
	if cfg.Observability.ServiceName != "orders" || cfg.Observability.OTLPEndpoint != "collector:4317" {
		t.Errorf("observability = %+v", cfg.Observability)
	}
	// An empty environment value means "unset", so the default applies.
	if cfg.Observability.MetricsPath != "/metrics" {
		t.Errorf("metrics path = %q, want default /metrics", cfg.Observability.MetricsPath)
	}
	if cfg.JWT.Audience != "api" || cfg.JWT.TokenTTL != 15*time.Minute || !cfg.JWT.DevTokenEndpoint {
		t.Errorf("jwt = audience %q ttl %v dev %v", cfg.JWT.Audience, cfg.JWT.TokenTTL, cfg.JWT.DevTokenEndpoint)
	}
}

func TestLoadFlagOverridesEnvironment(t *testing.T) {
	env := withEnv(writeKeys(t), map[string]string{"APP_PORT": "9090", "APP_LOG_LEVEL": "debug"})

	cfg, err := config.Load([]string{"-port", "7070"}, getenvFrom(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 7070 {
		t.Errorf("port = %d, want flag value 7070", cfg.Server.Port)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log level = %q, want env value debug", cfg.Log.Level)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	keys := writeKeys(t)
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{name: "malformed env value", env: map[string]string{"APP_PORT": "abc"}, wantErr: "APP_PORT"},
		{name: "port out of range", args: []string{"-port", "70000"}, wantErr: "port"},
		{name: "unknown log level", env: map[string]string{"APP_LOG_LEVEL": "verbose"}, wantErr: "log level"},
		{name: "empty log file path", args: []string{"-log-file-path", ""}, wantErr: "log file path"},
		{name: "missing log directory", args: []string{"-log-file-path", "/does/not/exist/app.log"}, wantErr: "log file directory"},
		{name: "zero read header timeout", args: []string{"-read-header-timeout", "0s"}, wantErr: "read header timeout"},
		{name: "zero shutdown timeout", args: []string{"-shutdown-timeout", "0s"}, wantErr: "shutdown timeout"},
		{name: "non-positive body limit", args: []string{"-max-body-bytes", "0"}, wantErr: "max body bytes"},
		{name: "invalid cors origin", args: []string{"-cors-allowed-origins", "ftp://x.example"}, wantErr: "CORS origin"},
		{name: "relative metrics path", args: []string{"-metrics-path", "metrics"}, wantErr: "metrics path"},
		{name: "otlp endpoint with scheme", args: []string{"-otel-endpoint", "http://collector:4317"}, wantErr: "OTLP endpoint"},
		{name: "empty service name", args: []string{"-service-name", ""}, wantErr: "service name"},
		{name: "missing key file", args: []string{"-jwt-public-key-path", "/does/not/exist.pem"}, wantErr: "public key"},
		{name: "unknown flag", args: []string{"-nope"}, wantErr: "nope"},
		{name: "positional argument", args: []string{"extra"}, wantErr: "unexpected argument"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load(tt.args, getenvFrom(withEnv(keys, tt.env)))
			if err == nil {
				t.Fatalf("Load() = %+v, want error containing %q", cfg, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadHelpReturnsErrHelp(t *testing.T) {
	_, err := config.Load([]string{"-h"}, getenvFrom(nil))
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("Load(-h) error = %v, want flag.ErrHelp", err)
	}
}

func TestUsageListsEnvironmentVariables(t *testing.T) {
	var b strings.Builder
	config.Usage(&b)
	for _, want := range []string{"-port", "APP_PORT", "-jwt-dev-token-endpoint", "APP_JWT_DEV_TOKEN_ENDPOINT"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("Usage() output does not mention %q", want)
		}
	}
}

func TestOpenLogOutputCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")

	w, closeFn, err := config.OpenLogOutput(path)
	if err != nil {
		t.Fatalf("OpenLogOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if _, err := w.Write([]byte("line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log file permissions = %o, want 600", perm)
	}
}

func TestOpenLogOutputStdout(t *testing.T) {
	w, closeFn, err := config.OpenLogOutput(config.LogStdout)
	if err != nil {
		t.Fatalf("OpenLogOutput(stdout) error = %v", err)
	}
	if w != os.Stdout {
		t.Errorf("writer = %v, want os.Stdout", w)
	}
	// Closing the stdout sentinel must not close the process stdout.
	if err := closeFn(); err != nil {
		t.Errorf("close = %v, want nil", err)
	}
}
