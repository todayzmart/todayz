package policy

import "github.com/todayz/backend/internal/platform/authz"

// Baskets actions (W2-14, D-110). Basket search itself is public
// (discover.yaml, authz.Public in internal/search); these cover the
// customer's saved baskets only.
const (
	// KindCustomer: the signed-in customer (/v1/me), on their own baskets.
	MeBasketsView   = "me.baskets.view"   // list, detail
	MeBasketsManage = "me.baskets.manage" // create, update, delete
)

func init() {
	Register(authz.Table{Domain: "baskets", Actions: []authz.Action{
		{Name: MeBasketsView, Kind: authz.KindCustomer, Doc: "GET /v1/me/baskets, /v1/me/baskets/{basketId} (C-22): own baskets only"},
		{Name: MeBasketsManage, Kind: authz.KindCustomer, Doc: "POST /v1/me/baskets, PATCH|DELETE /v1/me/baskets/{basketId} (C-22): own baskets only"},
	}})
}
