package mw

// Slot 2 · secheaders · owner W0-03.

import (
	"net/http"
	"strings"
)

// Security headers on every API response (the API serves JSON only).
const (
	ContentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'"
	ReferrerPolicy        = "no-referrer"
	// DefaultCacheControl is set on /v1 responses whose handler set none.
	DefaultCacheControl = "no-store"
)

// SecHeaders sets X-Content-Type-Options, Referrer-Policy and a deny-all
// Content-Security-Policy on every response, and Cache-Control: no-store on
// /v1 responses unless the handler set its own (it is added when the
// response is written, so a handler's choice wins).
func SecHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", ReferrerPolicy)
			h.Set("Content-Security-Policy", ContentSecurityPolicy)
			if r.URL.Path == "/v1" || strings.HasPrefix(r.URL.Path, "/v1/") {
				w = &cacheDefaultWriter{ResponseWriter: w}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type cacheDefaultWriter struct {
	http.ResponseWriter
	done bool
}

func (c *cacheDefaultWriter) apply() {
	if !c.done {
		c.done = true
		if c.Header().Get("Cache-Control") == "" {
			c.Header().Set("Cache-Control", DefaultCacheControl)
		}
	}
}

func (c *cacheDefaultWriter) WriteHeader(code int) {
	c.apply()
	c.ResponseWriter.WriteHeader(code)
}

func (c *cacheDefaultWriter) Write(b []byte) (int, error) {
	c.apply()
	return c.ResponseWriter.Write(b)
}

func (c *cacheDefaultWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
