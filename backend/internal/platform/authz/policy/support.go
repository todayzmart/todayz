package policy

import "github.com/todayz/backend/internal/platform/authz"

// Support actions (W3-08, 02 §3.3 "support.tickets" and "admin.support.tickets"
// rows, D-226). W3-08 also built the customer's own tickets (C-19), which W2-09
// had deferred.
const (
	// KindCustomer: the signed-in customer (/v1/me), on tickets they opened.
	MeSupportTickets = "me.support.tickets"

	// KindBusiness: by role in {bid}. A manager sees every ticket of the
	// business, not only their store's (D-226).
	SupportTickets = "support.tickets"

	// KindAdmin: support, admin and super_admin.
	AdminSupportTickets = "admin.support.tickets"
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	agents := map[authz.Role]authz.Grant{authz.Support: authz.Allow, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	Register(authz.Table{Domain: "support", Actions: []authz.Action{
		{Name: MeSupportTickets, Kind: authz.KindCustomer, Doc: "GET|POST /v1/me/support/tickets, GET|POST /v1/me/support/tickets/{ticketId}/messages, POST /v1/me/support/attachments (C-19): own tickets only, notes never shown"},
		{Name: SupportTickets, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET|POST {B}/support/tickets, GET|POST {B}/support/tickets/{ticketId}/messages, POST {B}/support/attachments (B-15): the business's own tickets, notes never shown"},
		{Name: AdminSupportTickets, Kind: authz.KindAdmin, Grants: agents, Doc: "GET /v1/admin/tickets, /v1/admin/tickets/{ticketId}; POST .../assign, .../reply, .../notes, .../close, .../attachments (A-14): assign and close audited"},
	}})
}
