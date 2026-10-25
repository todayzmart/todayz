package policy

import "github.com/todayz/backend/internal/platform/authz"

// Insights actions (W3-03; 02 §3.3): the store's overview, Demand insights,
// analytics and repeat customers. Owner and manager only; staff below
// manager get 403 (04 §2.6).
const (
	InsightsView  = "insights.view"  // {S}/overview, {S}/insights, {S}/insights/dismiss, {S}/analytics
	CustomersView = "customers.view" // {S}/customers
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "insights", Actions: []authz.Action{
		{Name: InsightsView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {S}/overview (B-03), GET {S}/insights, POST {S}/insights/dismiss (D-99), GET {S}/analytics (B-12)"},
		{Name: CustomersView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {S}/customers?segment= (B-09): repeat customers, phones masked"},
	}})
}
