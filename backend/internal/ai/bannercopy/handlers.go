package bannercopy

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the banner copy operations to their route check (owner and manager).
var Ops = map[string]string{
	"CreateBannerCopyDraft":  policy.BannersManage,
	"GetBannerCopyDraft":     policy.BannersManage,
	"ConfirmBannerCopyDraft": policy.BannersManage,
}

// Handlers are the banner copy operations of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) CreateBannerCopyDraft(ctx context.Context, req aiapi.CreateBannerCopyDraftRequestObject) (aiapi.CreateBannerCopyDraftResponseObject, error) {
	sc, p, err := retail.Scope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, retail.FieldErr("body", "required")
	}
	id, err := h.S.Create(ctx, sc, p.UserID, *req.Body)
	if err != nil {
		return nil, err
	}
	return aiapi.CreateBannerCopyDraft202JSONResponse{DraftId: id.String(), Status: aiapi.AiDraftCreatedStatusProcessing}, nil
}

func (h Handlers) GetBannerCopyDraft(ctx context.Context, req aiapi.GetBannerCopyDraftRequestObject) (aiapi.GetBannerCopyDraftResponseObject, error) {
	sc, _, err := retail.Scope(ctx)
	if err != nil {
		return nil, err
	}
	id, err := retail.PathID(req.DraftId)
	if err != nil {
		return nil, err
	}
	v, err := h.S.Get(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	return aiapi.GetBannerCopyDraft200JSONResponse(v), nil
}

func (h Handlers) ConfirmBannerCopyDraft(ctx context.Context, req aiapi.ConfirmBannerCopyDraftRequestObject) (aiapi.ConfirmBannerCopyDraftResponseObject, error) {
	sc, p, err := retail.Scope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, retail.FieldErr("body", "required")
	}
	id, err := retail.PathID(req.DraftId)
	if err != nil {
		return nil, err
	}
	res, err := h.S.Confirm(ctx, sc, p, id, req.Body.Lines)
	if err != nil {
		return nil, err
	}
	return aiapi.ConfirmBannerCopyDraft200JSONResponse(res), nil
}
