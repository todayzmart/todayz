package offers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	offersapi "github.com/todayz/backend/gen/api/offers"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// maxBody bounds the copy bodyKeys reads; the validator (slot 13) already
// refused anything larger than the 1 MB body limit.
const maxBody = 1 << 20

// ops maps every offers operation to the action the engine checks (slot 15)
// in the path's business and store. The nearby feed is public.
var ops = map[string]string{
	"ListNearbyOffers": authz.Public,
	"ListOffers":       policy.OffersManage,
	"CreateOffer":      policy.OffersManage,
	"GetOffer":         policy.OffersManage,
	"UpdateOffer":      policy.OffersManage,
	"PreviewOffer":     policy.OffersManage,
}

// Register mounts the offers routes on r. az is the permission engine
// (slot 15); it may be nil only in a route-only test, where every non-public
// call is then refused. svc may be built without a database (503).
func Register(r chi.Router, svc *Service, az authz.Authorizer) {
	offersapi.Register(r.With(bodyKeys), handlers{svc},
		offersapi.StrictMiddlewareFunc(authz.Strict[offersapi.StrictHandlerFunc](az, ops)),
	)
}

type handlers struct{ s *Service }

var _ offersapi.StrictServerInterface = handlers{}

type keysKey struct{}

// bodyKeys records the top-level keys of a PATCH body, so that a key sent as
// null (clear it) differs from a key left out (keep it); the generated
// request types read both as nil. The body is restored for the strict server.
func bodyKeys(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodPatch && r.Method != http.MethodPut) || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		var raw map[string]json.RawMessage
		keys := map[string]bool{}
		if json.Unmarshal(body, &raw) == nil {
			for k := range raw {
				keys[k] = true
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keysKey{}, keys)))
	})
}

// sent reports whether the request body had the top-level key (even as null).
func sent(ctx context.Context, key string) bool {
	keys, _ := ctx.Value(keysKey{}).(map[string]bool)
	return keys[key]
}

func (h handlers) ListNearbyOffers(ctx context.Context, req offersapi.ListNearbyOffersRequestObject) (offersapi.ListNearbyOffersResponseObject, error) {
	l, err := h.s.listNearby(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return offersapi.ListNearbyOffers200JSONResponse(l), nil
}

func (h handlers) ListOffers(ctx context.Context, req offersapi.ListOffersRequestObject) (offersapi.ListOffersResponseObject, error) {
	l, err := h.s.listOffers(ctx, req.Sid, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return offersapi.ListOffers200JSONResponse(l), nil
}

func (h handlers) CreateOffer(ctx context.Context, req offersapi.CreateOfferRequestObject) (offersapi.CreateOfferResponseObject, error) {
	o, v, err := h.s.createOffer(ctx, req.Sid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return offersapi.CreateOffer201JSONResponse{Body: o, Headers: offersapi.CreateOffer201ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) GetOffer(ctx context.Context, req offersapi.GetOfferRequestObject) (offersapi.GetOfferResponseObject, error) {
	o, v, err := h.s.getOffer(ctx, req.Sid, req.OfferId)
	if err != nil {
		return nil, asProblem(err)
	}
	return offersapi.GetOffer200JSONResponse{Body: o, Headers: offersapi.GetOffer200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) UpdateOffer(ctx context.Context, req offersapi.UpdateOfferRequestObject) (offersapi.UpdateOfferResponseObject, error) {
	o, v, err := h.s.updateOffer(ctx, req.Sid, req.OfferId, req.Params.IfMatch, req.Body, func(k string) bool { return sent(ctx, k) })
	if err != nil {
		return nil, asProblem(err)
	}
	return offersapi.UpdateOffer200JSONResponse{Body: o, Headers: offersapi.UpdateOffer200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) PreviewOffer(ctx context.Context, req offersapi.PreviewOfferRequestObject) (offersapi.PreviewOfferResponseObject, error) {
	p, err := h.s.previewOffer(ctx, req.Sid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return offersapi.PreviewOffer200JSONResponse(p), nil
}
