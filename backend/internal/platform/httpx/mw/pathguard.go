package mw

// Slot 8 · pathguard · owner W0-04 (D-100, security review H-1).

import (
	"net/http"
	"strings"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// PathGuard refuses, with 400 bad_request, a request path that two routers
// could read differently: a raw path that is not the canonical encoding of
// its decoded form (an unescaped quote, a bare non-ASCII byte, a stray %), an
// encoded slash (%2F) or an encoded or bare backslash (%5C). chi routes on the
// raw path and decodes parameters afterwards; without this guard a path like
// /v1/me/orders/a%2F" matched one route for chi and another, or none, for the
// contract validator. Everything after the guard sees one path.
func PathGuard() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !pathIsCanonical(r) {
				problem.Write(w, r, problem.New(genproblems.BadRequest, "The request path is not valid"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// pathIsCanonical is the rule PathGuard applies; exported through the
// middleware only.
func pathIsCanonical(r *http.Request) bool {
	u := r.URL
	if u.RawPath != "" && u.RawPath != u.EscapedPath() {
		return false
	}
	raw := u.RawPath
	if raw == "" {
		raw = u.Path
	}
	upper := strings.ToUpper(raw)
	if strings.Contains(upper, "%2F") || strings.Contains(upper, "%5C") {
		return false
	}
	return !strings.Contains(u.Path, `\`)
}
