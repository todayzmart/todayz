package adminapi

import (
	"context"
	"slices"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Role is an admin role (04 §2.7, D-64).
type Role string

// Admin roles. super_admin satisfies every role.
const (
	RoleFieldAgent Role = "field_agent"
	RoleSupport    Role = "support"
	RoleCatalogue  Role = "catalogue"
	RoleAnalyst    Role = "analyst"
	RoleFinance    Role = "finance"
	RoleAdmin      Role = "admin"
	RoleSuperAdmin Role = "super_admin"
)

// Admin is the signed-in admin a Gate let through.
type Admin struct {
	ID    ids.ID
	Roles []Role
}

// Has reports whether a holds role r; super_admin holds every role.
func (a Admin) Has(r Role) bool {
	return slices.Contains(a.Roles, r) || slices.Contains(a.Roles, RoleSuperAdmin)
}

// HasAny reports whether a holds one of rs.
func (a Admin) HasAny(rs []Role) bool {
	return slices.ContainsFunc(rs, a.Has)
}

// Gate is the admin auth seam: "require an admin holding one of these roles,
// and a fresh step-up when stepUp is set".
//
// W1-01 owns admin sessions, roles and step-up (D-64): cmd/todayz/routes_admin.go
// passes authz.AdminGate (internal/platform/authz/admingate.go) through a small
// adapter. The contract of Require:
//
//   - no admin session: a *problem.Error with code unauthorized (401);
//   - an admin holding none of anyOf (super_admin counts as every role): forbidden (403);
//   - stepUp and no fresh step-up: W1-01's step_up_required (403, auth.yaml);
//   - otherwise the admin, with every role it holds (the handlers narrow
//     reads by role, e.g. finance sees money kinds only).
//
// Require never writes the response; the handlers return its error as is.
type Gate interface {
	Require(ctx context.Context, anyOf []Role, stepUp bool) (Admin, error)
}

// DenyGate refuses every request with 401 unauthorized. It is the gate when
// Deps.Gate is nil, so a mount without W1-01's gate fails closed.
type DenyGate struct{}

// Require implements Gate.
func (DenyGate) Require(context.Context, []Role, bool) (Admin, error) {
	return Admin{}, problem.New(genproblems.Unauthorized, "admin sign-in is not available yet")
}

// Role sets used by the handlers (04 §2.7, D-95).
var (
	// readRoles may read A-21. finance reads money kinds only.
	readRoles = []Role{RoleSupport, RoleAnalyst, RoleFinance, RoleAdmin, RoleSuperAdmin}
	// actRoles may pause, resume, run and retry non-money kinds (with a reason).
	actRoles = []Role{RoleAdmin, RoleSuperAdmin}
	// moneyRoles may act on money kinds, with step-up.
	moneyRoles = []Role{RoleSuperAdmin}
)

// financeOnly reports whether a reads A-21 only through finance, which
// limits it to money kinds.
func financeOnly(a Admin) bool {
	return a.Has(RoleFinance) && !a.HasAny([]Role{RoleSupport, RoleAnalyst, RoleAdmin})
}
