package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, port int) *HTTPServer {
	t.Helper()
	cfg := &HTTPServerConfig{
		Addr:         "127.0.0.1",
		Port:         port,
		Timeout:      5,
		ReadTimeout:  5,
		WriteTimeout: 5,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return cfg.New()
}

// freePort reserves an ephemeral port on the loopback interface and releases
// it, returning the port number for the server under test to bind.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("release free port: %v", err)
	}
	return port
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
	port := freePort(t)
	srv := newTestServer(t, port)
	srv.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "pong")
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/ping")
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Errors() is buffered and only receives fatal serve errors; after a clean
	// shutdown (http.ErrServerClosed) it must stay empty.
	select {
	case err := <-srv.Errors():
		t.Fatalf("Errors() reported %v after clean shutdown", err)
	default:
	}
}
