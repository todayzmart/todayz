package mw

// Slot 7 · cors · owner W0-03.

import (
	"net/http"
	"slices"
)

const (
	corsAllowHeaders  = "Authorization, Content-Type, Idempotency-Key, If-Match, X-Request-Id, traceparent, tracestate"
	corsExposeHeaders = "X-Request-Id, Idempotent-Replayed, Retry-After, ETag, Location"
	corsMethods       = "GET, POST, PUT, PATCH, DELETE"
)

// CORS allows exactly the given origins (02 §2.4 step 6) with credentials, so
// the refresh cookie reaches /v1/auth (04 §1, D-14). Any other origin gets no
// CORS headers and the browser blocks it. Preflights end here with 204.
func CORS(origins []string) func(http.Handler) http.Handler {
	allowed := slices.Clone(origins)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Add("Vary", "Origin")
			ok := slices.Contains(allowed, origin)
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				if ok {
					h.Set("Access-Control-Allow-Origin", origin)
					h.Set("Access-Control-Allow-Credentials", "true")
					h.Set("Access-Control-Allow-Methods", corsMethods)
					h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
					h.Set("Access-Control-Max-Age", "600")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if ok {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
			}
			next.ServeHTTP(w, r)
		})
	}
}
