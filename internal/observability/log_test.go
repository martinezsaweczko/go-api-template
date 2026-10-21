package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/martinezsaweczko/go-api-template/internal/observability"
)

func decodeLine(t *testing.T, b *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(b.Bytes(), &entry); err != nil {
		t.Fatalf("decode log line %q: %v", b.String(), err)
	}
	b.Reset()
	return entry
}

func TestTraceHandlerAddsTraceCorrelation(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9},
		SpanID:     trace.SpanID{0x00, 0xf0},
		TraceFlags: trace.FlagsSampled,
	})
	traced := trace.ContextWithSpanContext(context.Background(), sc)

	var buf bytes.Buffer
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(&buf, nil)))

	tests := []struct {
		name      string
		log       func()
		wantTrace bool
		wantAttr  string
		group     string
	}{
		{name: "context with span", log: func() { log.InfoContext(traced, "hello") }, wantTrace: true},
		{name: "context without span", log: func() { log.InfoContext(context.Background(), "hello") }},
		{name: "derived logger keeps correlation", log: func() { log.With("k", "v").InfoContext(traced, "hello") }, wantTrace: true, wantAttr: "k"},
		// Attributes added to a record land in the logger's open group.
		{name: "group nests correlation", log: func() { log.WithGroup("g").InfoContext(traced, "hello", "k", "v") }, wantTrace: true, group: "g"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.log()
			entry := decodeLine(t, &buf)
			if tt.wantAttr != "" {
				if _, ok := entry[tt.wantAttr]; !ok {
					t.Errorf("entry %v lost attribute %q", entry, tt.wantAttr)
				}
			}
			if tt.group != "" {
				nested, ok := entry[tt.group].(map[string]any)
				if !ok {
					t.Fatalf("entry %v has no group %q", entry, tt.group)
				}
				entry = nested
			}
			traceID, hasTrace := entry["trace_id"]
			spanID, hasSpan := entry["span_id"]
			if hasTrace != tt.wantTrace || hasSpan != tt.wantTrace {
				t.Fatalf("entry %v: trace_id present %v, span_id present %v, want %v", entry, hasTrace, hasSpan, tt.wantTrace)
			}
			if tt.wantTrace && (traceID != sc.TraceID().String() || spanID != sc.SpanID().String()) {
				t.Errorf("trace_id/span_id = %v/%v, want %s/%s", traceID, spanID, sc.TraceID(), sc.SpanID())
			}
		})
	}
}

func TestTraceHandlerHonoursLevel(t *testing.T) {
	h := observability.NewTraceHandler(slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(info) = true, want the wrapped handler's level to apply")
	}
}
