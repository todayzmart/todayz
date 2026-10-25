package payouts

import (
	"bytes"
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	api "github.com/todayz/backend/gen/api/admin-payouts"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps the admin-payouts operations to their authz actions. The money
// actions (hold, unhold, mark paid, export) are finance and super_admin with
// step-up (D-64); the business's statements are credit_account.view.
var Ops = map[string]string{
	"ListAdminStatements":    policy.AdminPayoutView,
	"GetAdminStatement":      policy.AdminPayoutView,
	"ListAdminPayouts":       policy.AdminPayoutView,
	"GetAdminPayout":         policy.AdminPayoutView,
	"HoldPayout":             policy.AdminPayoutManage,
	"UnholdPayout":           policy.AdminPayoutManage,
	"MarkPayoutPaid":         policy.AdminPayoutManage,
	"ExportPayouts":          policy.AdminPayoutExport,
	"ListBusinessStatements": policy.CreditAccountView,
	"GetBusinessStatement":   policy.CreditAccountView,
}

// Register mounts A-11's and B-10's routes. staff limits a business member to
// their stores (D-125).
func Register(r chi.Router, s *Service, az authz.Authorizer, staff authz.StaffResolver) {
	api.Register(r, handlers{s: s, staff: staff}, api.StrictMiddlewareFunc(authz.Strict[api.StrictHandlerFunc](az, Ops)))
}

type handlers struct {
	s     *Service
	staff authz.StaffResolver
}

var _ api.StrictServerInterface = handlers{}

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, problem.New(genproblems.NotFound, "")
	}
	return id, nil
}

func csvBody(b []byte) (*bytes.Reader, int64) { return bytes.NewReader(b), int64(len(b)) }

// ListAdminStatements implements GET /v1/admin/statements.
func (h handlers) ListAdminStatements(ctx context.Context, req api.ListAdminStatementsRequestObject) (api.ListAdminStatementsResponseObject, error) {
	out, err := h.s.Statements(ctx, req.Params.Period, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	return api.ListAdminStatements200JSONResponse(out), nil
}

// GetAdminStatement implements GET /v1/admin/statements/{statementId}.
func (h handlers) GetAdminStatement(ctx context.Context, req api.GetAdminStatementRequestObject) (api.GetAdminStatementResponseObject, error) {
	id, err := pathID(req.StatementId)
	if err != nil {
		return nil, err
	}
	if req.Params.Format != nil && *req.Params.Format == api.Csv {
		b, err := h.s.StatementCSV(ctx, id)
		if err != nil {
			return nil, err
		}
		body, n := csvBody(b)
		return api.GetAdminStatement200TextcsvResponse{Body: body, ContentLength: n}, nil
	}
	out, err := h.s.Statement(ctx, id)
	if err != nil {
		return nil, err
	}
	return api.GetAdminStatement200JSONResponse(out), nil
}

// ListAdminPayouts implements GET /v1/admin/payouts.
func (h handlers) ListAdminPayouts(ctx context.Context, req api.ListAdminPayoutsRequestObject) (api.ListAdminPayoutsResponseObject, error) {
	var status *string
	if req.Params.Status != nil {
		st := string(*req.Params.Status)
		status = &st
	}
	out, err := h.s.Payouts(ctx, req.Params.Period, status, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	return api.ListAdminPayouts200JSONResponse(out), nil
}

// GetAdminPayout implements GET /v1/admin/payouts/{payoutId}.
func (h handlers) GetAdminPayout(ctx context.Context, req api.GetAdminPayoutRequestObject) (api.GetAdminPayoutResponseObject, error) {
	id, err := pathID(req.PayoutId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.Payout(ctx, id)
	if err != nil {
		return nil, err
	}
	return api.GetAdminPayout200JSONResponse(out), nil
}

// HoldPayout implements POST /v1/admin/payouts/{payoutId}/hold.
func (h handlers) HoldPayout(ctx context.Context, req api.HoldPayoutRequestObject) (api.HoldPayoutResponseObject, error) {
	p, id, err := h.admin(ctx, req.PayoutId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.Hold(ctx, p, id, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return api.HoldPayout200JSONResponse(out), nil
}

// UnholdPayout implements POST /v1/admin/payouts/{payoutId}/unhold.
func (h handlers) UnholdPayout(ctx context.Context, req api.UnholdPayoutRequestObject) (api.UnholdPayoutResponseObject, error) {
	p, id, err := h.admin(ctx, req.PayoutId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.Unhold(ctx, p, id, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return api.UnholdPayout200JSONResponse(out), nil
}

// MarkPayoutPaid implements POST /v1/admin/payouts/{payoutId}/mark-paid.
func (h handlers) MarkPayoutPaid(ctx context.Context, req api.MarkPayoutPaidRequestObject) (api.MarkPayoutPaidResponseObject, error) {
	p, id, err := h.admin(ctx, req.PayoutId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.MarkPaid(ctx, p, id, req.Body.Utr, req.Body.AmountPaise, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return api.MarkPayoutPaid200JSONResponse(out), nil
}

func (h handlers) admin(ctx context.Context, payout string, hasBody bool) (authn.Principal, ids.ID, error) {
	p, err := principal(ctx)
	if err != nil {
		return authn.Principal{}, ids.Zero, err
	}
	id, err := pathID(payout)
	if err != nil {
		return authn.Principal{}, ids.Zero, err
	}
	if !hasBody {
		return authn.Principal{}, ids.Zero, problem.New(genproblems.BadRequest, "")
	}
	return p, id, nil
}

// ExportPayouts implements GET /v1/admin/payout-exports.
func (h handlers) ExportPayouts(ctx context.Context, req api.ExportPayoutsRequestObject) (api.ExportPayoutsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	b, err := h.s.Export(ctx, p, req.Params.Period)
	if err != nil {
		return nil, err
	}
	body, n := csvBody(b)
	return api.ExportPayouts200TextcsvResponse{Body: body, ContentLength: n}, nil
}

// ListBusinessStatements implements GET /v1/biz/businesses/{bid}/statements.
func (h handlers) ListBusinessStatements(ctx context.Context, req api.ListBusinessStatementsRequestObject) (api.ListBusinessStatementsResponseObject, error) {
	bid, stores, err := h.member(ctx)
	if err != nil {
		return nil, err
	}
	out, err := h.s.BusinessStatements(ctx, bid, stores, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	return api.ListBusinessStatements200JSONResponse(out), nil
}

// GetBusinessStatement implements GET /v1/biz/businesses/{bid}/statements/{period}.
func (h handlers) GetBusinessStatement(ctx context.Context, req api.GetBusinessStatementRequestObject) (api.GetBusinessStatementResponseObject, error) {
	bid, stores, err := h.member(ctx)
	if err != nil {
		return nil, err
	}
	if req.Params.Format != nil && *req.Params.Format == api.Csv {
		b, err := h.s.BusinessStatementCSV(ctx, bid, stores, req.Period)
		if err != nil {
			return nil, err
		}
		body, n := csvBody(b)
		return api.GetBusinessStatement200TextcsvResponse{Body: body, ContentLength: n}, nil
	}
	out, err := h.s.BusinessStatement(ctx, bid, stores, req.Period)
	if err != nil {
		return nil, err
	}
	return api.GetBusinessStatement200JSONResponse(out), nil
}

// member is the business the route's check passed and the caller's store
// limit there (nil: every store).
func (h handlers) member(ctx context.Context) (ids.ID, []ids.ID, error) {
	p, err := principal(ctx)
	if err != nil {
		return ids.Zero, nil, err
	}
	d, ok := authz.DecisionFrom(ctx)
	if !ok || d.Scope.BusinessID == ids.Zero {
		return ids.Zero, nil, problem.Wrap(genproblems.Internal, errors.New("payouts: no business in the authz decision"))
	}
	if h.staff == nil {
		return ids.Zero, nil, problem.Wrap(genproblems.ServiceUnavailable, ErrUnavailable)
	}
	m, ok, err := h.staff.Membership(ctx, p.UserID, d.Scope.BusinessID)
	if err != nil {
		return ids.Zero, nil, problem.Wrap(genproblems.Internal, err)
	}
	if !ok {
		// authz allowed a moment ago; the membership ended in between.
		return ids.Zero, nil, problem.New(genproblems.NotFound, "")
	}
	return d.Scope.BusinessID, m.StoreIDs, nil
}
