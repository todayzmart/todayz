package banners

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	bannersapi "github.com/todayz/backend/gen/api/banners"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// maxBody bounds the copy bodyKeys reads; the validator (slot 13) already
// refused anything larger than the 1 MB body limit.
const maxBody = 1 << 20

// ops maps every banners operation to the action the engine checks (slot 15).
var ops = map[string]string{
	"ListBanners":  policy.BannersManage,
	"CreateBanner": policy.BannersManage,
	"GetBanner":    policy.BannersManage,
	"UpdateBanner": policy.BannersManage,
	"TapBanner":    policy.MeBannersTap,
}

// Register mounts the banners routes on r. az is the permission engine
// (slot 15); it may be nil only in a route-only test, where every non-public
// call is then refused. svc may be built without a database (503).
func Register(r chi.Router, svc *Service, az authz.Authorizer) {
	bannersapi.Register(r.With(bodyKeys), handlers{svc},
		bannersapi.StrictMiddlewareFunc(authz.Strict[bannersapi.StrictHandlerFunc](az, ops)),
	)
}

type handlers struct{ s *Service }

var _ bannersapi.StrictServerInterface = handlers{}

type keysKey struct{}

// bodyKeys records the top-level keys of a PATCH body, so that a key sent as
// null (clear it) differs from a key left out (keep it). The body is restored
// for the strict server.
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

func (h handlers) ListBanners(ctx context.Context, req bannersapi.ListBannersRequestObject) (bannersapi.ListBannersResponseObject, error) {
	l, err := h.s.listBanners(ctx, req.Sid, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return bannersapi.ListBanners200JSONResponse(l), nil
}

func (h handlers) CreateBanner(ctx context.Context, req bannersapi.CreateBannerRequestObject) (bannersapi.CreateBannerResponseObject, error) {
	b, v, err := h.s.createBanner(ctx, req.Sid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return bannersapi.CreateBanner201JSONResponse{Body: b, Headers: bannersapi.CreateBanner201ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) GetBanner(ctx context.Context, req bannersapi.GetBannerRequestObject) (bannersapi.GetBannerResponseObject, error) {
	b, v, err := h.s.getBanner(ctx, req.Sid, req.BannerId)
	if err != nil {
		return nil, asProblem(err)
	}
	return bannersapi.GetBanner200JSONResponse{Body: b, Headers: bannersapi.GetBanner200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) UpdateBanner(ctx context.Context, req bannersapi.UpdateBannerRequestObject) (bannersapi.UpdateBannerResponseObject, error) {
	b, v, err := h.s.updateBanner(ctx, req.Sid, req.BannerId, req.Params.IfMatch, req.Body, func(k string) bool { return sent(ctx, k) })
	if err != nil {
		return nil, asProblem(err)
	}
	return bannersapi.UpdateBanner200JSONResponse{Body: b, Headers: bannersapi.UpdateBanner200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) TapBanner(ctx context.Context, req bannersapi.TapBannerRequestObject) (bannersapi.TapBannerResponseObject, error) {
	if err := h.s.tap(ctx, req.BannerId); err != nil {
		return nil, asProblem(err)
	}
	return bannersapi.TapBanner204Response{}, nil
}
