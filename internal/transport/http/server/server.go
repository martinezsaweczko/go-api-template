package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// Server wraps the standard net/http server implementation
type HTTPServerConfig struct {
	Addr         string
	Port         int
	Timeout      int
	ReadTimeout  int
	WriteTimeout int
	Log          *slog.Logger
}

type HTTPServer struct {
	httpServer *http.Server
	mux        *http.ServeMux
	log        *slog.Logger
	errs       chan error
}

// New creates a new HTTP server on the specified port
func (h *HTTPServerConfig) New() *HTTPServer {
	addr := fmt.Sprintf("%s:%d", h.Addr, h.Port)
	mux := http.NewServeMux()

	return &HTTPServer{
		mux: mux,
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  time.Duration(h.ReadTimeout) * time.Second,
			WriteTimeout: time.Duration(h.WriteTimeout) * time.Second,
			IdleTimeout:  time.Duration(h.Timeout) * time.Second,
		},
		log:  h.Log,
		errs: make(chan error, 1),
	}
}

// Start binds the listen address synchronously, so errors such as a port
// already in use are returned to the caller, and then serves requests in the
// background. A fatal error from the serve loop is reported on Errors().
func (s *HTTPServer) Start() error {
	s.log.Info("Starting HTTP server", "address", s.httpServer.Addr, "timeout", s.httpServer.IdleTimeout, "read_timeout", s.httpServer.ReadTimeout, "write_timeout", s.httpServer.WriteTimeout)

	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.httpServer.Addr, err)
	}

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("Server error", "error", err)
			s.errs <- err
		}
	}()

	s.log.Info("HTTP server started", "address", ln.Addr().String())
	return nil
}

// Errors returns a channel that receives a fatal error from the serve loop.
// It never receives http.ErrServerClosed.
func (s *HTTPServer) Errors() <-chan error {
	return s.errs
}

// Stop gracefully shuts down the server with context timeout
func (s *HTTPServer) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// Handle implements the Server interface for routing
func (s *HTTPServer) Handle(pattern string, handler http.Handler) {
	if mux, ok := s.httpServer.Handler.(*http.ServeMux); ok {
		mux.Handle(pattern, handler)
	}
}

// HandleFunc implements the Server interface for routing functions
func (s *HTTPServer) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if mux, ok := s.httpServer.Handler.(*http.ServeMux); ok {
		mux.HandleFunc(pattern, handler)
	}
}

// HandleWithMiddleware registers a handler with specific middleware
// The middleware will be applied in order (first to last)
// Example: server.HandleWithMiddleware("/api/users", handler, loggingMiddleware, authMiddleware)
func (s *HTTPServer) HandleWithMiddleware(pattern string, handler http.Handler, middlewares ...middleware.Middleware) {
	wrapped := middleware.Chain(handler, middlewares...)
	s.Handle(pattern, wrapped)
}

// HandleFuncWithMiddleware registers a handler function with specific middleware
// The middleware will be applied in order (first to last)
// Example: server.HandleFuncWithMiddleware("/api/version", versionHandler, loggingMiddleware)
func (s *HTTPServer) HandleFuncWithMiddleware(pattern string, handler func(http.ResponseWriter, *http.Request), middlewares ...middleware.Middleware) {
	wrapped := middleware.Chain(http.HandlerFunc(handler), middlewares...)
	s.Handle(pattern, wrapped)
}
