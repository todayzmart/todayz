package mw

// Slot 4 · otel · owner W1-07. The server span: the client's traceparent is
// the parent, the span is renamed to `METHOD route-template` once chi has
// routed (so the name never carries an id), and the request context carries
// the span so logs, SQL spans and exemplars share its trace id (05 §2.3).

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// OTel starts the server span on tp (the global provider when nil) and reads
// the propagator from prop (the global one when nil).
func OTel(tp trace.TracerProvider, prop propagation.TextMapPropagator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t, p := tp, prop
			if t == nil {
				t = otel.GetTracerProvider()
			}
			if p == nil {
				p = otel.GetTextMapPropagator()
			}
			ctx := p.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := t.Tracer(telemetry.MeterName).Start(ctx, "HTTP "+r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(attribute.String("http.request.method", r.Method)))
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			finish := func(status int) {
				route := telemetry.RouteLabel("")
				if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
					route = rc.RoutePattern()
				}
				span.SetName(r.Method + " " + route)
				span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
				if status >= 500 {
					span.SetStatus(codes.Error, "")
				}
				span.End()
			}
			defer func() {
				if rec := recover(); rec != nil { // recover (slot 3) is outside; end the span, then re-panic
					finish(http.StatusInternalServerError)
					panic(rec)
				}
			}()
			next.ServeHTTP(sw, r.WithContext(ctx))
			finish(sw.status)
		})
	}
}
