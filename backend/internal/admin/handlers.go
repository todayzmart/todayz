package admin

import (
	"context"

	"github.com/go-chi/chi/v5"

	auditapi "github.com/todayz/backend/gen/api/admin-audit"
	customersapi "github.com/todayz/backend/gen/api/admin-customers"
	settingsapi "github.com/todayz/backend/gen/api/admin-settings"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/ids"
)

// CustomerOps maps the admin-customers operations to their authz actions.
var CustomerOps = map[string]string{
	"SearchAdminCustomers":       policy.AdminCustomerView,
	"GetAdminCustomer":           policy.AdminCustomerView,
	"BlockAdminCustomer":         policy.AdminCustomerBlock,
	"UnblockAdminCustomer":       policy.AdminCustomerBlock,
	"RevokeAdminCustomerSession": policy.AdminCustomerBlock,
	"ResetAdminCustomerStrikes":  policy.AdminStrikesReset,
}

// AuditOps maps the admin-audit operations to their authz actions. The
// subject panel's slot-15 action only admits the panel roles; the service
// then checks the subject's own view action.
var AuditOps = map[string]string{
	"ListAdminAuditLog":     policy.AdminAuditView,
	"ListAdminSubjectAudit": policy.AdminAuditSubject,
}

// SettingsOps maps the admin-settings operations to their authz actions. A
// change needs a fresh step-up here; the service also checks the money
// action for a paise, bp or credits key.
var SettingsOps = map[string]string{
	"ListAdminSettings":       policy.AdminSettingsView,
	"ListAdminSettingHistory": policy.AdminSettingsView,
	"UpdateAdminSetting":      policy.AdminSettingsEdit,
}

// Register mounts the admin-customers, admin-settings and admin-audit routes behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	customersapi.Register(r, customerHandlers{s},
		customersapi.StrictMiddlewareFunc(authz.Strict[customersapi.StrictHandlerFunc](az, CustomerOps)))
	settingsapi.Register(r, settingHandlers{s},
		settingsapi.StrictMiddlewareFunc(authz.Strict[settingsapi.StrictHandlerFunc](az, SettingsOps)))
	auditapi.Register(r, auditHandlers{s},
		auditapi.StrictMiddlewareFunc(authz.Strict[auditapi.StrictHandlerFunc](az, AuditOps)))
}

type customerHandlers struct{ s *Service }

var _ customersapi.StrictServerInterface = customerHandlers{}

// SearchAdminCustomers implements GET /v1/admin/customers?phone=.
func (h customerHandlers) SearchAdminCustomers(ctx context.Context, req customersapi.SearchAdminCustomersRequestObject) (customersapi.SearchAdminCustomersResponseObject, error) {
	out, err := h.s.search(ctx, req.Params.Phone)
	if err != nil {
		return nil, asProblem(err)
	}
	return customersapi.SearchAdminCustomers200JSONResponse(out), nil
}

// GetAdminCustomer implements GET /v1/admin/customers/{customerId}.
func (h customerHandlers) GetAdminCustomer(ctx context.Context, req customersapi.GetAdminCustomerRequestObject) (customersapi.GetAdminCustomerResponseObject, error) {
	id, err := pathID(req.CustomerId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.customer(ctx, id)
	if err != nil {
		return nil, asProblem(err)
	}
	return customersapi.GetAdminCustomer200JSONResponse(out), nil
}

// BlockAdminCustomer implements POST /v1/admin/customers/{customerId}/block.
func (h customerHandlers) BlockAdminCustomer(ctx context.Context, req customersapi.BlockAdminCustomerRequestObject) (customersapi.BlockAdminCustomerResponseObject, error) {
	actor, id, err := adminCall(ctx, req.CustomerId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.setStatus(ctx, actor, id, "blocked", req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return customersapi.BlockAdminCustomer200JSONResponse(out), nil
}

// UnblockAdminCustomer implements POST /v1/admin/customers/{customerId}/unblock.
func (h customerHandlers) UnblockAdminCustomer(ctx context.Context, req customersapi.UnblockAdminCustomerRequestObject) (customersapi.UnblockAdminCustomerResponseObject, error) {
	actor, id, err := adminCall(ctx, req.CustomerId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.setStatus(ctx, actor, id, "active", req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return customersapi.UnblockAdminCustomer200JSONResponse(out), nil
}

// ResetAdminCustomerStrikes implements POST /v1/admin/customers/{customerId}/strikes/reset.
func (h customerHandlers) ResetAdminCustomerStrikes(ctx context.Context, req customersapi.ResetAdminCustomerStrikesRequestObject) (customersapi.ResetAdminCustomerStrikesResponseObject, error) {
	actor, id, err := adminCall(ctx, req.CustomerId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.resetStrikes(ctx, actor, id, req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return customersapi.ResetAdminCustomerStrikes200JSONResponse(out), nil
}

// RevokeAdminCustomerSession implements DELETE /v1/admin/customers/{customerId}/sessions/{sessionId}.
func (h customerHandlers) RevokeAdminCustomerSession(ctx context.Context, req customersapi.RevokeAdminCustomerSessionRequestObject) (customersapi.RevokeAdminCustomerSessionResponseObject, error) {
	actor, id, err := adminCall(ctx, req.CustomerId, true)
	if err != nil {
		return nil, err
	}
	sid, err := pathID(req.SessionId)
	if err != nil {
		return nil, err
	}
	if err := h.s.revokeSession(ctx, actor, id, sid); err != nil {
		return nil, asProblem(err)
	}
	return customersapi.RevokeAdminCustomerSession204Response{}, nil
}

type auditHandlers struct{ s *Service }

var _ auditapi.StrictServerInterface = auditHandlers{}

// ListAdminAuditLog implements GET /v1/admin/audit-log.
func (h auditHandlers) ListAdminAuditLog(ctx context.Context, req auditapi.ListAdminAuditLogRequestObject) (auditapi.ListAdminAuditLogResponseObject, error) {
	p := req.Params
	f := auditFilter{action: p.Action, typ: p.SubjectType, sub: p.SubjectId, from: p.From, to: p.To, limit: p.Limit, cursor: p.Cursor}
	if p.ActorId != nil {
		a, err := ids.Parse(*p.ActorId)
		if err != nil {
			return nil, fieldErr("query.actorId", "invalid")
		}
		f.actor = ids.UUID(a)
	}
	out, err := h.s.auditLog(ctx, f)
	if err != nil {
		return nil, asProblem(err)
	}
	return auditapi.ListAdminAuditLog200JSONResponse(out), nil
}

// ListAdminSubjectAudit implements GET /v1/admin/audit-log/subject.
func (h auditHandlers) ListAdminSubjectAudit(ctx context.Context, req auditapi.ListAdminSubjectAuditRequestObject) (auditapi.ListAdminSubjectAuditResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	out, err := h.s.subjectAudit(ctx, p, req.Params.SubjectType, req.Params.SubjectId, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return auditapi.ListAdminSubjectAudit200JSONResponse(out), nil
}

type settingHandlers struct{ s *Service }

var _ settingsapi.StrictServerInterface = settingHandlers{}

// ListAdminSettings implements GET /v1/admin/settings.
func (h settingHandlers) ListAdminSettings(ctx context.Context, _ settingsapi.ListAdminSettingsRequestObject) (settingsapi.ListAdminSettingsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	out, err := h.s.listSettings(ctx, p)
	if err != nil {
		return nil, asProblem(err)
	}
	return settingsapi.ListAdminSettings200JSONResponse(out), nil
}

// UpdateAdminSetting implements PATCH /v1/admin/settings/{key}.
func (h settingHandlers) UpdateAdminSetting(ctx context.Context, req settingsapi.UpdateAdminSettingRequestObject) (settingsapi.UpdateAdminSettingResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	out, err := h.s.updateSetting(ctx, p, req.Key, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return settingsapi.UpdateAdminSetting200JSONResponse(out), nil
}

// ListAdminSettingHistory implements GET /v1/admin/settings/{key}/history.
func (h settingHandlers) ListAdminSettingHistory(ctx context.Context, req settingsapi.ListAdminSettingHistoryRequestObject) (settingsapi.ListAdminSettingHistoryResponseObject, error) {
	out, err := h.s.settingHistory(ctx, req.Key, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return settingsapi.ListAdminSettingHistory200JSONResponse(out), nil
}

func adminCall(ctx context.Context, id string, hasBody bool) (actor, target ids.ID, err error) {
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
