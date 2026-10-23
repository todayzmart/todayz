package authz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// AdminGate is the role check for admin routes that are not (yet) rows of a
// policy table: "an admin holding one of these roles, and a fresh step-up
// when asked" (02 §3.2–3.3, D-64). It reads the principal slot 11 put in the
// context, whose roles and StepUpAt come from the live session (admin_users
// and the sign-in's last TOTP verification), so a role change, a disabled
// admin or an expired step-up takes effect on the next request.
//
//	gate := authz.NewAdminGate(clk)
//	p, err := gate.Require(ctx, []authz.Role{authz.Admin}, false)
//	r.With(gate.Middleware([]authz.Role{authz.SuperAdmin}, true)).Post(...)
//
// Errors are problems, never written by Require:
//
//   - no principal: 401 unauthorized;
//   - not an admin-app principal, or an admin without amr otp+totp: 403
//     forbidden;
//   - an admin holding none of anyOf (super_admin holds every role): 403
//     forbidden;
//   - stepUp and no TOTP verification within authn.StepUpWindow (10 min):
//     403 step_up_required, and the client calls POST /v1/auth/totp/verify
//     {purpose: step_up} and retries;
//   - anyOf empty or naming a role that is not a D-64 admin role: 500 (a
//     programming error; fail closed).
//
// Prefer a policy table action (KindAdmin, StepUp) checked by slot 15 for a
// domain's own admin routes: it is in the golden matrix (AU-14). The gate is
// for seams that take a role list, such as W1-11's adminapi.Gate.
type AdminGate struct {
	clock clock.Clock
}

// NewAdminGate returns a gate on clk (the step-up window is measured with it).
func NewAdminGate(clk clock.Clock) *AdminGate { return &AdminGate{clock: clk} }

var errGateNoRoles = errors.New("authz: admin gate needs at least one role")

// Require checks the request's principal and returns it on success. The
// returned principal's Roles are every role the admin holds (handlers narrow
// reads by role).
func (g *AdminGate) Require(ctx context.Context, anyOf []Role, stepUp bool) (authn.Principal, error) {
	if g == nil || g.clock == nil {
		return authn.Principal{}, problem.Wrap(genproblems.Internal, errors.New("authz: admin gate without a clock"))
	}
	if len(anyOf) == 0 {
		return authn.Principal{}, problem.Wrap(genproblems.Internal, errGateNoRoles)
	}
	for _, r := range anyOf {
		if !IsAdminRole(string(r)) {
			return authn.Principal{}, problem.Wrap(genproblems.Internal, fmt.Errorf("authz: admin gate: %q is not an admin role", r))
		}
	}
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	if p.App != authn.AppAdmin || p.AMR != authn.AMROTPTOTP {
		return authn.Principal{}, refused()
	}
	if !holdsAny(p.Roles, anyOf) {
		return authn.Principal{}, refused()
	}
	if stepUp {
		at := p.StepUpAt
		if at.IsZero() || g.clock.Now().Sub(at) > authn.StepUpWindow {
			return authn.Principal{}, problem.New(genproblems.StepUpRequired, "")
		}
	}
	return p, nil
}

// Middleware is Require for a hand-mounted route: a refusal is written as
// problem+json and the handler never runs.
func (g *AdminGate) Middleware(anyOf []Role, stepUp bool) func(http.Handler) http.Handler {
	roles := slices.Clone(anyOf)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := g.Require(r.Context(), roles, stepUp); err != nil {
				problem.Write(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// holdsAny reports whether held contains one of want; super_admin holds
// every role.
func holdsAny(held []string, want []Role) bool {
	if slices.Contains(held, string(SuperAdmin)) {
		return true
	}
	for _, r := range want {
		if slices.Contains(held, string(r)) {
			return true
		}
	}
	return false
}
