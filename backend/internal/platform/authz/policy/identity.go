package policy

import "github.com/todayz/backend/internal/platform/authz"

// Identity actions (W1-01). Sign-in, refresh, logout, logout-all and the
// admin SMS step-up routes are authz.Public: they run before there is a
// principal, authenticate by their own cookie, or are how an admin refreshes
// their step-up (the handler checks the admin caller, D-264).
const (
	MeView   = "me.view"
	MeEdit   = "me.edit"
	MeDelete = "me.delete"
	// Saved addresses (W2-15): the customer's own only; another user's is 404.
	MeAddressesView   = "me.addresses.view"
	MeAddressesManage = "me.addresses.manage"
	// Profile photo (D-259): the customer's own, seen only by them.
	MePhotoManage = "me.photo.manage"
)

func init() {
	Register(authz.Table{Domain: "identity", Actions: []authz.Action{
		{Name: MeView, Kind: authz.KindAny, Doc: "GET /v1/me: own profile, any app"},
		{Name: MeEdit, Kind: authz.KindAny, Doc: "PATCH /v1/me: own name and language, any app"},
		{Name: MeDelete, Kind: authz.KindCustomer, Doc: "DELETE /v1/me: delete own account, customer app, with an SMS OTP (D-31)"},
		{Name: MeAddressesView, Kind: authz.KindCustomer, Doc: "GET /v1/me/addresses: own saved addresses, customer app"},
		{Name: MePhotoManage, Kind: authz.KindCustomer, Doc: "POST /v1/me/photo/uploads, POST .../confirm, DELETE /v1/me/photo: add, change or remove own profile photo, customer app"},
		{Name: MeAddressesManage, Kind: authz.KindCustomer, Doc: "POST/PATCH/DELETE /v1/me/addresses: add, change or delete own saved addresses, customer app"},
	}})
}
