package policy

import "github.com/todayz/backend/internal/platform/authz"

// Admin console actions (W3-05, 02 §3.3 admin table, D-64). Every action here is
// KindAdmin: the slot-11 admin session (otp+totp) and its live roles. The admin
// order cancel is orders' (policy/orders.go), review moderation is reviews'
// (policy/reviews.go) and the jobs console is W1-11's adminapi gate.
const (
	// customers (A-06)
	AdminCustomerView  = "admin.customer.view"  // exact phone search (keyed hash), detail
	AdminCustomerBlock = "admin.customer.block" // block, unblock, revoke a session
	AdminStrikesReset  = "admin.strikes.reset"  // reset a customer's or a store's strikes, with a reason (D-41)

	// orders (A-07)
	AdminOrderView = "admin.order.view" // search, detail with timeline

	// stores (A-05)
	AdminStoreView   = "admin.store.view"   // list, detail
	AdminStoreReview = "admin.store.review" // a new store at a verified business: approve or send back (D-40)
	AdminStoreEdit   = "admin.store.edit"   // pin, hours, force-close

	// leads (Leads tab on A-03)
	AdminLeadView   = "admin.lead.view"
	AdminLeadManage = "admin.lead.manage"

	// banners (A-20)
	AdminBannerModerate = "admin.banner.moderate"

	// settings (A-17); money settings are the keys with unit paise, bp or credits
	AdminSettingsView      = "admin.settings.view"
	AdminSettingsEdit      = "admin.settings.edit"
	AdminSettingsEditMoney = "admin.settings.edit_money"

	// admin users (A-18)
	AdminUsersView   = "admin.users.view"
	AdminUsersManage = "admin.users.manage"

	// audit (A-19 and every screen's audit side panel)
	AdminAuditView    = "admin.audit.view"    // the whole log, every filter
	AdminAuditSubject = "admin.audit.subject" // one subject's history; the service also checks the subject's own view action
)

func init() {
	grant := func(rs ...authz.Role) map[authz.Role]authz.Grant {
		m := map[authz.Role]authz.Grant{}
		for _, r := range rs {
			m[r] = authz.Allow
		}
		return m
	}
	support := grant(authz.Support, authz.Admin, authz.SuperAdmin)
	admins := grant(authz.Admin, authz.SuperAdmin)
	leads := grant(authz.FieldAgent, authz.Support, authz.Admin, authz.SuperAdmin)
	// Analyst reads Pulse, dashboards and jobs only (D-64), so it has no audit panel.
	panel := grant(authz.FieldAgent, authz.Support, authz.Catalogue, authz.Finance, authz.Admin, authz.SuperAdmin)
	Register(authz.Table{Domain: "admin", Actions: []authz.Action{
		{Name: AdminCustomerView, Kind: authz.KindAdmin, Grants: support, Doc: "GET /v1/admin/customers?phone=, /v1/admin/customers/{customerId} (A-06)"},
		{Name: AdminCustomerBlock, Kind: authz.KindAdmin, Grants: admins, Doc: "POST .../customers/{customerId}/block, /unblock; DELETE .../sessions/{sessionId}: audited"},
		{Name: AdminStrikesReset, Kind: authz.KindAdmin, Grants: support, Doc: "POST .../customers/{customerId}/strikes/reset, .../stores/{sid}/strikes/reset: with a reason, audited (D-41)"},

		{Name: AdminOrderView, Kind: authz.KindAdmin, Grants: support, Doc: "GET /v1/admin/orders, /v1/admin/orders/{orderId} (A-07)"},

		{Name: AdminStoreView, Kind: authz.KindAdmin, Grants: support, Doc: "GET /v1/admin/stores, /v1/admin/stores/{sid} (A-05)"},
		{Name: AdminStoreReview, Kind: authz.KindAdmin, Grants: admins, Doc: "POST /v1/admin/stores/{sid}/review: pending_review to live, or back with a note (D-40); audited"},
		{Name: AdminStoreEdit, Kind: authz.KindAdmin, Grants: admins, Doc: "PATCH /v1/admin/stores/{sid}, POST .../force-close: audited"},

		{Name: AdminLeadView, Kind: authz.KindAdmin, Grants: leads, Doc: "GET /v1/admin/leads (Leads tab on A-03)"},
		{Name: AdminLeadManage, Kind: authz.KindAdmin, Grants: leads, Doc: "POST /v1/admin/leads/{leadId}/status: audited"},

		{Name: AdminBannerModerate, Kind: authz.KindAdmin, Grants: support, Doc: "GET /v1/admin/banners, POST .../approve, .../takedown (A-20): audited"},

		{Name: AdminSettingsView, Kind: authz.KindAdmin, Grants: admins, Doc: "GET /v1/admin/settings, .../{key}/history (A-17)"},
		{Name: AdminSettingsEdit, Kind: authz.KindAdmin, Grants: admins, StepUp: true, Doc: "PATCH /v1/admin/settings/{key}: keys that are not money settings; audited"},
		{Name: AdminSettingsEditMoney, Kind: authz.KindAdmin, Grants: grant(authz.SuperAdmin), StepUp: true, Doc: "PATCH /v1/admin/settings/{key} on a key with unit paise, bp or credits (D-38, D-64)"},

		{Name: AdminUsersView, Kind: authz.KindAdmin, Grants: grant(authz.SuperAdmin), Doc: "GET /v1/admin/users (A-18)"},
		{Name: AdminUsersManage, Kind: authz.KindAdmin, Grants: grant(authz.SuperAdmin), StepUp: true, Doc: "POST /v1/admin/users, PATCH .../{adminId}, POST .../reset-totp (A-18): step-up, audited"},

		{Name: AdminAuditView, Kind: authz.KindAdmin, Grants: admins, Doc: "GET /v1/admin/audit-log (A-19)"},
		{Name: AdminAuditSubject, Kind: authz.KindAdmin, Grants: panel, Doc: "GET /v1/admin/audit-log/subject: one subject's history for a screen's audit panel, only when the caller may view that subject"},
	}})
}
