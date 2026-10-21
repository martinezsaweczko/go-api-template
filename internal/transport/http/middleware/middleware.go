// Package middleware provides composable net/http middleware: tracing,
// metrics, recovery, request IDs, access logging, CORS, request body limits
// and JWT authorization. Every middleware has the shape func(http.Handler) http.Handler.
package middleware

import (
	"encoding/json"
	"net/http"
)

// Middleware defines the middleware function signature
// It takes a handler and returns a wrapped handler
type Middleware func(http.Handler) http.Handler

// Chain applies multiple middlewares to a handler in order.
// The first middleware is the outermost layer: Chain(h, a, b) serves a -> b -> h.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// HandlerFunc is a convenience function to wrap http.HandlerFunc with middleware
func HandlerFunc(handler http.HandlerFunc, middlewares ...Middleware) http.HandlerFunc {
	wrapped := Chain(handler, middlewares...)
	return func(w http.ResponseWriter, r *http.Request) {
		wrapped.ServeHTTP(w, r)
	}
}

// writeJSONError writes {"error": message} with the given status code.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
