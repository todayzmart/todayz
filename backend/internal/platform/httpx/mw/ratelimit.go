package mw

// Slot 11 · ratelimit · owner W1-01. No-op until W1-01 adds the per-principal
// or per-IP token buckets (02 §2.3) and 429 rate_limited.

import "net/http"

// RateLimit limits requests per principal or IP per route group.
func RateLimit() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
