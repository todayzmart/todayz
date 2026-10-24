package baskets

import (
	"context"

	"github.com/go-chi/chi/v5"

	basketsapi "github.com/todayz/backend/gen/api/baskets"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the baskets operations to their authz actions, keyed by the
// generated strict server's method names.
var Ops = map[string]string{
	"ListMyBaskets": policy.MeBasketsView,
	"GetMyBasket":   policy.MeBasketsView,
	"CreateBasket":  policy.MeBasketsManage,
	"UpdateBasket":  policy.MeBasketsManage,
	"DeleteBasket":  policy.MeBasketsManage,
}

// Register mounts /v1/me/baskets* behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	basketsapi.Register(r, handlers{s},
		basketsapi.StrictMiddlewareFunc(authz.Strict[basketsapi.StrictHandlerFunc](az, Ops)))
}

type handlers struct{ s *Service }

var _ basketsapi.StrictServerInterface = handlers{}

// ListMyBaskets implements GET /v1/me/baskets.
func (h handlers) ListMyBaskets(ctx context.Context, _ basketsapi.ListMyBasketsRequestObject) (basketsapi.ListMyBasketsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	l, err := h.s.list(ctx, p.UserID)
	if err != nil {
		return nil, asProblem(err)
	}
	return basketsapi.ListMyBaskets200JSONResponse(l), nil
}

// CreateBasket implements POST /v1/me/baskets.
func (h handlers) CreateBasket(ctx context.Context, req basketsapi.CreateBasketRequestObject) (basketsapi.CreateBasketResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	b, err := h.s.create(ctx, p.UserID, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return basketsapi.CreateBasket201JSONResponse{Body: b, Headers: basketsapi.CreateBasket201ResponseHeaders{ETag: ptr(etag(b.Version))}}, nil
}

// GetMyBasket implements GET /v1/me/baskets/{basketId}.
func (h handlers) GetMyBasket(ctx context.Context, req basketsapi.GetMyBasketRequestObject) (basketsapi.GetMyBasketResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.BasketId)
	if err != nil {
		return nil, err
	}
	b, err := h.s.get(ctx, p.UserID, id)
	if err != nil {
		return nil, asProblem(err)
	}
	return basketsapi.GetMyBasket200JSONResponse{Body: b, Headers: basketsapi.GetMyBasket200ResponseHeaders{ETag: ptr(etag(b.Version))}}, nil
}

// UpdateBasket implements PATCH /v1/me/baskets/{basketId}.
func (h handlers) UpdateBasket(ctx context.Context, req basketsapi.UpdateBasketRequestObject) (basketsapi.UpdateBasketResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.BasketId)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	b, err := h.s.update(ctx, p.UserID, id, req.Params.IfMatch, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return basketsapi.UpdateBasket200JSONResponse{Body: b, Headers: basketsapi.UpdateBasket200ResponseHeaders{ETag: ptr(etag(b.Version))}}, nil
}

// DeleteBasket implements DELETE /v1/me/baskets/{basketId}.
func (h handlers) DeleteBasket(ctx context.Context, req basketsapi.DeleteBasketRequestObject) (basketsapi.DeleteBasketResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.BasketId)
	if err != nil {
		return nil, err
	}
	if err := h.s.remove(ctx, p.UserID, id); err != nil {
		return nil, asProblem(err)
	}
	return basketsapi.DeleteBasket204Response{}, nil
}

func ptr[T any](v T) *T { return &v }
