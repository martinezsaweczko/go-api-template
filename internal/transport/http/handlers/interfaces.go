// Package handlers implements the HTTP endpoints. Each handler is built from
// a *Config struct and registers its own routes on a Router.
package handlers

import (
	"net/http"

	"github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"
)

// Router is the route registration surface handlers need from the server.
type Router interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
	HandleWithMiddleware(pattern string, handler http.Handler, middlewares ...middleware.Middleware)
	HandleFuncWithMiddleware(pattern string, handler func(http.ResponseWriter, *http.Request), middlewares ...middleware.Middleware)
}
