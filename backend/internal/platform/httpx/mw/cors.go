package mw

// Slot 7 · cors · owner W0-03.

import (
	"net/http"
	"path"
	"slices"
	"strings"
)

const (
	corsAllowHeaders  = "Authorization, Content-Type, Idempotency-Key, If-Match, X-Request-Id, traceparent, tracestate"
	corsExposeHeaders = "X-Request-Id, Idempotent-Replayed, Retry-After, ETag, Location"
	corsMethods       = "GET, POST, PUT, PATCH, DELETE"

	// The marketing site's rule for /v1/public/* (D-102): no credentials,
	// POST only, the two headers the lead form sends.
	publicPathPrefix        = "/v1/public/"
	publicCORSAllowHeaders  = "Content-Type, Idempotency-Key"
	publicCORSExposeHeaders = "X-Request-Id, Idempotent-Replayed, Retry-After"
	publicCORSMethods       = "POST, OPTIONS"
)

// CORS allows exactly the given origins (02 §2.4 step 6) with credentials, so
// the refresh cookie reaches /v1/auth (04 §1, D-14). publicOrigins (the
// marketing site) may call /v1/public/* only, POST without credentials, and
// get no CORS headers anywhere else (D-102). Any other origin gets no CORS
// headers and the browser blocks it. Preflights end here with 204.
func CORS(origins, publicOrigins []string) func(http.Handler) http.Handler {
	allowed := slices.Clone(origins)
	public := slices.Clone(publicOrigins)
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
			// The cleaned path, so /v1/public/../auth/refresh is not public.
			publicOK := !ok && slices.Contains(public, origin) && strings.HasPrefix(path.Clean(r.URL.Path), publicPathPrefix)
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				switch {
				case ok:
					h.Set("Access-Control-Allow-Origin", origin)
					h.Set("Access-Control-Allow-Credentials", "true")
					h.Set("Access-Control-Allow-Methods", corsMethods)
					h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
					h.Set("Access-Control-Max-Age", "600")
				case publicOK:
					h.Set("Access-Control-Allow-Origin", origin)
					h.Set("Access-Control-Allow-Methods", publicCORSMethods)
					h.Set("Access-Control-Allow-Headers", publicCORSAllowHeaders)
					h.Set("Access-Control-Max-Age", "600")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			switch {
			case ok:
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
			case publicOK && r.Method == http.MethodPost:
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Expose-Headers", publicCORSExposeHeaders)
			}
			next.ServeHTTP(w, r)
		})
	}
}
