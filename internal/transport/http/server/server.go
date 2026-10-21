// Package server wraps net/http.Server with synchronous listen errors, a
// global middleware chain and graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// HTTPServerConfig configures an HTTPServer. Port 0 binds an ephemeral port.
type HTTPServerConfig struct {
	Address           string
	Port              int
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	Log               *slog.Logger
}

// HTTPServer serves a ServeMux wrapped by a global middleware chain.
type HTTPServer struct {
	httpServer  *http.Server
	mux         *http.ServeMux
	middlewares []middleware.Middleware
	log         *slog.Logger
	errs        chan error

	mu       sync.Mutex
	listener net.Listener
}

// New creates a new HTTP server for the configured address and port.
func (h *HTTPServerConfig) New() *HTTPServer {
	return &HTTPServer{
		mux: http.NewServeMux(),
		httpServer: &http.Server{
			Addr:              net.JoinHostPort(h.Address, strconv.Itoa(h.Port)),
			ReadHeaderTimeout: h.ReadHeaderTimeout,
			ReadTimeout:       h.ReadTimeout,
			WriteTimeout:      h.WriteTimeout,
			IdleTimeout:       h.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(h.Log.Handler(), slog.LevelWarn),
		},
		log:  h.Log,
		errs: make(chan error, 1),
	}
}

// Use appends middlewares to the global chain wrapping every route, including
// unmatched requests. The first middleware is the outermost. Call it before Start.
func (s *HTTPServer) Use(middlewares ...middleware.Middleware) {
	s.middlewares = append(s.middlewares, middlewares...)
}

// Start binds the listen address synchronously, so errors such as a port
// already in use are returned to the caller, and then serves requests in the
// background. A fatal error from the serve loop is reported on Errors().
func (s *HTTPServer) Start() error {
	s.httpServer.Handler = middleware.Chain(s.mux, s.middlewares...)

	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.httpServer.Addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("Server error", "error", err)
			s.errs <- err
		}
	}()

	s.log.Info("HTTP server started",
		"address", ln.Addr().String(),
		"read_header_timeout", s.httpServer.ReadHeaderTimeout,
		"read_timeout", s.httpServer.ReadTimeout,
		"write_timeout", s.httpServer.WriteTimeout,
		"idle_timeout", s.httpServer.IdleTimeout,
	)
	return nil
}

// Addr returns the address the server is bound to, or nil before Start.
// With port 0 it reports the actual ephemeral port.
func (s *HTTPServer) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Errors returns a channel that receives a fatal error from the serve loop.
// It never receives http.ErrServerClosed.
func (s *HTTPServer) Errors() <-chan error {
	return s.errs
}

// Stop gracefully shuts down the server, waiting for in-flight requests until ctx expires.
func (s *HTTPServer) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Handle registers a handler for the pattern.
func (s *HTTPServer) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

// HandleFunc registers a handler function for the pattern.
func (s *HTTPServer) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	s.mux.HandleFunc(pattern, handler)
}

// HandleWithMiddleware registers a handler wrapped by route-specific
// middleware (first is outermost), inside the global chain.
func (s *HTTPServer) HandleWithMiddleware(pattern string, handler http.Handler, middlewares ...middleware.Middleware) {
	s.Handle(pattern, middleware.Chain(handler, middlewares...))
}

// HandleFuncWithMiddleware registers a handler function wrapped by
// route-specific middleware (first is outermost), inside the global chain.
func (s *HTTPServer) HandleFuncWithMiddleware(pattern string, handler func(http.ResponseWriter, *http.Request), middlewares ...middleware.Middleware) {
	s.Handle(pattern, middleware.Chain(http.HandlerFunc(handler), middlewares...))
}
