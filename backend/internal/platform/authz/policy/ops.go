package policy

import "github.com/todayz/backend/internal/platform/authz"

// Ops actions (W3-15): A-22 System (04 §2.7, D-97). Read-only, admin and
// super_admin only (06 §3); A-21 Jobs is W1-11's adminapi gate, not a row here.
const (
	AdminSystemView = "admin.system.view" // GET /v1/admin/system
)

func init() {
	Register(authz.Table{Domain: "ops", Actions: []authz.Action{
		{Name: AdminSystemView, Kind: authz.KindAdmin,
			Grants: map[authz.Role]authz.Grant{authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow},
			Doc:    "GET /v1/admin/system (A-22): Beanstalk, ALB, RDS, alarms, database and River panels; read-only"},
	}})
}
