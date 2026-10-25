package policy

import "github.com/todayz/backend/internal/platform/authz"

// Voice actions (W3-04, B-06, D-06, D-65). voice.draft is the route check:
// any staff role records a voice update and reads or confirms their own
// drafts; owners and managers also see other staff's drafts in the store
// (voice.draft.others). Confirm then checks each line as its own domain's
// action: sale as pos.sell, count as stock.count, restock as stock.restock
// and set_price as item.price.edit.
const (
	VoiceDraft       = "voice.draft"
	VoiceDraftOthers = "voice.draft.others"
)

func init() {
	staff := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow, authz.Cashier: authz.Allow}
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "voice", Actions: []authz.Action{
		{Name: VoiceDraft, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/ai/voice-stock/drafts; GET and confirm own drafts (each line checked again at confirm)"},
		{Name: VoiceDraftOthers, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET and confirm a voice draft another staff member recorded in the store"},
	}})
}
