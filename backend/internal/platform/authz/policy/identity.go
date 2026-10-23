package policy

import "github.com/todayz/backend/internal/platform/authz"

// Identity actions (W1-01). Sign-in, refresh and logout are authz.Public:
// they run before there is a principal, or authenticate by their own cookie.
const (
	MeView = "me.view"
	MeEdit = "me.edit"
)

func init() {
	Register(authz.Table{Domain: "identity", Actions: []authz.Action{
		{Name: MeView, Kind: authz.KindAny, Doc: "GET /v1/me: own profile, any app"},
		{Name: MeEdit, Kind: authz.KindAny, Doc: "PATCH /v1/me: own name and language, any app"},
	}})
}
