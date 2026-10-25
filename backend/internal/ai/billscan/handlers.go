package billscan

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the supplier bill operations to their route check (owner and manager).
var Ops = map[string]string{
	"CreateSupplierBillDraft":  policy.StockRestock,
	"GetSupplierBillDraft":     policy.StockRestock,
	"ConfirmSupplierBillDraft": policy.StockRestock,
}

// Handlers are the supplier bill operations of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) CreateSupplierBillDraft(ctx context.Context, req aiapi.CreateSupplierBillDraftRequestObject) (aiapi.CreateSupplierBillDraftResponseObject, error) {
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
	return aiapi.CreateSupplierBillDraft202JSONResponse{DraftId: id.String(), Status: aiapi.AiDraftCreatedStatusProcessing}, nil
}

func (h Handlers) GetSupplierBillDraft(ctx context.Context, req aiapi.GetSupplierBillDraftRequestObject) (aiapi.GetSupplierBillDraftResponseObject, error) {
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
	return aiapi.GetSupplierBillDraft200JSONResponse(v), nil
}

func (h Handlers) ConfirmSupplierBillDraft(ctx context.Context, req aiapi.ConfirmSupplierBillDraftRequestObject) (aiapi.ConfirmSupplierBillDraftResponseObject, error) {
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
	return aiapi.ConfirmSupplierBillDraft200JSONResponse(retail.Result(res)), nil
}
