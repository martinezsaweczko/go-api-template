package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, port int) *HTTPServer {
	t.Helper()
	cfg := &HTTPServerConfig{
		Address:           "127.0.0.1",
		Port:              port,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       5 * time.Second,
		Log:               slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return cfg.New()
}

func stopServer(t *testing.T, srv *HTTPServer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestNewAppliesTimeouts(t *testing.T) {
	srv := newTestServer(t, 0)
	got := srv.httpServer
	if got.ReadHeaderTimeout != 2*time.Second || got.ReadTimeout != 5*time.Second ||
		got.WriteTimeout != 5*time.Second || got.IdleTimeout != 5*time.Second {
		t.Errorf("timeouts = header %v read %v write %v idle %v", got.ReadHeaderTimeout, got.ReadTimeout, got.WriteTimeout, got.IdleTimeout)
	}
}

func TestAddrIsNilBeforeStart(t *testing.T) {
	if addr := newTestServer(t, 0).Addr(); addr != nil {
		t.Errorf("Addr() before Start = %v, want nil", addr)
	}
}

func TestStartReturnsErrorWhenPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := newTestServer(t, ln.Addr().(*net.TCPAddr).Port)

	err = srv.Start()
	if err == nil {
		_ = srv.Stop(context.Background())
		t.Fatal("Start() returned nil, want error for port already in use")
	}
	if !strings.Contains(err.Error(), "listen on") {
		t.Errorf("Start() error = %q, want it to mention the listen address", err)
	}
}

func TestStartServesRequestsAndStopsCleanly(t *testing.T) {
	srv := newTestServer(t, 0)
	srv.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "pong")
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	addr := srv.Addr()
	if addr == nil || addr.(*net.TCPAddr).Port == 0 {
		t.Fatalf("Addr() = %v, want the bound ephemeral port", addr)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr.String() + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != "pong" {
		t.Errorf("body = %q, want %q", body, "pong")
	}

	stopServer(t, srv)

	// Errors() is buffered and only receives fatal serve errors; after a clean
	// shutdown (http.ErrServerClosed) it must stay empty.
	select {
	case err := <-srv.Errors():
		t.Fatalf("Errors() reported %v after clean shutdown", err)
	default:
	}
}

func TestUseAppliesMiddlewareToEveryRoute(t *testing.T) {
	srv := newTestServer(t, 0)
	srv.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Global", "yes")
			next.ServeHTTP(w, r)
		})
	})
	srv.HandleFunc("GET /a", func(http.ResponseWriter, *http.Request) {})
	srv.HandleFunc("GET /b", func(http.ResponseWriter, *http.Request) {})

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { stopServer(t, srv) })

	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/a", "/b", "/unknown"} {
		resp, err := client.Get("http://" + srv.Addr().String() + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.Header.Get("X-Global") != "yes" {
			t.Errorf("GET %s: global middleware not applied", path)
		}
	}
}
