package shelfscan

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the shelf photo operations to their route check (owner and manager; the create also needs item.price.edit).
var Ops = map[string]string{
	"CreateShelfPhotoDraft":  policy.ItemManage,
	"GetShelfPhotoDraft":     policy.ItemManage,
	"ConfirmShelfPhotoDraft": policy.ItemManage,
}

// Handlers are the shelf photo operations of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) CreateShelfPhotoDraft(ctx context.Context, req aiapi.CreateShelfPhotoDraftRequestObject) (aiapi.CreateShelfPhotoDraftResponseObject, error) {
	sc, p, err := retail.Scope(ctx)
	if err != nil {
		return nil, err
	}
	img, err := retail.ReadPhoto(req.Body)
	if err != nil {
		return nil, err
	}
	id, err := h.S.Create(ctx, sc, p.UserID, img)
	if err != nil {
		return nil, err
	}
	return aiapi.CreateShelfPhotoDraft202JSONResponse{DraftId: id.String(), Status: aiapi.AiDraftCreatedStatusProcessing}, nil
}

func (h Handlers) GetShelfPhotoDraft(ctx context.Context, req aiapi.GetShelfPhotoDraftRequestObject) (aiapi.GetShelfPhotoDraftResponseObject, error) {
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
	return aiapi.GetShelfPhotoDraft200JSONResponse(v), nil
}

func (h Handlers) ConfirmShelfPhotoDraft(ctx context.Context, req aiapi.ConfirmShelfPhotoDraftRequestObject) (aiapi.ConfirmShelfPhotoDraftResponseObject, error) {
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
	return aiapi.ConfirmShelfPhotoDraft200JSONResponse(retail.Result(res)), nil
}
