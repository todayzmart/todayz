package policy

import "github.com/todayz/backend/internal/platform/authz"

// Coach actions (W3-11; 11 §2.1, B-19): the store's coach cards. Owner and
// manager only, like Demand insights; staff below manager get 403.
const (
	CoachView = "coach.view" // GET {S}/coach/cards
	CoachAct  = "coach.act"  // POST {S}/coach/cards/{cardId}/done|dismiss
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "coach", Actions: []authz.Action{
		{Name: CoachView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {S}/coach/cards (B-19, Overview coach slot); listing marks new cards seen"},
		{Name: CoachAct, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "POST {S}/coach/cards/{cardId}/done, /dismiss (Not useful quiets the subject for coach_dismiss_quiet_days)"},
	}})
}
