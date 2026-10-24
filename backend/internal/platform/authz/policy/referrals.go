package policy

import "github.com/todayz/backend/internal/platform/authz"

// Referrals actions (W2-03, 01 §8). GET /v1/public/ref/{code} is authz.Public
// and has no action.
const (
	// KindCustomer: the signed-in customer (/v1/me), on their own referrals.
	MeReferralsView  = "me.referrals.view"  // code, link, rewards, stats (C-16)
	MeReferralsClaim = "me.referrals.claim" // claim the referral the account joined through

	// KindBusiness: by role in {bid}.
	ReferralsView  = "referrals.view"
	ReferralsClaim = "referrals.claim" // D-160: the owner claims the shop's referral
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	owner := map[authz.Role]authz.Grant{authz.Owner: authz.Allow}
	Register(authz.Table{Domain: "referrals", Actions: []authz.Action{
		{Name: MeReferralsView, Kind: authz.KindCustomer, Doc: "GET /v1/me/referrals (C-16): own code and stats"},
		{Name: MeReferralsClaim, Kind: authz.KindCustomer, Doc: "POST /v1/me/referrals/claim: once, within the attribution window of sign-up"},

		{Name: ReferralsView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {B}/referrals (B-10)"},
		{Name: ReferralsClaim, Kind: authz.KindBusiness, Grants: owner, Doc: "POST {B}/referrals/claim: once, before verification (D-160)"},
	}})
}
