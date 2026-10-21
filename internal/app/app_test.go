package app_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/martinezsaweczko/go-api-template/internal/app"
	"github.com/martinezsaweczko/go-api-template/internal/config"
	"github.com/martinezsaweczko/go-api-template/internal/observability"
	"github.com/martinezsaweczko/go-api-template/internal/transport/http/server"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &config.Config{
		Server: config.ServerConfig{
			Address:           "127.0.0.1",
			ReadHeaderTimeout: time.Second,
			ShutdownTimeout:   5 * time.Second,
			MaxBodyBytes:      1 << 10,
		},
		Log:           config.LogConfig{Level: "info", FilePath: config.LogStdout},
		Observability: config.ObservabilityConfig{ServiceName: "test"},
		JWT: config.JWTConfig{
			PrivateKey: key,
			PublicKey:  &key.PublicKey,
			Issuer:     "test",
			TokenTTL:   time.Minute,
		},
	}
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_ = resp.Body.Close()
	return resp
}

func TestRunServesProbesAndShutsDownOnCancel(t *testing.T) {
	cfg := testConfig(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := (&server.HTTPServerConfig{Address: "127.0.0.1", Port: 0, ReadHeaderTimeout: time.Second, Log: log}).New()
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	a := app.NewApp(cfg).
		WithServer(srv).
		WithLog(log).
		WithObservability(&observability.ObservabilityInst{Trace: tp, MeterProvider: noop.NewMeterProvider()}).
		WithBasePath("/api/v1")

	if a.Ready() {
		t.Fatal("Ready() = true before Run")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for !a.Ready() {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("app did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	base := "http://" + srv.Addr().String()
	for _, path := range []string{"/livez", "/readyz", "/api/v1/health"} {
		if resp := get(t, base+path); resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
		if resp := get(t, base+path); resp.Header.Get("X-Request-ID") == "" {
			t.Errorf("GET %s has no X-Request-ID: global middleware not applied", path)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil after cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
	if a.Ready() {
		t.Error("Ready() = true after shutdown")
	}
}

func TestRunRequiresDependencies(t *testing.T) {
	if err := app.NewApp(testConfig(t)).Run(context.Background()); err == nil {
		t.Fatal("Run() without server = nil error, want error")
	}
}
