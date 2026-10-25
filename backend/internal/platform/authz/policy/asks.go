package policy

import "github.com/todayz/backend/internal/platform/authz"

// Asks actions (W3-16, 02 §3.3 "asks" and "demand" rows, D-101). asks.manage
// (mute) is in business.go because the store settings carry asksMuted.
// Changing the prefilled price of a reply also needs item.price.edit, and
// pricing an unlisted item ("Add it to your stock") needs item.manage; the
// handler checks those on top of asks.reply. The share cards and the site
// strip are public (authz.Public in internal/share).
const (
	// KindCustomer: the signed-in customer, on their own asks (/v1/me).
	MeAsksView   = "me.asks.view"
	MeAsksCreate = "me.asks.create"

	// KindBusiness: by role in {bid} and access to {sid}.
	AsksView  = "asks.view"
	AsksReply = "asks.reply"

	// KindAdmin.
	AdminDemandGapsView = "admin.demand_gaps.view"
	AdminMarqueeApprove = "admin.marquee.approve"
)

func init() {
	staff := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow, authz.Cashier: authz.Allow}
	gaps := map[authz.Role]authz.Grant{
		authz.FieldAgent: authz.Allow, authz.Catalogue: authz.Allow, authz.Analyst: authz.Allow,
		authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow,
	}
	marquee := map[authz.Role]authz.Grant{authz.Catalogue: authz.Allow, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	Register(authz.Table{Domain: "asks", Actions: []authz.Action{
		{Name: MeAsksView, Kind: authz.KindCustomer, Doc: "GET /v1/me/asks, /v1/me/asks/{askId} (C-26): own asks only, another customer's is 404"},
		{Name: MeAsksCreate, Kind: authz.KindCustomer, Doc: "POST /v1/me/asks (C-25), POST /v1/me/asks/{askId}/close: own asks only"},

		{Name: AsksView, Kind: authz.KindBusiness, Grants: staff, Doc: "GET {S}/asks (B-20): no customer field, distance band only"},
		{Name: AsksReply, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/asks/{askId}/reply, .../report; a changed price also needs item.price.edit, an unlisted item item.manage"},

		{Name: AdminDemandGapsView, Kind: authz.KindAdmin, Grants: gaps, Doc: "GET /v1/admin/demand-gaps (A-23): aggregated, no customer, k-floor ask_gap_min_customers"},
		{Name: AdminMarqueeApprove, Kind: authz.KindAdmin, Grants: marquee, Doc: "GET /v1/admin/site-marquee, PUT /v1/admin/site-marquee/{cityId} (A-23): approve or reject a city's proposed strip; audited"},
	}})
}
