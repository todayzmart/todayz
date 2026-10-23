package policy

import "github.com/todayz/backend/internal/platform/authz"

// Notify actions (W2-05): the realtime streams (02 §5) and the in-app inbox
// and preferences (04 §2.2, §2.6).
//
// The matrix (02 §3.3) lists {S}/stream under orders' `order.view` and
// asks' `asks.view`, and the admin stream under pulse's `admin.pulse.view`.
// Action names are unique across tables and those domains have not landed,
// so the streams have their own actions here with the same rows (owner,
// manager and cashier with access to the store; every admin role except
// field_agent). TODO(lead): fold store.stream into order.view when W2-02
// lands and admin.stream into admin.pulse.view with W3-07, or keep them
// separate; the grants are identical either way.
const (
	// KindCustomer: the signed-in customer (/v1/me).
	MeStream        = "me.stream"
	MeNotifications = "me.notifications"
	// KindAny: whoever is signed in, any app.
	MeNotificationPrefs = "me.notification_prefs"

	// KindBusiness: by role in {bid} and, for the stream, access to {sid}.
	StoreStream = "store.stream"
	InboxView   = "inbox.view"

	// KindAdmin.
	AdminStream = "admin.stream"
)

func init() {
	staff := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow, authz.Cashier: authz.Allow}
	pulse := map[authz.Role]authz.Grant{
		authz.Support: authz.Allow, authz.Catalogue: authz.Allow, authz.Analyst: authz.Allow,
		authz.Finance: authz.Allow, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow,
	}
	Register(authz.Table{Domain: "notify", Actions: []authz.Action{
		{Name: MeStream, Kind: authz.KindCustomer, Doc: "GET /v1/me/stream: the customer's SSE stream (orders, asks, inbox; D-101)"},
		{Name: MeNotifications, Kind: authz.KindCustomer, Doc: "GET /v1/me/notifications, POST /v1/me/notifications/read (C-20)"},
		{Name: MeNotificationPrefs, Kind: authz.KindAny, Doc: "GET/PATCH /v1/me/notification-prefs: own per-kind on/off, any app"},

		{Name: StoreStream, Kind: authz.KindBusiness, Grants: staff, Doc: "GET {S}/stream: the store's SSE stream (02 §3.3 order.view and asks.view rows)"},
		{Name: InboxView, Kind: authz.KindBusiness, Grants: staff, Doc: "GET {B}/inbox, POST {B}/inbox/read (B-16, D-46); a store-limited member sees only their stores' rows"},

		{Name: AdminStream, Kind: authz.KindAdmin, Grants: pulse, Doc: "GET /v1/admin/stream: ops live counters (02 §3.3 admin.pulse.view row)"},
	}})
}
