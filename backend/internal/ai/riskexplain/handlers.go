package riskexplain

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/assist"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the risk summary operations to A-13's own route check (support and admins).
var Ops = map[string]string{
	"CreateRiskSummaryDraft":  policy.AdminRiskView,
	"GetRiskSummaryDraft":     policy.AdminRiskView,
	"ConfirmRiskSummaryDraft": policy.AdminRiskView,
}

// Handlers are the risk summary operations of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) CreateRiskSummaryDraft(ctx context.Context, req aiapi.CreateRiskSummaryDraftRequestObject) (aiapi.CreateRiskSummaryDraftResponseObject, error) {
	p, err := assist.Caller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, retail.FieldErr("body", "required")
	}
	fid, err := retail.PathID(req.Body.FlagId)
	if err != nil {
		return nil, retail.FieldErr("body.flagId", "no_flag")
	}
	id, err := h.S.Create(ctx, p, fid)
	if err != nil {
		return nil, err
	}
	return aiapi.CreateRiskSummaryDraft202JSONResponse{DraftId: id.String(), Status: aiapi.AiDraftCreatedStatusProcessing}, nil
}

func (h Handlers) GetRiskSummaryDraft(ctx context.Context, req aiapi.GetRiskSummaryDraftRequestObject) (aiapi.GetRiskSummaryDraftResponseObject, error) {
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
	return aiapi.GetRiskSummaryDraft200JSONResponse(v), nil
}

func (h Handlers) ConfirmRiskSummaryDraft(ctx context.Context, req aiapi.ConfirmRiskSummaryDraftRequestObject) (aiapi.ConfirmRiskSummaryDraftResponseObject, error) {
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
	return aiapi.ConfirmRiskSummaryDraft200JSONResponse(retail.Result(res)), nil
}
