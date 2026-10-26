package catalogue

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/assist"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the catalogue clean-up operations to A-08's review queue check (catalogue reviewers and admins); each line's own action is checked at confirm.
var Ops = map[string]string{
	"CreateCatalogueCleanupDraft":  policy.AdminCatalogueReview,
	"GetCatalogueCleanupDraft":     policy.AdminCatalogueReview,
	"ConfirmCatalogueCleanupDraft": policy.AdminCatalogueReview,
}

// Handlers are the catalogue clean-up operations of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) CreateCatalogueCleanupDraft(ctx context.Context, req aiapi.CreateCatalogueCleanupDraftRequestObject) (aiapi.CreateCatalogueCleanupDraftResponseObject, error) {
	p, err := assist.Caller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, retail.FieldErr("body", "required")
	}
	pid, err := retail.PathID(req.Body.PendingId)
	if err != nil {
		return nil, retail.FieldErr("body.pendingId", "no_pending_product")
	}
	id, err := h.S.Create(ctx, p, pid)
	if err != nil {
		return nil, err
	}
	return aiapi.CreateCatalogueCleanupDraft202JSONResponse{DraftId: id.String(), Status: aiapi.AiDraftCreatedStatusProcessing}, nil
}

func (h Handlers) GetCatalogueCleanupDraft(ctx context.Context, req aiapi.GetCatalogueCleanupDraftRequestObject) (aiapi.GetCatalogueCleanupDraftResponseObject, error) {
	p, err := assist.Caller(ctx)
	if err != nil {
		return nil, err
	}
	id, err := retail.PathID(req.DraftId)
	if err != nil {
		return nil, err
	}
	v, err := h.S.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	return aiapi.GetCatalogueCleanupDraft200JSONResponse(v), nil
}

func (h Handlers) ConfirmCatalogueCleanupDraft(ctx context.Context, req aiapi.ConfirmCatalogueCleanupDraftRequestObject) (aiapi.ConfirmCatalogueCleanupDraftResponseObject, error) {
	p, err := assist.Caller(ctx)
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
	res, err := h.S.Confirm(ctx, p, id, req.Body.Lines)
	if err != nil {
		return nil, err
	}
	return aiapi.ConfirmCatalogueCleanupDraft200JSONResponse(retail.Result(res)), nil
}
