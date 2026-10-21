package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// Log attribute keys used to correlate log records with traces.
const (
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// TraceHandler is a slog.Handler that adds trace_id and span_id attributes to
// every record whose context carries a valid span context, so logs written
// with the *Context methods (InfoContext, ErrorContext, Log) can be joined
// with their traces.
type TraceHandler struct {
	next slog.Handler
}

// NewTraceHandler wraps next with trace correlation.
func NewTraceHandler(next slog.Handler) *TraceHandler {
	return &TraceHandler{next: next}
}

// Enabled reports whether the wrapped handler handles records at level.
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds the trace correlation attributes, when present, and delegates
// to the wrapped handler.
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r = r.Clone()
		r.AddAttrs(
			slog.String(TraceIDKey, sc.TraceID().String()),
			slog.String(SpanIDKey, sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs returns a TraceHandler wrapping next.WithAttrs(attrs).
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a TraceHandler wrapping next.WithGroup(name). Within a
// group the correlation attributes are nested under that group.
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{next: h.next.WithGroup(name)}
}
