package mw

// Slot 14 · authz · owner W1-01. Per route: r.With(mw.Authz("orders.accept")).
// No-op until W1-01 adds the policy check (02 §3: route action + path scope).

import "net/http"

// Authz checks that the principal may perform action on the route's scope.
func Authz(action string) func(http.Handler) http.Handler {
	_ = action
	return func(next http.Handler) http.Handler { return next }
}
