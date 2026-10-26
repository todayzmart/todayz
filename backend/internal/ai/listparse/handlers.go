package listparse

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps the list parse operations to their route check.
var Ops = map[string]string{
	"CreateListParseDraft":  policy.MeAiUse,
	"GetListParseDraft":     policy.MeAiUse,
	"ConfirmListParseDraft": policy.MeAiUse,
}

// Customer is the signed-in caller (401 when anonymous).
func Customer(ctx context.Context) (ids.ID, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return ids.Zero, problem.New(genproblems.Unauthorized, "")
	}
	return p.UserID, nil
}

// Handlers are the list parse operations of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) CreateListParseDraft(ctx context.Context, req aiapi.CreateListParseDraftRequestObject) (aiapi.CreateListParseDraftResponseObject, error) {
	who, err := Customer(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, retail.FieldErr("body", "required")
	}
	id, err := h.S.Create(ctx, who, req.Body.Text)
	if err != nil {
		return nil, err
	}
	return aiapi.CreateListParseDraft202JSONResponse{DraftId: id.String(), Status: aiapi.AiDraftCreatedStatusProcessing}, nil
}

func (h Handlers) GetListParseDraft(ctx context.Context, req aiapi.GetListParseDraftRequestObject) (aiapi.GetListParseDraftResponseObject, error) {
	who, err := Customer(ctx)
	if err != nil {
		return nil, err
	}
	id, err := retail.PathID(req.DraftId)
	if err != nil {
		return nil, err
	}
	v, err := h.S.Get(ctx, who, id)
	if err != nil {
		return nil, err
	}
	return aiapi.GetListParseDraft200JSONResponse(v), nil
}

func (h Handlers) ConfirmListParseDraft(ctx context.Context, req aiapi.ConfirmListParseDraftRequestObject) (aiapi.ConfirmListParseDraftResponseObject, error) {
	who, err := Customer(ctx)
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
	res, err := h.S.Confirm(ctx, who, id, req.Body.Lines)
	if err != nil {
		return nil, err
	}
	return aiapi.ConfirmListParseDraft200JSONResponse(res), nil
}
