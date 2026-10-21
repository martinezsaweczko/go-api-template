package middleware

import (
	"net/http"
	"slices"
)

const (
	corsAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsAllowHeaders = "Authorization, Content-Type, " + RequestIDHeader
	corsMaxAge       = "600"
)

// CORS answers preflight requests and sets CORS headers for the allowed
// origins. "*" allows any origin. With no origins it is a no-op, so CORS is
// disabled and OPTIONS requests reach the router unchanged.
func CORS(allowedOrigins []string) Middleware {
	if len(allowedOrigins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	wildcard := slices.Contains(allowedOrigins, "*")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			preflight := r.Method == http.MethodOptions && origin != "" &&
				r.Header.Get("Access-Control-Request-Method") != ""

			if origin != "" {
				w.Header().Add("Vary", "Origin")
				switch {
				case wildcard:
					w.Header().Set("Access-Control-Allow-Origin", "*")
				case slices.Contains(allowedOrigins, origin):
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
				w.Header().Set("Access-Control-Expose-Headers", RequestIDHeader)
			}

			if !preflight {
				next.ServeHTTP(w, r)
				return
			}

			// Preflight: answer directly. A disallowed origin gets no
			// Access-Control-Allow-Origin, so the browser blocks the request.
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				w.Header().Set("Access-Control-Allow-Methods", corsAllowMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
				w.Header().Set("Access-Control-Max-Age", corsMaxAge)
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
}
