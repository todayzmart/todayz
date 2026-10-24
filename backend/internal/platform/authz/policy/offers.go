package policy

import "github.com/todayz/backend/internal/platform/authz"

// Offers actions (W2-07, 02 §3.3 "offers" row). The customer-facing
// GET /v1/discover/offers is public (authz.Public in internal/offers).
const (
	OffersManage = "offers.manage"
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "offers", Actions: []authz.Action{
		{Name: OffersManage, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET|POST {S}/offers, GET|PATCH {S}/offers/{offerId}, POST {S}/offer-previews (B-08)"},
	}})
}
