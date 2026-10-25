package admin

import (
	"context"

	api "github.com/todayz/backend/gen/api/admin-notifications"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// NotifyOps maps the admin-notifications operations to their authz actions
// (A-16, D-224): reads for admins, and the test send to the caller's own
// inbox. There is no edit.
var NotifyOps = map[string]string{
	"ListAdminNotificationTemplates": policy.AdminNotifyView,
	"ListAdminIntegrations":          policy.AdminNotifyView,
	"TestSendAdminNotification":      policy.AdminNotifyManage,
}

type notifyHandlers struct{ s *Service }

var _ api.StrictServerInterface = notifyHandlers{}

// ListAdminNotificationTemplates implements GET /v1/admin/notification-templates.
func (h notifyHandlers) ListAdminNotificationTemplates(_ context.Context, req api.ListAdminNotificationTemplatesRequestObject) (api.ListAdminNotificationTemplatesResponseObject, error) {
	return api.ListAdminNotificationTemplates200JSONResponse(h.s.listNotificationTemplates(req.Params.App)), nil
}

// ListAdminIntegrations implements GET /v1/admin/integrations.
func (h notifyHandlers) ListAdminIntegrations(ctx context.Context, _ api.ListAdminIntegrationsRequestObject) (api.ListAdminIntegrationsResponseObject, error) {
	out, err := h.s.listIntegrations(ctx)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.ListAdminIntegrations200JSONResponse(out), nil
}

// TestSendAdminNotification implements POST /v1/admin/notification-templates/test-send.
func (h notifyHandlers) TestSendAdminNotification(ctx context.Context, req api.TestSendAdminNotificationRequestObject) (api.TestSendAdminNotificationResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	out, err := h.s.testSend(ctx, p.UserID, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.TestSendAdminNotification201JSONResponse(out), nil
}
