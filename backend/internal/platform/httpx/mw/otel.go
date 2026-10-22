package mw

// Slot 4 · otel · owner W1-07. No-op until W1-07 adds the server span
// (otelhttp, traceparent from the client, route template as the span name).

import "net/http"

// OTel starts the server span.
func OTel() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
