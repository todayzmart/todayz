package authz

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Public marks an operation that needs no permission check (a `security: []`
// operation such as POST /v1/auth/otp/send, or refresh and logout, which
// authenticate by their cookie inside the handler). It is written in the
// operation map explicitly so that forgetting an operation fails closed.
const Public = ""

// Authorizer is the check Strict and mw.Authz run (Engine implements it).
type Authorizer interface {
	Check(ctx context.Context, p authn.Principal, action string, s Scope) (Decision, error)
}

// ScopeFrom reads the scope from chi's URL params `bid` and `sid`
// (02 §3.1 names them so in every /v1/biz path). A value that is present but
// not a ULID is a 404, never a scope-less check.
func ScopeFrom(r *http.Request) (Scope, error) {
	var s Scope
	for name, dst := range map[string]*ids.ID{"bid": &s.BusinessID, "sid": &s.StoreID} {
		v := chi.URLParam(r, name)
		if v == "" {
			continue
		}
		id, err := ids.Parse(v)
		if err != nil {
			return Scope{}, problem.New(genproblems.NotFound, "")
		}
		*dst = id
	}
	return s, nil
}

// Authorize runs the check for one request: the principal from authn, the
// scope from the path. On success the returned context carries the Decision.
func Authorize(a Authorizer, r *http.Request, action string) (context.Context, error) {
	ctx := r.Context()
	if a == nil {
		return ctx, problem.Wrap(genproblems.Internal, fmt.Errorf("authz: no authorizer for %s", action))
	}
	s, err := ScopeFrom(r)
	if err != nil {
		return ctx, err
	}
	p, _ := authn.FromContext(ctx)
	d, err := a.Check(ctx, p, action, s)
	if err != nil {
		return ctx, err
	}
	notePrincipal(ctx, p, d)
	return WithDecision(ctx, d), nil
}

// notePrincipal gives the access log the caller's kind, role and store (05;
// never the user id).
func notePrincipal(ctx context.Context, p authn.Principal, d Decision) {
	kind := "anonymous"
	if !p.IsAnonymous() {
		kind = p.App.Kind()
	}
	store := ""
	if d.Scope.StoreID != ids.Zero {
		store = d.Scope.StoreID.String()
	}
	telemetry.NotePrincipal(ctx, kind, string(d.Role), store)
}

// Strict returns slot 15 for one domain's generated strict server. ops maps
// every operationId of the domain to its action, or to Public. An operation
// missing from ops is refused with 500 (fail closed; the domain's route test
// catches it before it ships). A domain wires it as
//
//	authapi.Register(r, h, authapi.StrictMiddlewareFunc(authz.Strict[authapi.StrictHandlerFunc](e, ops)))
//
// The handler error is a problem; the generated strict server writes it
// through openapi.StrictOptions.
func Strict[H ~func(context.Context, http.ResponseWriter, *http.Request, any) (any, error)](a Authorizer, ops map[string]string) func(H, string) H {
	return func(next H, operationID string) H {
		action, known := ops[operationID]
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if !known {
				return nil, problem.Wrap(genproblems.Internal, fmt.Errorf("authz: operation %s has no action", operationID))
			}
			if action == Public {
				return next(ctx, w, r, req)
			}
			actx, err := Authorize(a, r.WithContext(ctx), action)
			if err != nil {
				return nil, err
			}
			return next(actx, w, r.WithContext(actx), req)
		}
	}
}
