package middleware

import "net/http"

// BodyLimit caps every request body at maxBytes using http.MaxBytesReader.
// Reading past the limit fails with *http.MaxBytesError; handlers should map
// it to 413 Request Entity Too Large.
func BodyLimit(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
