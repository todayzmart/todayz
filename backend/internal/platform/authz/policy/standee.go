package policy

import "github.com/todayz/backend/internal/platform/authz"

// Standee actions (02 §3.3 "standee.manage": owner and manager; W2-10 the
// PDF, W3-09 the standee rows, their counts and reorders).
const (
	StandeeManage = "standee.manage"
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "standee", Actions: []authz.Action{
		{Name: StandeeManage, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "{S}/standees (list, create), {S}/standee-sheet/printed, {S}/standees/{standeeId}/reorder (B-10)"},
	}})
}
