package admin

import (
	"context"

	api "github.com/todayz/backend/gen/api/admin-banners"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// BannerOps maps the admin-banners operations to their authz actions (A-20):
// support and up read the queue and act on it.
var BannerOps = map[string]string{
	"ListAdminBanners":    policy.AdminBannerModerate,
	"ApproveAdminBanner":  policy.AdminBannerModerate,
	"TakedownAdminBanner": policy.AdminBannerModerate,
}

type bannerHandlers struct{ s *Service }

var _ api.StrictServerInterface = bannerHandlers{}

// ListAdminBanners implements GET /v1/admin/banners.
func (h bannerHandlers) ListAdminBanners(ctx context.Context, req api.ListAdminBannersRequestObject) (api.ListAdminBannersResponseObject, error) {
	out, err := h.s.listBanners(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.ListAdminBanners200JSONResponse(out), nil
}

// ApproveAdminBanner implements POST /v1/admin/banners/{bannerId}/approve.
func (h bannerHandlers) ApproveAdminBanner(ctx context.Context, req api.ApproveAdminBannerRequestObject) (api.ApproveAdminBannerResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BannerId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.approveBanner(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.ApproveAdminBanner200JSONResponse(out), nil
}

// TakedownAdminBanner implements POST /v1/admin/banners/{bannerId}/takedown.
func (h bannerHandlers) TakedownAdminBanner(ctx context.Context, req api.TakedownAdminBannerRequestObject) (api.TakedownAdminBannerResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BannerId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.takedownBanner(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.TakedownAdminBanner200JSONResponse(out), nil
}
