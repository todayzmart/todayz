package policy

import "github.com/todayz/backend/internal/platform/authz"

// Identity actions (W1-01). Sign-in, refresh, logout, logout-all and the
// admin TOTP routes are authz.Public: they run before there is a principal,
// authenticate by their own cookie, or are how an admin reaches otp+totp
// (the handler checks the admin caller).
const (
	MeView   = "me.view"
	MeEdit   = "me.edit"
	MeDelete = "me.delete"
)

func init() {
	Register(authz.Table{Domain: "identity", Actions: []authz.Action{
		{Name: MeView, Kind: authz.KindAny, Doc: "GET /v1/me: own profile, any app"},
		{Name: MeEdit, Kind: authz.KindAny, Doc: "PATCH /v1/me: own name and language, any app"},
		{Name: MeDelete, Kind: authz.KindCustomer, Doc: "DELETE /v1/me: delete own account, customer app, with an SMS OTP (D-31)"},
	}})
}
