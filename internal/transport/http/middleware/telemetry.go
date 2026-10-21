package middleware

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
)

// UnmatchedRoute is the http.route label for requests no route matched, so
// unknown paths cannot inflate metric cardinality.
const UnmatchedRoute = "unmatched"

const instrumentationName = "github.com/martinezsaweczko/go-api-template/internal/transport/http/middleware"

// durationBuckets are the semantic-convention boundaries (seconds) for
// http.server.request.duration.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// routeInfo carries the matched route pattern from RouteTagger, which runs
// inside the router, back out to the telemetry middleware wrapping it.
type routeInfo struct {
	route string
}

type routeInfoKey struct{}

// withRouteInfo returns r with a routeInfo in its context, reusing one that
// an outer middleware already installed.
func withRouteInfo(r *http.Request) (*http.Request, *routeInfo) {
	if info, ok := r.Context().Value(routeInfoKey{}).(*routeInfo); ok {
		return r, info
	}
	info := &routeInfo{}
	return r.WithContext(context.WithValue(r.Context(), routeInfoKey{}, info)), info
}

// label returns the matched route, or UnmatchedRoute when none matched.
func (i *routeInfo) label() string {
	if i.route == "" {
		return UnmatchedRoute
	}
	return i.route
}

// RouteTagger wraps the router (normally an *http.ServeMux) and reports the
// matched route pattern to Tracing and Metrics. Go 1.22+ ServeMux sets
// r.Pattern only on the request it receives, which outer middleware never
// sees, so the pattern is published through the request context instead. It
// also renames the active server span to "METHOD /route" and sets http.route.
func RouteTagger(router http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r)

		route := routeFromPattern(r.Pattern)
		if route == "" {
			return
		}
		if info, ok := r.Context().Value(routeInfoKey{}).(*routeInfo); ok {
			info.route = route
		}
		if span := trace.SpanFromContext(r.Context()); span.IsRecording() {
			span.SetName(r.Method + " " + route)
			span.SetAttributes(semconv.HTTPRoute(route))
		}
	})
}

// routeFromPattern strips the optional method and host from a ServeMux
// pattern: "GET example.com/items/{id}" becomes "/items/{id}".
func routeFromPattern(pattern string) string {
	if _, rest, ok := strings.Cut(pattern, " "); ok {
		pattern = strings.TrimLeft(rest, " \t")
	}
	if i := strings.IndexByte(pattern, '/'); i > 0 {
		pattern = pattern[i:]
	}
	return pattern
}

// Tracing starts a server span for every request with otelhttp, continuing
// any trace propagated by the caller, and puts it in the request context so
// handler spans become its children. The span is named after the HTTP
// method and renamed to "METHOD /route" by RouteTagger once a route matches.
// Requests to excludedPaths (health probes, metrics scrapes) are not traced.
// A nil tp or prop falls back to the global provider or propagator.
// otelhttp's own metrics are disabled: Metrics owns HTTP metrics.
func Tracing(tp trace.TracerProvider, prop propagation.TextMapPropagator, excludedPaths ...string) Middleware {
	return func(next http.Handler) http.Handler {
		inner := otelhttp.NewHandler(next, "http.server",
			otelhttp.WithTracerProvider(tp),
			otelhttp.WithPropagators(prop),
			otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method }),
			otelhttp.WithFilter(func(r *http.Request) bool { return !slices.Contains(excludedPaths, r.URL.Path) }),
		)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, _ = withRouteInfo(r)
			inner.ServeHTTP(w, r)
		})
	}
}

// Metrics records, for every request, the http.server.requests counter and
// the http.server.request.duration histogram (seconds), both labelled with
// http.request.method, http.route and http.response.status_code. Routes come
// from RouteTagger; unmatched requests use UnmatchedRoute.
func Metrics(mp metric.MeterProvider) (Middleware, error) {
	meter := mp.Meter(instrumentationName)
	requests, err := meter.Int64Counter("http.server.requests",
		metric.WithDescription("Number of HTTP requests served."),
		metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("create request counter: %w", err)
	}
	duration, err := meter.Float64Histogram("http.server.request.duration",
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...))
	if err != nil {
		return nil, fmt.Errorf("create duration histogram: %w", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			r, info := withRouteInfo(r)
			rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rec, r)

			attrs := metric.WithAttributeSet(attribute.NewSet(
				semconv.HTTPRequestMethodKey.String(normalizeMethod(r.Method)),
				semconv.HTTPRoute(info.label()),
				semconv.HTTPResponseStatusCode(rec.statusCode),
			))
			// Record on a context detached from cancellation so a client
			// disconnect cannot drop the measurement.
			ctx := context.WithoutCancel(r.Context())
			requests.Add(ctx, 1, attrs)
			duration.Record(ctx, time.Since(start).Seconds(), attrs)
		})
	}, nil
}

// normalizeMethod maps non-standard methods to "_OTHER", as the semantic
// conventions require, so arbitrary methods cannot inflate cardinality.
func normalizeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	}
	return "_OTHER"
}
