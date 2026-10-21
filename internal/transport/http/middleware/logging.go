package middleware

import (
	"log/slog"
	"net/http"
	"slices"
	"time"
)

// Logging logs every request with its method, path, status, size, duration
// and request ID. Requests to quietPaths (health probes, metrics scrapes) are
// logged at Debug instead of Info so they do not flood the logs.
func Logging(log *slog.Logger, quietPaths ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			level := slog.LevelInfo
			if slices.Contains(quietPaths, r.URL.Path) {
				level = slog.LevelDebug
			}
			log.Log(r.Context(), level, "HTTP request",
				"method", r.Method,
				"path", r.URL.Path,
				"status_code", wrapped.statusCode,
				"response_size", wrapped.written,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
				"request_id", RequestIDFromContext(r.Context()),
			)
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture the status code and
// response size. It exposes Unwrap so http.ResponseController reaches the
// underlying writer, and passes Flush through for streaming handlers.
type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	written     int64
	wroteHeader bool
}

func (w *statusRecorder) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		w.statusCode = statusCode
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	w.wroteHeader = true
	n, err := w.ResponseWriter.Write(b)
	w.written += int64(n)
	return n, err
}

// Flush implements http.Flusher when the underlying writer supports it.
func (w *statusRecorder) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap returns the underlying writer for http.ResponseController.
func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

var (
	_ http.ResponseWriter = (*statusRecorder)(nil)
	_ http.Flusher        = (*statusRecorder)(nil)
)
