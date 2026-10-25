package admin

import (
	"context"

	api "github.com/todayz/backend/gen/api/admin-users"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// UserOps maps the admin-users operations to their authz actions (A-18):
// super_admin only, and every change needs a fresh step-up.
var UserOps = map[string]string{
	"ListAdminUsers":     policy.AdminUsersView,
	"InviteAdminUser":    policy.AdminUsersManage,
	"UpdateAdminUser":    policy.AdminUsersManage,
	"ResetAdminUserTotp": policy.AdminUsersManage,
}

type userHandlers struct{ s *Service }

var _ api.StrictServerInterface = userHandlers{}

// ListAdminUsers implements GET /v1/admin/users.
func (h userHandlers) ListAdminUsers(ctx context.Context, req api.ListAdminUsersRequestObject) (api.ListAdminUsersResponseObject, error) {
	out, err := h.s.listAdminUsers(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.ListAdminUsers200JSONResponse(out), nil
}

// InviteAdminUser implements POST /v1/admin/users.
func (h userHandlers) InviteAdminUser(ctx context.Context, req api.InviteAdminUserRequestObject) (api.InviteAdminUserResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	out, err := h.s.inviteAdminUser(ctx, p.UserID, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.InviteAdminUser201JSONResponse(out), nil
}

// UpdateAdminUser implements PATCH /v1/admin/users/{adminId}.
func (h userHandlers) UpdateAdminUser(ctx context.Context, req api.UpdateAdminUserRequestObject) (api.UpdateAdminUserResponseObject, error) {
	actor, id, err := adminCall(ctx, req.AdminId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.updateAdminUser(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.UpdateAdminUser200JSONResponse(out), nil
}

// ResetAdminUserTotp implements POST /v1/admin/users/{adminId}/reset-totp.
func (h userHandlers) ResetAdminUserTotp(ctx context.Context, req api.ResetAdminUserTotpRequestObject) (api.ResetAdminUserTotpResponseObject, error) {
	actor, id, err := adminCall(ctx, req.AdminId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.resetAdminTOTP(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.ResetAdminUserTotp200JSONResponse(out), nil
}
