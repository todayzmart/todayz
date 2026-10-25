package policy

import "github.com/todayz/backend/internal/platform/authz"

// Risk actions (W3-02, 02 §3.3 "risk" and "referrals" rows, D-64, D-78).
// A-13 Risk is read by support, admin and super_admin; only admin and
// super_admin clear or confirm a flag. A-10 Referrals is admin and
// super_admin. None of them is step-up: a flag moves no money itself, and a
// referral reject only stops a reward that has not posted.
const (
	AdminRiskView       = "admin.risk.view"       // flags queue and a flag's evidence (A-13)
	AdminRiskResolve    = "admin.risk.resolve"    // clear or confirm a flag, with a reason
	AdminReferralsView  = "admin.referrals.view"  // referrals list, risk-flagged filter (A-10)
	AdminReferralReject = "admin.referral.reject" // reject a referral whose reward has not posted
)

func init() {
	support := map[authz.Role]authz.Grant{authz.Support: authz.Allow, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	admins := map[authz.Role]authz.Grant{authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	Register(authz.Table{Domain: "risk", Actions: []authz.Action{
		{Name: AdminRiskView, Kind: authz.KindAdmin, Grants: support, Doc: "GET /v1/admin/risk/flags, /v1/admin/risk/flags/{flagId} (A-13)"},
		{Name: AdminRiskResolve, Kind: authz.KindAdmin, Grants: admins, Doc: "POST /v1/admin/risk/flags/{flagId}/clear, .../confirm: audited with a reason; blocking or suspending is the customer's or business's own action"},
		{Name: AdminReferralsView, Kind: authz.KindAdmin, Grants: admins, Doc: "GET /v1/admin/referrals (A-10)"},
		{Name: AdminReferralReject, Kind: authz.KindAdmin, Grants: admins, Doc: "POST /v1/admin/referrals/{referralId}/reject: pending, or qualified with no reward posted; audited with a reason"},
	}})
}
