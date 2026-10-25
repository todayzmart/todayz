package replydraft

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the review reply operations to their route check (owner and manager).
var Ops = map[string]string{
	"CreateReviewReplyDraft":  policy.ReviewReply,
	"GetReviewReplyDraft":     policy.ReviewReply,
	"ConfirmReviewReplyDraft": policy.ReviewReply,
}

// Handlers are the review reply operations of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) CreateReviewReplyDraft(ctx context.Context, req aiapi.CreateReviewReplyDraftRequestObject) (aiapi.CreateReviewReplyDraftResponseObject, error) {
	sc, p, err := retail.Scope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, retail.FieldErr("body", "required")
	}
	rid, err := retail.PathID(req.Body.ReviewId)
	if err != nil {
		return nil, retail.FieldErr("body.reviewId", "no_review")
	}
	id, err := h.S.Create(ctx, sc, p.UserID, rid)
	if err != nil {
		return nil, err
	}
	return aiapi.CreateReviewReplyDraft202JSONResponse{DraftId: id.String(), Status: aiapi.AiDraftCreatedStatusProcessing}, nil
}

func (h Handlers) GetReviewReplyDraft(ctx context.Context, req aiapi.GetReviewReplyDraftRequestObject) (aiapi.GetReviewReplyDraftResponseObject, error) {
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
	return aiapi.GetReviewReplyDraft200JSONResponse(v), nil
}

func (h Handlers) ConfirmReviewReplyDraft(ctx context.Context, req aiapi.ConfirmReviewReplyDraftRequestObject) (aiapi.ConfirmReviewReplyDraftResponseObject, error) {
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
	return aiapi.ConfirmReviewReplyDraft200JSONResponse(retail.Result(res)), nil
}
