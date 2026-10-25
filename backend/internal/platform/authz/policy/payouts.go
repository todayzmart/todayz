package policy

import "github.com/todayz/backend/internal/platform/authz"

// Payouts actions (W3-01, 02 §3.3 "payouts" rows, A-11). The business side's
// statements (B-10) are credits' credit_account.view.
const (
	AdminPayoutView     = "admin.payout.view"      // statements, payouts
	AdminPayoutBankView = "admin.payout.bank_view" // the payee's UPI ID and bank account, masked to the last 4
	AdminPayoutExport   = "admin.payout.export"    // the bank/UPI batch CSV with decrypted payee fields (D-85)
	AdminPayoutManage   = "admin.payout.manage"    // hold, unhold, mark paid with a UTR
)

func init() {
	grant := func(rs ...authz.Role) map[authz.Role]authz.Grant {
		m := map[authz.Role]authz.Grant{}
		for _, r := range rs {
			m[r] = authz.Allow
		}
		return m
	}
	view := grant(authz.Finance, authz.Admin, authz.SuperAdmin)
	// Admin has no payout hold, unhold or mark paid (D-64).
	money := grant(authz.Finance, authz.SuperAdmin)
	Register(authz.Table{Domain: "payouts", Actions: []authz.Action{
		{Name: AdminPayoutView, Kind: authz.KindAdmin, Grants: view, Doc: "GET /v1/admin/payouts, .../{payoutId}, /v1/admin/payout-statements, .../{statementId}.csv (A-11)"},
		{Name: AdminPayoutBankView, Kind: authz.KindAdmin, Grants: view, Doc: "the masked payee block on GET /v1/admin/payouts/{payoutId} (last 4 only)"},
		{Name: AdminPayoutExport, Kind: authz.KindAdmin, Grants: money, StepUp: true, Doc: "GET /v1/admin/payout-exports?period=: pending payouts with decrypted UPI and bank fields; audited with ids only"},
		{Name: AdminPayoutManage, Kind: authz.KindAdmin, Grants: money, StepUp: true, Doc: "POST /v1/admin/payouts/{payoutId}/hold, /unhold, /mark-paid (UTR): audited"},
	}})
}
