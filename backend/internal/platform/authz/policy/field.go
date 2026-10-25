package policy

import "github.com/todayz/backend/internal/platform/authz"

// Field agent actions (W3-06; 02 §3.3 admin table, 01 §14, D-39, D-64). Every action is
// KindAdmin. A field agent's grant is conditional and the handler (internal/business/agent.go)
// applies it: Own is only businesses whose onboarded_by is the caller; OwnWindow is that and only
// while the business is onboarding or inside field_agent_edit_window_days of its verification.
const (
	AdminBusinessOnboard = "admin.business.onboard"  // A-04: create a business for a shop, its store, photos, owner code; read its items
	AdminFieldItemsEdit  = "admin.field.items.edit"  // A-04: add and change the items of a business the caller onboarded (D-39 window)
	AdminFieldOwnStats   = "admin.field.own_stats"   // A-15: the caller's own numbers
	AdminFieldAgentsView = "admin.field_agents.view" // A-15: every agent's numbers
)

func init() {
	own := map[authz.Role]authz.Grant{authz.FieldAgent: authz.Own, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	window := map[authz.Role]authz.Grant{authz.FieldAgent: authz.OwnWindow, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	admins := map[authz.Role]authz.Grant{authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	Register(authz.Table{Domain: "field", Actions: []authz.Action{
		{Name: AdminBusinessOnboard, Kind: authz.KindAdmin, Grants: own, Doc: "GET/POST /v1/admin/field/businesses, GET/PATCH .../{bid}, POST .../owner-confirm(/verify), .../documents, .../stores, GET .../stores/{sid}/items, .../products: own businesses only"},
		{Name: AdminFieldItemsEdit, Kind: authz.KindAdmin, Grants: window, Doc: "POST .../stores/{sid}/items, PATCH .../items/{itemId}: own, inside field_agent_edit_window_days (D-39)"},
		{Name: AdminFieldOwnStats, Kind: authz.KindAdmin, Grants: own, Doc: "GET /v1/admin/field/me/stats (A-15): the caller's own numbers"},
		{Name: AdminFieldAgentsView, Kind: authz.KindAdmin, Grants: admins, Doc: "GET /v1/admin/field-agents (A-15): every agent's numbers"},
	}})
}
