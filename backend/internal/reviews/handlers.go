package reviews

import (
	"context"

	"github.com/go-chi/chi/v5"

	adminapi "github.com/todayz/backend/gen/api/admin-reviews"
	reviewsapi "github.com/todayz/backend/gen/api/reviews"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps the reviews operations (customer, shop and public page) to their
// authz actions, keyed by the generated strict server's method names.
var Ops = map[string]string{
	"ListDiscoverStoreReviews": authz.Public,
	"ListMyReviews":            policy.MeReviewsView,
	"CreateReview":             policy.MeReviewsManage,
	"UpdateReview":             policy.MeReviewsManage,
	"ReportReview":             policy.MeReviewsManage,
	"ListStoreReviews":         policy.ReviewsView,
	"ReplyToReview":            policy.ReviewReply,
	"UpdateReviewReply":        policy.ReviewReply,
	"ReportStoreReview":        policy.ReviewReport,
}

// AdminOps maps the admin-reviews operations to their authz actions.
var AdminOps = map[string]string{
	"ListAdminReviews":       policy.AdminReviewModerate,
	"HideReview":             policy.AdminReviewModerate,
	"HideReviewReply":        policy.AdminReviewModerate,
	"ListAdminReviewReports": policy.AdminReviewModerate,
	"ResolveReviewReport":    policy.AdminReviewModerate,
	"RestoreReview":          policy.AdminReviewRestore,
	"RestoreReviewReply":     policy.AdminReviewRestore,
}

// Register mounts the customer, shop and public review routes and the admin
// moderation routes behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	reviewsapi.Register(r, handlers{s},
		reviewsapi.StrictMiddlewareFunc(authz.Strict[reviewsapi.StrictHandlerFunc](az, Ops)))
	adminapi.Register(r, adminHandlers{s},
		adminapi.StrictMiddlewareFunc(authz.Strict[adminapi.StrictHandlerFunc](az, AdminOps)))
}

type handlers struct{ s *Service }

var _ reviewsapi.StrictServerInterface = handlers{}

// ListDiscoverStoreReviews implements GET /v1/discover/stores/{slug}/reviews.
func (h handlers) ListDiscoverStoreReviews(ctx context.Context, req reviewsapi.ListDiscoverStoreReviewsRequestObject) (reviewsapi.ListDiscoverStoreReviewsResponseObject, error) {
	l, err := h.s.listPublic(ctx, req.Slug, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.ListDiscoverStoreReviews200JSONResponse(l), nil
}

// ListMyReviews implements GET /v1/me/reviews.
func (h handlers) ListMyReviews(ctx context.Context, req reviewsapi.ListMyReviewsRequestObject) (reviewsapi.ListMyReviewsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	l, err := h.s.listMine(ctx, p.UserID, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.ListMyReviews200JSONResponse(l), nil
}

// CreateReview implements POST /v1/me/reviews.
func (h handlers) CreateReview(ctx context.Context, req reviewsapi.CreateReviewRequestObject) (reviewsapi.CreateReviewResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	m, err := h.s.create(ctx, p.UserID, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.CreateReview201JSONResponse(m), nil
}

// UpdateReview implements PATCH /v1/me/reviews/{reviewId}.
func (h handlers) UpdateReview(ctx context.Context, req reviewsapi.UpdateReviewRequestObject) (reviewsapi.UpdateReviewResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.ReviewId)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	m, err := h.s.update(ctx, p.UserID, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.UpdateReview200JSONResponse(m), nil
}

// ReportReview implements POST /v1/me/review-reports.
func (h handlers) ReportReview(ctx context.Context, req reviewsapi.ReportReviewRequestObject) (reviewsapi.ReportReviewResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	rid, err := ParseID(req.Body.ReviewId)
	if err != nil {
		return nil, fieldErr("body.reviewId", "invalid")
	}
	rc, err := h.s.reportAsCustomer(ctx, p.UserID, rid, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.ReportReview201JSONResponse(rc), nil
}

// ListStoreReviews implements GET /v1/biz/businesses/{bid}/stores/{sid}/reviews.
func (h handlers) ListStoreReviews(ctx context.Context, req reviewsapi.ListStoreReviewsRequestObject) (reviewsapi.ListStoreReviewsResponseObject, error) {
	sid, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	l, err := h.s.listForStore(ctx, sid, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.ListStoreReviews200JSONResponse(l), nil
}

// ReplyToReview implements POST .../reviews/{reviewId}/reply.
func (h handlers) ReplyToReview(ctx context.Context, req reviewsapi.ReplyToReviewRequestObject) (reviewsapi.ReplyToReviewResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	sid, rid, err := storeAndReview(req.Sid, req.ReviewId)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	rp, err := h.s.reply(ctx, p.UserID, sid, rid, req.Body.Text)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.ReplyToReview201JSONResponse(rp), nil
}

// UpdateReviewReply implements PATCH .../reviews/{reviewId}/reply.
func (h handlers) UpdateReviewReply(ctx context.Context, req reviewsapi.UpdateReviewReplyRequestObject) (reviewsapi.UpdateReviewReplyResponseObject, error) {
	sid, rid, err := storeAndReview(req.Sid, req.ReviewId)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	rp, err := h.s.editReply(ctx, sid, rid, req.Body.Text)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.UpdateReviewReply200JSONResponse(rp), nil
}

// ReportStoreReview implements POST .../reviews/{reviewId}/report.
func (h handlers) ReportStoreReview(ctx context.Context, req reviewsapi.ReportStoreReviewRequestObject) (reviewsapi.ReportStoreReviewResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	sid, rid, err := storeAndReview(req.Sid, req.ReviewId)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	rc, err := h.s.reportAsStore(ctx, p.UserID, sid, rid, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return reviewsapi.ReportStoreReview201JSONResponse(rc), nil
}

type adminHandlers struct{ s *Service }

var _ adminapi.StrictServerInterface = adminHandlers{}

// ListAdminReviews implements GET /v1/admin/reviews.
func (h adminHandlers) ListAdminReviews(ctx context.Context, req adminapi.ListAdminReviewsRequestObject) (adminapi.ListAdminReviewsResponseObject, error) {
	var state *string
	if req.Params.State != nil {
		state = ptr(string(*req.Params.State))
	}
	l, err := h.s.listAdmin(ctx, state, req.Params.StoreId, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.ListAdminReviews200JSONResponse(l), nil
}

// HideReview implements POST /v1/admin/reviews/{reviewId}/hide.
func (h adminHandlers) HideReview(ctx context.Context, req adminapi.HideReviewRequestObject) (adminapi.HideReviewResponseObject, error) {
	p, rid, err := adminCall(ctx, req.ReviewId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	a, err := h.s.hide(ctx, p, rid, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.HideReview200JSONResponse(a), nil
}

// RestoreReview implements POST /v1/admin/reviews/{reviewId}/restore.
func (h adminHandlers) RestoreReview(ctx context.Context, req adminapi.RestoreReviewRequestObject) (adminapi.RestoreReviewResponseObject, error) {
	p, rid, err := adminCall(ctx, req.ReviewId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	a, err := h.s.restore(ctx, p, rid, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.RestoreReview200JSONResponse(a), nil
}

// HideReviewReply implements POST /v1/admin/reviews/{reviewId}/reply/hide.
func (h adminHandlers) HideReviewReply(ctx context.Context, req adminapi.HideReviewReplyRequestObject) (adminapi.HideReviewReplyResponseObject, error) {
	p, rid, err := adminCall(ctx, req.ReviewId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	a, err := h.s.hideReply(ctx, p, rid, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.HideReviewReply200JSONResponse(a), nil
}

// RestoreReviewReply implements POST /v1/admin/reviews/{reviewId}/reply/restore.
func (h adminHandlers) RestoreReviewReply(ctx context.Context, req adminapi.RestoreReviewReplyRequestObject) (adminapi.RestoreReviewReplyResponseObject, error) {
	p, rid, err := adminCall(ctx, req.ReviewId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	a, err := h.s.restoreReply(ctx, p, rid, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.RestoreReviewReply200JSONResponse(a), nil
}

// ListAdminReviewReports implements GET /v1/admin/review-reports.
func (h adminHandlers) ListAdminReviewReports(ctx context.Context, req adminapi.ListAdminReviewReportsRequestObject) (adminapi.ListAdminReviewReportsResponseObject, error) {
	var status *string
	if req.Params.Status != nil {
		status = ptr(string(*req.Params.Status))
	}
	l, err := h.s.listReports(ctx, status, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.ListAdminReviewReports200JSONResponse(l), nil
}

// ResolveReviewReport implements POST /v1/admin/review-reports/{reportId}/resolve.
func (h adminHandlers) ResolveReviewReport(ctx context.Context, req adminapi.ResolveReviewReportRequestObject) (adminapi.ResolveReviewReportResponseObject, error) {
	p, rid, err := adminCall(ctx, req.ReportId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	r, err := h.s.resolveReport(ctx, p, rid, string(req.Body.Resolution), req.Body.Note)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminapi.ResolveReviewReport200JSONResponse(r), nil
}

// adminCall reads the admin principal and the id in the path, and checks that a body came.
func adminCall(ctx context.Context, id string, hasBody bool) (admin, target ids.ID, err error) {
	p, err := principal(ctx)
	if err != nil {
		return ids.Zero, ids.Zero, err
	}
	target, err = pathID(id)
	if err != nil {
		return ids.Zero, ids.Zero, err
	}
	if !hasBody {
		return ids.Zero, ids.Zero, fieldErr("body", "required")
	}
	return p.UserID, target, nil
}

func storeAndReview(sid, rid string) (store, review ids.ID, err error) {
	if store, err = pathID(sid); err != nil {
		return ids.Zero, ids.Zero, err
	}
	if review, err = pathID(rid); err != nil {
		return ids.Zero, ids.Zero, err
	}
	return store, review, nil
}

// ParseID parses a ULID in a body field.
func ParseID(s string) (ids.ID, error) { return ids.Parse(s) }
