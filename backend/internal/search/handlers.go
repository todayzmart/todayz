package search

import (
	"context"

	"github.com/go-chi/chi/v5"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/internal/platform/authz"
)

// discoverOps: every discover operation is public (anyone may look up
// places, browse shops and search; a signed-in caller is only recorded on
// the search log). Slot 15 lets a Public operation through without a check.
var discoverOps = map[string]string{
	"ReversePlace":           authz.Public,
	"SearchPlaces":           authz.Public,
	"SearchDiscover":         authz.Public,
	"SearchSuggest":          authz.Public,
	"ListDiscoverStores":     authz.Public,
	"GetDiscoverStore":       authz.Public,
	"ListDiscoverStoreItems": authz.Public,
	"GetDiscoverSummary":     authz.Public,
}

// Register mounts /v1/places/* and /v1/discover/* on r. az may be nil: every
// operation here is public.
func Register(r chi.Router, svc *Service, az authz.Authorizer) {
	discoverapi.Register(r, handlers{svc},
		discoverapi.StrictMiddlewareFunc(authz.Strict[discoverapi.StrictHandlerFunc](az, discoverOps)),
	)
}

type handlers struct{ s *Service }

var _ discoverapi.StrictServerInterface = handlers{}

func (h handlers) ReversePlace(ctx context.Context, req discoverapi.ReversePlaceRequestObject) (discoverapi.ReversePlaceResponseObject, error) {
	out, err := h.s.ReversePlace(ctx, req.Params)
	if err != nil {
		return nil, err
	}
	return discoverapi.ReversePlace200JSONResponse(out), nil
}

func (h handlers) SearchPlaces(ctx context.Context, req discoverapi.SearchPlacesRequestObject) (discoverapi.SearchPlacesResponseObject, error) {
	out, err := h.s.SearchPlaces(ctx, req.Params)
	if err != nil {
		return nil, err
	}
	return discoverapi.SearchPlaces200JSONResponse(out), nil
}

func (h handlers) SearchDiscover(ctx context.Context, req discoverapi.SearchDiscoverRequestObject) (discoverapi.SearchDiscoverResponseObject, error) {
	out, err := h.s.Search(ctx, req.Params)
	if err != nil {
		return nil, err
	}
	return discoverapi.SearchDiscover200JSONResponse(out), nil
}

func (h handlers) SearchSuggest(ctx context.Context, req discoverapi.SearchSuggestRequestObject) (discoverapi.SearchSuggestResponseObject, error) {
	out, err := h.s.Suggest(ctx, req.Params)
	if err != nil {
		return nil, err
	}
	return discoverapi.SearchSuggest200JSONResponse(out), nil
}

func (h handlers) ListDiscoverStores(ctx context.Context, req discoverapi.ListDiscoverStoresRequestObject) (discoverapi.ListDiscoverStoresResponseObject, error) {
	out, err := h.s.ListStores(ctx, req.Params)
	if err != nil {
		return nil, err
	}
	return discoverapi.ListDiscoverStores200JSONResponse(out), nil
}

func (h handlers) GetDiscoverStore(ctx context.Context, req discoverapi.GetDiscoverStoreRequestObject) (discoverapi.GetDiscoverStoreResponseObject, error) {
	out, err := h.s.GetStore(ctx, req.Slug, req.Params)
	if err != nil {
		return nil, err
	}
	return discoverapi.GetDiscoverStore200JSONResponse(out), nil
}

func (h handlers) ListDiscoverStoreItems(ctx context.Context, req discoverapi.ListDiscoverStoreItemsRequestObject) (discoverapi.ListDiscoverStoreItemsResponseObject, error) {
	out, err := h.s.ListStoreItems(ctx, req.Slug, req.Params)
	if err != nil {
		return nil, err
	}
	return discoverapi.ListDiscoverStoreItems200JSONResponse(out), nil
}

func (h handlers) GetDiscoverSummary(ctx context.Context, req discoverapi.GetDiscoverSummaryRequestObject) (discoverapi.GetDiscoverSummaryResponseObject, error) {
	out, err := h.s.Summary(ctx, req.Params)
	if err != nil {
		return nil, err
	}
	return discoverapi.GetDiscoverSummary200JSONResponse(out), nil
}
