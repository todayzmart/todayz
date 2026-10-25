package credits

import (
	"context"

	"github.com/go-chi/chi/v5"

	api "github.com/todayz/backend/gen/api/admin-credits"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// AdminOps maps the admin-credits operations to their authz actions. Reject
// is the approve action: a second person rejects, the requester (whom the
// NotOwn grant lets through to the handler) can only cancel.
var AdminOps = map[string]string{
	"ListAdminLedgerAccounts":     policy.AdminLedgerView,
	"ListAdminLedgerEntries":      policy.AdminLedgerView,
	"GetAdminCreditExpiryPreview": policy.AdminLedgerView,
	"ListCreditAdjustments":       policy.AdminLedgerView,
	"CreateCreditAdjustment":      policy.AdminAdjustmentRequest,
	"ApproveCreditAdjustment":     policy.AdminAdjustmentApprove,
	"RejectCreditAdjustment":      policy.AdminAdjustmentApprove,
}

// RegisterAdmin mounts A-09's routes.
func RegisterAdmin(r chi.Router, a *Admin, az authz.Authorizer) {
	api.Register(r, adminHandlers{a}, api.StrictMiddlewareFunc(authz.Strict[api.StrictHandlerFunc](az, AdminOps)))
}

type adminHandlers struct{ a *Admin }

var _ api.StrictServerInterface = adminHandlers{}

const defaultExpiryDays = 30 //settings:ok the contract's days default (getAdminCreditExpiryPreview)

func adminPathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, problem.New(genproblems.NotFound, "")
	}
	return id, nil
}

// ListAdminLedgerAccounts implements GET /v1/admin/ledger/accounts.
func (h adminHandlers) ListAdminLedgerAccounts(ctx context.Context, req api.ListAdminLedgerAccountsRequestObject) (api.ListAdminLedgerAccountsResponseObject, error) {
	out, err := h.a.LedgerAccounts(ctx, req.Params.Q)
	if err != nil {
		return nil, err
	}
	return api.ListAdminLedgerAccounts200JSONResponse(out), nil
}

// ListAdminLedgerEntries implements GET /v1/admin/ledger/accounts/{accountId}/entries.
func (h adminHandlers) ListAdminLedgerEntries(ctx context.Context, req api.ListAdminLedgerEntriesRequestObject) (api.ListAdminLedgerEntriesResponseObject, error) {
	id, err := adminPathID(req.AccountId)
	if err != nil {
		return nil, err
	}
	out, err := h.a.LedgerEntries(ctx, id, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	return api.ListAdminLedgerEntries200JSONResponse(out), nil
}

// GetAdminCreditExpiryPreview implements GET /v1/admin/credits/expiry-preview.
func (h adminHandlers) GetAdminCreditExpiryPreview(ctx context.Context, req api.GetAdminCreditExpiryPreviewRequestObject) (api.GetAdminCreditExpiryPreviewResponseObject, error) {
	days := defaultExpiryDays
	if req.Params.Days != nil {
		days = *req.Params.Days
	}
	out, err := h.a.ExpiryPreview(ctx, days)
	if err != nil {
		return nil, err
	}
	return api.GetAdminCreditExpiryPreview200JSONResponse(out), nil
}

// ListCreditAdjustments implements GET /v1/admin/credit-adjustments.
func (h adminHandlers) ListCreditAdjustments(ctx context.Context, req api.ListCreditAdjustmentsRequestObject) (api.ListCreditAdjustmentsResponseObject, error) {
	p, err := adminPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	var status *string
	if req.Params.Status != nil {
		s := string(*req.Params.Status)
		status = &s
	}
	out, err := h.a.AdjustmentRequests(ctx, p.UserID, status, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	return api.ListCreditAdjustments200JSONResponse(out), nil
}

// CreateCreditAdjustment implements POST /v1/admin/credit-adjustments.
func (h adminHandlers) CreateCreditAdjustment(ctx context.Context, req api.CreateCreditAdjustmentRequestObject) (api.CreateCreditAdjustmentResponseObject, error) {
	p, err := adminPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	in := AdjustmentInput{AccountID: req.Body.AccountId, AmountPaise: req.Body.AmountPaise, Reason: req.Body.Reason, Link: req.Body.Link}
	if req.Params.IdempotencyKey != nil {
		in.IdempotencyKey = *req.Params.IdempotencyKey
	}
	out, err := h.a.CreateAdjustment(ctx, p, in)
	if err != nil {
		return nil, err
	}
	return api.CreateCreditAdjustment201JSONResponse(out), nil
}

// ApproveCreditAdjustment implements POST /v1/admin/credit-adjustments/{requestId}/approve.
func (h adminHandlers) ApproveCreditAdjustment(ctx context.Context, req api.ApproveCreditAdjustmentRequestObject) (api.ApproveCreditAdjustmentResponseObject, error) {
	p, err := adminPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := adminPathID(req.RequestId)
	if err != nil {
		return nil, err
	}
	var note *string
	if req.Body != nil {
		note = &req.Body.Note
	}
	out, err := h.a.ApproveAdjustment(ctx, p, id, note)
	if err != nil {
		return nil, err
	}
	return api.ApproveCreditAdjustment200JSONResponse(out), nil
}

// RejectCreditAdjustment implements POST /v1/admin/credit-adjustments/{requestId}/reject.
func (h adminHandlers) RejectCreditAdjustment(ctx context.Context, req api.RejectCreditAdjustmentRequestObject) (api.RejectCreditAdjustmentResponseObject, error) {
	p, err := adminPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := adminPathID(req.RequestId)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	out, err := h.a.RejectAdjustment(ctx, p, id, req.Body.Note)
	if err != nil {
		return nil, err
	}
	return api.RejectCreditAdjustment200JSONResponse(out), nil
}
