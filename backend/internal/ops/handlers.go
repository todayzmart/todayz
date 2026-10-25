package ops

import (
	"context"

	"github.com/go-chi/chi/v5"

	api "github.com/todayz/backend/gen/api/admin-system"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// SystemOps maps the admin-system operations to their authz actions.
var SystemOps = map[string]string{
	"GetAdminSystem": policy.AdminSystemView,
}

// Register mounts GET /v1/admin/system behind slot 15 (admin.system.view).
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	api.Register(r, handlers{s}, api.StrictMiddlewareFunc(authz.Strict[api.StrictHandlerFunc](az, SystemOps)))
}

type handlers struct{ s *Service }

var _ api.StrictServerInterface = handlers{}

// GetAdminSystem implements GET /v1/admin/system: always 200, each panel with
// its own status.
func (h handlers) GetAdminSystem(ctx context.Context, _ api.GetAdminSystemRequestObject) (api.GetAdminSystemResponseObject, error) {
	return api.GetAdminSystem200JSONResponse(h.s.System(ctx)), nil
}
