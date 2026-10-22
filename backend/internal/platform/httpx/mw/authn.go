package mw

// Slot 10 · authn · owner W1-01. No-op until W1-01: it will read the Bearer
// access token and store the principal with reqctx.WithPrincipal. Until then
// reqctx.PrincipalFrom returns reqctx.Anonymous for every request.

import "net/http"

// Authn resolves the caller.
func Authn() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
