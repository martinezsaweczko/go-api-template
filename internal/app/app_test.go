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
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

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

// startApp runs the app on an ephemeral port until the test ends and
// returns its base URL.
func startApp(t *testing.T, obs *observability.ObservabilityInst) string {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := (&server.HTTPServerConfig{Address: "127.0.0.1", Port: 0, ReadHeaderTimeout: time.Second, Log: log}).New()
	a := app.NewApp(testConfig(t)).WithServer(srv).WithLog(log).WithObservability(obs).WithBasePath("/api/v1")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run() error = %v", err)
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for !a.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("app did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "http://" + srv.Addr().String()
}

func TestRunInstrumentsRequests(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})
	base := startApp(t, &observability.ObservabilityInst{Trace: tp, MeterProvider: mp, Propagator: propagation.TraceContext{}})

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/version", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET version: %v", err)
	}
	_ = resp.Body.Close()
	get(t, base+"/livez")

	var server sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == "GET /livez" {
			t.Error("probe /livez was traced, want it excluded")
		}
		if s.Name() == "GET /api/v1/version" {
			server = s
		}
	}
	if server == nil {
		t.Fatalf("no server span named after the route; spans = %d", len(recorder.Ended()))
	}
	if got := server.SpanContext().TraceID().String(); got != traceID {
		t.Errorf("server span trace ID = %s, want propagated %s", got, traceID)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	routes := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == "http.server.requests" {
				for _, dp := range sum.DataPoints {
					route, _ := dp.Attributes.Value("http.route")
					routes[route.AsString()] += dp.Value
				}
			}
		}
	}
	for _, route := range []string{"/api/v1/version", "/livez"} {
		if routes[route] != 1 {
			t.Errorf("http.server.requests{http.route=%q} = %d, want 1 (all routes: %v)", route, routes[route], routes)
		}
	}
}

func TestRunRequiresDependencies(t *testing.T) {
	if err := app.NewApp(testConfig(t)).Run(context.Background()); err == nil {
		t.Fatal("Run() without server = nil error, want error")
	}
}
