package mw

// Slot 9 · routepolicy · owner W0-03.

import (
	"net/http"

	"github.com/todayz/backend/internal/platform/httpx/route"
)

// RoutePolicy puts the matched route's policy (route.Handle at registration)
// in the request context for bodylimit and idempotency, which run before chi
// picks the route. A nil table gives every request the zero policy.
func RoutePolicy(t *route.Table) func(http.Handler) http.Handler {
	if t == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return t.Middleware
}
