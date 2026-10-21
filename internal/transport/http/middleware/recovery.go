package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recovery converts a panic in a downstream handler into a 500 JSON response
// and logs the panic value with its stack trace. If the handler had already
// started the response, a second status and body cannot be sent: the panic is
// logged and re-raised as http.ErrAbortHandler so net/http aborts the
// connection and the client sees a truncated response instead of a corrupted
// one. http.ErrAbortHandler from the handler itself is re-panicked unchanged.
func Recovery(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				log.ErrorContext(r.Context(), "Panic while serving request",
					"panic", fmt.Sprint(v),
					"stack", string(debug.Stack()),
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", RequestIDFromContext(r.Context()),
					"response_started", rec.wroteHeader,
				)
				if rec.wroteHeader {
					panic(http.ErrAbortHandler)
				}
				writeJSONError(w, http.StatusInternalServerError, "internal server error")
			}()
			next.ServeHTTP(rec, r)
		})
	}
}
