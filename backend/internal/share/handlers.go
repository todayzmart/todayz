package share

import (
	"context"

	"github.com/go-chi/chi/v5"

	admindemandapi "github.com/todayz/backend/gen/api/admin-demand"
	shareapi "github.com/todayz/backend/gen/api/share"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// shareOps are public: the customer app creates cards before sign-in and
// link previews are read by crawlers.
var shareOps = map[string]string{
	"CreateShareCard": authz.Public,
	"GetShareCard":    authz.Public,
	"GetSiteMarquee":  authz.Public,
}

var adminOps = map[string]string{
	"ListDemandGaps":    policy.AdminDemandGapsView,
	"ListSiteMarquee":   policy.AdminMarqueeApprove,
	"DecideSiteMarquee": policy.AdminMarqueeApprove,
}

// Register mounts the share routes (/v1/public/share-cards, /v1/public/
// site-marquee) and A-23's admin routes. az may be nil only in a route-only
// test, where every admin call is then refused.
func Register(r chi.Router, svc *Service, az authz.Authorizer) {
	shareapi.Register(r, shareHandlers{svc}, shareapi.StrictMiddlewareFunc(authz.Strict[shareapi.StrictHandlerFunc](az, shareOps)))
	admindemandapi.Register(r, adminHandlers{svc}, admindemandapi.StrictMiddlewareFunc(authz.Strict[admindemandapi.StrictHandlerFunc](az, adminOps)))
}

const (
	noStore   = "no-store"
	hourCache = "public, max-age=3600"
)

type shareHandlers struct{ s *Service }

var _ shareapi.StrictServerInterface = shareHandlers{}

func (h shareHandlers) CreateShareCard(ctx context.Context, req shareapi.CreateShareCardRequestObject) (shareapi.CreateShareCardResponseObject, error) {
	c, err := h.s.create(ctx, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	cc := noStore
	return shareapi.CreateShareCard200JSONResponse{Body: c, Headers: shareapi.CreateShareCard200ResponseHeaders{CacheControl: &cc}}, nil
}

func (h shareHandlers) GetShareCard(ctx context.Context, req shareapi.GetShareCardRequestObject) (shareapi.GetShareCardResponseObject, error) {
	p, err := h.s.get(ctx, req.Code)
	if err != nil {
		return nil, asProblem(err)
	}
	return shareapi.GetShareCard200JSONResponse(p), nil
}

func (h shareHandlers) GetSiteMarquee(ctx context.Context, req shareapi.GetSiteMarqueeRequestObject) (shareapi.GetSiteMarqueeResponseObject, error) {
	m, err := h.s.siteMarquee(ctx, req.Params.City)
	if err != nil {
		return nil, asProblem(err)
	}
	cc := hourCache
	return shareapi.GetSiteMarquee200JSONResponse{Body: m, Headers: shareapi.GetSiteMarquee200ResponseHeaders{CacheControl: &cc}}, nil
}

type adminHandlers struct{ s *Service }

var _ admindemandapi.StrictServerInterface = adminHandlers{}

func (h adminHandlers) ListDemandGaps(ctx context.Context, req admindemandapi.ListDemandGapsRequestObject) (admindemandapi.ListDemandGapsResponseObject, error) {
	l, err := h.s.gaps(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return admindemandapi.ListDemandGaps200JSONResponse(l), nil
}

func (h adminHandlers) ListSiteMarquee(ctx context.Context, req admindemandapi.ListSiteMarqueeRequestObject) (admindemandapi.ListSiteMarqueeResponseObject, error) {
	l, err := h.s.adminMarquee(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return admindemandapi.ListSiteMarquee200JSONResponse(l), nil
}

func (h adminHandlers) DecideSiteMarquee(ctx context.Context, req admindemandapi.DecideSiteMarqueeRequestObject) (admindemandapi.DecideSiteMarqueeResponseObject, error) {
	c, err := h.s.decide(ctx, req.CityId, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return admindemandapi.DecideSiteMarquee200JSONResponse(c), nil
}
