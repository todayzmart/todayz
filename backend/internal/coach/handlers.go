package coach

import (
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	coachapi "github.com/todayz/backend/gen/api/coach"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps every coach operation to the action the engine checks (slot 15).
var Ops = map[string]string{
	"ListCoachCards":    policy.CoachView,
	"MarkCoachCardDone": policy.CoachAct,
	"DismissCoachCard":  policy.CoachAct,
}

// Register mounts the coach routes on r. az may be nil only in a route-only
// test, where every call is then refused.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	coachapi.Register(r, handlers{s},
		coachapi.StrictMiddlewareFunc(authz.Strict[coachapi.StrictHandlerFunc](az, Ops)))
}

type handlers struct{ s *Service }

var _ coachapi.StrictServerInterface = handlers{}

func (h handlers) ListCoachCards(ctx context.Context, req coachapi.ListCoachCardsRequestObject) (coachapi.ListCoachCardsResponseObject, error) {
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	f := coachapi.CoachCardFilterLive
	if req.Params.Status != nil {
		f = *req.Params.Status
	}
	l, err := h.s.List(ctx, store, f, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return coachapi.ListCoachCards200JSONResponse(l), nil
}

func (h handlers) MarkCoachCardDone(ctx context.Context, req coachapi.MarkCoachCardDoneRequestObject) (coachapi.MarkCoachCardDoneResponseObject, error) {
	p, store, card, err := actArgs(ctx, req.Sid, req.CardId)
	if err != nil {
		return nil, err
	}
	c, err := h.s.Done(ctx, p.UserID, store, card)
	if err != nil {
		return nil, asProblem(err)
	}
	return coachapi.MarkCoachCardDone200JSONResponse(c), nil
}

func (h handlers) DismissCoachCard(ctx context.Context, req coachapi.DismissCoachCardRequestObject) (coachapi.DismissCoachCardResponseObject, error) {
	p, store, card, err := actArgs(ctx, req.Sid, req.CardId)
	if err != nil {
		return nil, err
	}
	c, err := h.s.Dismiss(ctx, p.UserID, store, card)
	if err != nil {
		return nil, asProblem(err)
	}
	return coachapi.DismissCoachCard200JSONResponse(c), nil
}

func actArgs(ctx context.Context, sid, cid string) (authn.Principal, ids.ID, ids.ID, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, ids.Zero, ids.Zero, problem.New(genproblems.Unauthorized, "")
	}
	store, err := pathID(sid)
	if err != nil {
		return p, ids.Zero, ids.Zero, err
	}
	card, err := pathID(cid)
	return p, store, card, err
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
