package rollups

import (
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	api "github.com/todayz/backend/gen/api/admin-pulse"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// PulseOps maps the admin-pulse operations to their authz actions.
var PulseOps = map[string]string{
	"GetAdminPulse":    policy.AdminPulseView,
	"GetAdminPulseMap": policy.AdminPulseView,
}

// Register mounts the admin-pulse routes behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	api.Register(r, handlers{s}, api.StrictMiddlewareFunc(authz.Strict[api.StrictHandlerFunc](az, PulseOps)))
}

type handlers struct{ s *Service }

var _ api.StrictServerInterface = handlers{}

// GetAdminPulse implements GET /v1/admin/pulse?range=.
func (h handlers) GetAdminPulse(ctx context.Context, req api.GetAdminPulseRequestObject) (api.GetAdminPulseResponseObject, error) {
	if err := checkRange(req.Params.Range); err != nil {
		return nil, err
	}
	out, err := h.s.Pulse(ctx, req.Params.Range)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.GetAdminPulse200JSONResponse(out), nil
}

// GetAdminPulseMap implements GET /v1/admin/pulse/map?range=.
func (h handlers) GetAdminPulseMap(ctx context.Context, req api.GetAdminPulseMapRequestObject) (api.GetAdminPulseMapResponseObject, error) {
	if err := checkRange(req.Params.Range); err != nil {
		return nil, err
	}
	out, err := h.s.Map(ctx, req.Params.Range)
	if err != nil {
		return nil, asProblem(err)
	}
	return api.GetAdminPulseMap200JSONResponse(out), nil
}

// checkRange backs up the request validator, which already refuses a range
// outside the enum.
func checkRange(r *api.AdminPulseRange) error {
	if r == nil {
		return nil
	}
	if _, err := rangeDays(*r); err != nil {
		return problem.New(genproblems.ValidationFailed, "").WithField("query.range", "must be one of today, 7d, 30d, 90d")
	}
	return nil
}

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}
