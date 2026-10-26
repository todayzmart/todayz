package triage

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/assist"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the ticket triage operations to A-14's own route check (support and admins).
var Ops = map[string]string{
	"CreateTicketTriageDraft":  policy.AdminSupportTickets,
	"GetTicketTriageDraft":     policy.AdminSupportTickets,
	"ConfirmTicketTriageDraft": policy.AdminSupportTickets,
}

// Handlers are the ticket triage operations of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) CreateTicketTriageDraft(ctx context.Context, req aiapi.CreateTicketTriageDraftRequestObject) (aiapi.CreateTicketTriageDraftResponseObject, error) {
	p, err := assist.Caller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, retail.FieldErr("body", "required")
	}
	tid, err := retail.PathID(req.Body.TicketId)
	if err != nil {
		return nil, retail.FieldErr("body.ticketId", "no_ticket")
	}
	id, err := h.S.Create(ctx, p, tid)
	if err != nil {
		return nil, err
	}
	return aiapi.CreateTicketTriageDraft202JSONResponse{DraftId: id.String(), Status: aiapi.AiDraftCreatedStatusProcessing}, nil
}

func (h Handlers) GetTicketTriageDraft(ctx context.Context, req aiapi.GetTicketTriageDraftRequestObject) (aiapi.GetTicketTriageDraftResponseObject, error) {
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
	return aiapi.GetTicketTriageDraft200JSONResponse(v), nil
}

func (h Handlers) ConfirmTicketTriageDraft(ctx context.Context, req aiapi.ConfirmTicketTriageDraftRequestObject) (aiapi.ConfirmTicketTriageDraftResponseObject, error) {
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
	return aiapi.ConfirmTicketTriageDraft200JSONResponse(retail.Result(res)), nil
}
