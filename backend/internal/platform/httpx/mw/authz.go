package mw

// Slot 15 · authz · owner W1-01 (02 §3.3). Runs after chi has routed and the
// validator (13) and idempotency (14) have run, so the path params are known.
//
// A domain's generated strict server gets it through authz.Strict (one action
// per operationId, fail closed). A hand-mounted route uses
// r.With(mw.Authz(engine, "order.accept")). Either way the handler reads the
// Decision with authz.DecisionFrom(ctx) to apply a conditional grant.

import (
	"net/http"

	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// Authz checks that the principal may perform action on the route's scope
// (businessId / storeId path params). A nil authorizer refuses (500).
func Authz(a authz.Authorizer, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, err := authz.Authorize(a, r, action)
			if err != nil {
				problem.Write(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
