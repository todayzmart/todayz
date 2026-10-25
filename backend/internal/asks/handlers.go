package asks

import (
	"context"

	"github.com/go-chi/chi/v5"

	asksapi "github.com/todayz/backend/gen/api/asks"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// ops maps every asks operation to the action the engine checks (slot 15):
// the customer's own asks, and the store inbox in the path's business and
// store.
var ops = map[string]string{
	"ListMyAsks":    policy.MeAsksView,
	"GetMyAsk":      policy.MeAsksView,
	"CreateAsk":     policy.MeAsksCreate,
	"CloseMyAsk":    policy.MeAsksCreate,
	"ListStoreAsks": policy.AsksView,
	"ReplyToAsk":    policy.AsksReply,
	"ReportAsk":     policy.AsksReply,
}

// Register mounts the asks routes on r. az is the permission engine (slot
// 15); it may be nil only in a route-only test, where every call is then
// refused. svc may be built without a database (503).
func Register(r chi.Router, svc *Service, az authz.Authorizer) {
	asksapi.Register(r, handlers{svc}, asksapi.StrictMiddlewareFunc(authz.Strict[asksapi.StrictHandlerFunc](az, ops)))
}

type handlers struct{ s *Service }

var _ asksapi.StrictServerInterface = handlers{}

func (h handlers) ListMyAsks(ctx context.Context, req asksapi.ListMyAsksRequestObject) (asksapi.ListMyAsksResponseObject, error) {
	l, err := h.s.listMine(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return asksapi.ListMyAsks200JSONResponse(l), nil
}

func (h handlers) CreateAsk(ctx context.Context, req asksapi.CreateAskRequestObject) (asksapi.CreateAskResponseObject, error) {
	d, err := h.s.create(ctx, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return asksapi.CreateAsk201JSONResponse(d), nil
}

func (h handlers) GetMyAsk(ctx context.Context, req asksapi.GetMyAskRequestObject) (asksapi.GetMyAskResponseObject, error) {
	d, err := h.s.getMine(ctx, req.AskId)
	if err != nil {
		return nil, asProblem(err)
	}
	return asksapi.GetMyAsk200JSONResponse(d), nil
}

func (h handlers) CloseMyAsk(ctx context.Context, req asksapi.CloseMyAskRequestObject) (asksapi.CloseMyAskResponseObject, error) {
	d, err := h.s.closeMine(ctx, req.AskId)
	if err != nil {
		return nil, asProblem(err)
	}
	return asksapi.CloseMyAsk200JSONResponse(d), nil
}

func (h handlers) ListStoreAsks(ctx context.Context, req asksapi.ListStoreAsksRequestObject) (asksapi.ListStoreAsksResponseObject, error) {
	l, err := h.s.listStore(ctx, req.Sid, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return asksapi.ListStoreAsks200JSONResponse(l), nil
}

func (h handlers) ReplyToAsk(ctx context.Context, req asksapi.ReplyToAskRequestObject) (asksapi.ReplyToAskResponseObject, error) {
	a, err := h.s.reply(ctx, req.Sid, req.AskId, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return asksapi.ReplyToAsk200JSONResponse(a), nil
}

func (h handlers) ReportAsk(ctx context.Context, req asksapi.ReportAskRequestObject) (asksapi.ReportAskResponseObject, error) {
	if err := h.s.report(ctx, req.Sid, req.AskId, req.Body); err != nil {
		return nil, asProblem(err)
	}
	return asksapi.ReportAsk204Response{}, nil
}
