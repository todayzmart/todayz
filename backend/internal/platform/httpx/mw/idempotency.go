package mw

// Slot 14 · idempotency · owner W0-03. Inline on every route, after validate (D-100). The logic lives in
// platform/idempotency; a route opts in where it is registered with
// route.Handle(route.Policy{Idempotency: …}, h) (04 §1.6).

import (
	"net/http"

	"github.com/todayz/backend/internal/platform/idempotency"
)

// Idempotency replays a stored response for a repeated Idempotency-Key.
func Idempotency(o idempotency.Options) func(http.Handler) http.Handler {
	if o.Store == nil {
		// No store (unit tests of other slots): pass through.
		return func(next http.Handler) http.Handler { return next }
	}
	return idempotency.Middleware(o)
}
