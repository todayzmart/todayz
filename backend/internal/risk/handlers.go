package risk

import (
	"context"

	"github.com/go-chi/chi/v5"

	refapi "github.com/todayz/backend/gen/api/admin-referrals"
	riskapi "github.com/todayz/backend/gen/api/admin-risk"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// Ops maps the admin-risk operations to their authz actions, keyed by the
// generated strict server's method names.
var Ops = map[string]string{
	"ListRiskFlags":   policy.AdminRiskView,
	"GetRiskFlag":     policy.AdminRiskView,
	"ClearRiskFlag":   policy.AdminRiskResolve,
	"ConfirmRiskFlag": policy.AdminRiskResolve,
}

// ReferralOps maps the admin-referrals operations to their authz actions.
var ReferralOps = map[string]string{
	"ListAdminReferrals":  policy.AdminReferralsView,
	"RejectAdminReferral": policy.AdminReferralReject,
}

// Register mounts A-13 Risk and A-10 Referrals behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	riskapi.Register(r, handlers{s},
		riskapi.StrictMiddlewareFunc(authz.Strict[riskapi.StrictHandlerFunc](az, Ops)))
	refapi.Register(r, refHandlers{s},
		refapi.StrictMiddlewareFunc(authz.Strict[refapi.StrictHandlerFunc](az, ReferralOps)))
}

type handlers struct{ s *Service }

var _ riskapi.StrictServerInterface = handlers{}

// ListRiskFlags implements GET /v1/admin/risk/flags.
func (h handlers) ListRiskFlags(ctx context.Context, req riskapi.ListRiskFlagsRequestObject) (riskapi.ListRiskFlagsResponseObject, error) {
	l, err := h.s.listFlags(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return riskapi.ListRiskFlags200JSONResponse(l), nil
}

// GetRiskFlag implements GET /v1/admin/risk/flags/{flagId}.
func (h handlers) GetRiskFlag(ctx context.Context, req riskapi.GetRiskFlagRequestObject) (riskapi.GetRiskFlagResponseObject, error) {
	id, err := pathID(req.FlagId)
	if err != nil {
		return nil, err
	}
	d, err := h.s.getFlag(ctx, id)
	if err != nil {
		return nil, asProblem(err)
	}
	return riskapi.GetRiskFlag200JSONResponse(d), nil
}

// ClearRiskFlag implements POST /v1/admin/risk/flags/{flagId}/clear.
func (h handlers) ClearRiskFlag(ctx context.Context, req riskapi.ClearRiskFlagRequestObject) (riskapi.ClearRiskFlagResponseObject, error) {
	f, err := h.resolve(ctx, req.FlagId, req.Body, "cleared")
	if err != nil {
		return nil, err
	}
	return riskapi.ClearRiskFlag200JSONResponse(f), nil
}

// ConfirmRiskFlag implements POST /v1/admin/risk/flags/{flagId}/confirm.
func (h handlers) ConfirmRiskFlag(ctx context.Context, req riskapi.ConfirmRiskFlagRequestObject) (riskapi.ConfirmRiskFlagResponseObject, error) {
	f, err := h.resolve(ctx, req.FlagId, req.Body, "confirmed")
	if err != nil {
		return nil, err
	}
	return riskapi.ConfirmRiskFlag200JSONResponse(f), nil
}

func (h handlers) resolve(ctx context.Context, flagID string, body *riskapi.RiskResolveRequest, to string) (riskapi.RiskFlag, error) {
	p, err := principal(ctx)
	if err != nil {
		return riskapi.RiskFlag{}, err
	}
	id, err := pathID(flagID)
	if err != nil {
		return riskapi.RiskFlag{}, err
	}
	if body == nil {
		return riskapi.RiskFlag{}, fieldErr("body", "required")
	}
	f, err := h.s.resolve(ctx, p.UserID, id, to, body.Reason)
	if err != nil {
		return riskapi.RiskFlag{}, asProblem(err)
	}
	return f, nil
}

type refHandlers struct{ s *Service }

var _ refapi.StrictServerInterface = refHandlers{}

// ListAdminReferrals implements GET /v1/admin/referrals.
func (h refHandlers) ListAdminReferrals(ctx context.Context, req refapi.ListAdminReferralsRequestObject) (refapi.ListAdminReferralsResponseObject, error) {
	l, err := h.s.listReferrals(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return refapi.ListAdminReferrals200JSONResponse(l), nil
}

// RejectAdminReferral implements POST /v1/admin/referrals/{referralId}/reject.
func (h refHandlers) RejectAdminReferral(ctx context.Context, req refapi.RejectAdminReferralRequestObject) (refapi.RejectAdminReferralResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.ReferralId)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	r, err := h.s.rejectReferral(ctx, p.UserID, id, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return refapi.RejectAdminReferral200JSONResponse(r), nil
}
