package policy

import "github.com/todayz/backend/internal/platform/authz"

// Rollups actions (W3-07): the admin Pulse (A-02, 04 §2.7). Pulse and its
// map are read-only for every admin role except field_agent (D-64, 01 §13).
const (
	AdminPulseView = "admin.pulse.view" // GET /v1/admin/pulse, /v1/admin/pulse/map
)

func init() {
	pulse := map[authz.Role]authz.Grant{
		authz.Support: authz.Allow, authz.Catalogue: authz.Allow, authz.Analyst: authz.Allow,
		authz.Finance: authz.Allow, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow,
	}
	Register(authz.Table{Domain: "rollups", Actions: []authz.Action{
		{Name: AdminPulseView, Kind: authz.KindAdmin, Grants: pulse, Doc: "GET /v1/admin/pulse?range=, /v1/admin/pulse/map (A-02): read-only"},
	}})
}
