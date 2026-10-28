package standee

import (
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	standeeapi "github.com/todayz/backend/gen/api/standee"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps every standee operation to the action the engine checks (slot 15)
// in the path's business and store.
var Ops = map[string]string{
	"ListStandees":          policy.StandeeManage,
	"CreateStandee":         policy.StandeeManage,
	"RequestStandeeReorder": policy.StandeeManage,
	"MarkStoreSheetPrinted": policy.StandeeManage,
}

// Register mounts the standee routes on r. az is the permission engine; it
// may be nil only in a route-only test, where every call is then refused.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	standeeapi.Register(r, handlers{s},
		standeeapi.StrictMiddlewareFunc(authz.Strict[standeeapi.StrictHandlerFunc](az, Ops)))
}

type handlers struct{ s *Service }

var _ standeeapi.StrictServerInterface = handlers{}

func (h handlers) ListStandees(ctx context.Context, req standeeapi.ListStandeesRequestObject) (standeeapi.ListStandeesResponseObject, error) {
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	l, err := h.s.List(ctx, store)
	if err != nil {
		return nil, asProblem(err)
	}
	return standeeapi.ListStandees200JSONResponse(l), nil
}

func (h handlers) CreateStandee(ctx context.Context, req standeeapi.CreateStandeeRequestObject) (standeeapi.CreateStandeeResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	st, err := h.s.Create(ctx, p, store, req.Body.Campaign)
	if err != nil {
		return nil, asProblem(err)
	}
	return standeeapi.CreateStandee201JSONResponse(st), nil
}

func (h handlers) RequestStandeeReorder(ctx context.Context, req standeeapi.RequestStandeeReorderRequestObject) (standeeapi.RequestStandeeReorderResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	standee, err := pathID(req.StandeeId)
	if err != nil {
		return nil, err
	}
	st, err := h.s.RequestReorder(ctx, p, store, standee)
	if err != nil {
		return nil, asProblem(err)
	}
	return standeeapi.RequestStandeeReorder200JSONResponse(st), nil
}

func (h handlers) MarkStoreSheetPrinted(ctx context.Context, req standeeapi.MarkStoreSheetPrintedRequestObject) (standeeapi.MarkStoreSheetPrintedResponseObject, error) {
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	if err := h.s.MarkSheetPrinted(ctx, store); err != nil {
		return nil, asProblem(err)
	}
	return standeeapi.MarkStoreSheetPrinted204Response{}, nil
}

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, problem.New(genproblems.NotFound, "")
	}
	return id, nil
}

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}
