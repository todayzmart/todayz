package policy

import "github.com/todayz/backend/internal/platform/authz"

// Banners actions (W2-07, 02 §3.3 "banners" row). Admin moderation is W3-05
// (support and above, D-64) and has its own actions there.
const (
	BannersManage = "banners.manage"
	// KindCustomer: the signed-in customer (/v1/me).
	MeBannersTap = "me.banners.tap"
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "banners", Actions: []authz.Action{
		{Name: BannersManage, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET|POST {S}/banners, GET|PATCH {S}/banners/{bannerId} (B-08)"},
		{Name: MeBannersTap, Kind: authz.KindCustomer, Doc: "POST /v1/me/banners/{bannerId}/tap: records the tap that attribution reads (D-77)"},
	}})
}
