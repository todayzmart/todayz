package policy

import "github.com/todayz/backend/internal/platform/authz"

// Credits actions (W2-01, 02 §3.3 "credits" rows; the admin rows W3-01).
const (
	// KindCustomer: the signed-in customer's own wallet (/v1/me).
	MeWalletView = "me.wallet.view"

	// KindBusiness: by role in {bid}.
	CreditAccountView = "credit_account.view"

	// KindAdmin (A-09, D-05, D-73).
	AdminLedgerView        = "admin.ledger.view"        // ledger browser, expiry preview
	AdminAdjustmentRequest = "admin.adjustment.request" // posts at once at or under the threshold, else a pending request
	AdminAdjustmentApprove = "admin.adjustment.approve" // approve or reject a pending request; never one's own
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	ledger := map[authz.Role]authz.Grant{authz.Finance: authz.Allow, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	// The admin role cannot adjust credits (02 §3.3, D-64).
	adjust := map[authz.Role]authz.Grant{authz.Finance: authz.Allow, authz.SuperAdmin: authz.Allow}
	notOwn := map[authz.Role]authz.Grant{authz.Finance: authz.NotOwn, authz.SuperAdmin: authz.NotOwn}
	Register(authz.Table{Domain: "credits", Actions: []authz.Action{
		{Name: MeWalletView, Kind: authz.KindCustomer, Doc: "GET /v1/me/wallet, /v1/me/wallet/transactions (C-16): own wallet only"},
		{Name: CreditAccountView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {B}/credit-account, {B}/statements (B-10): store balances, lines, monthly statements"},

		{Name: AdminLedgerView, Kind: authz.KindAdmin, Grants: ledger, Doc: "GET /v1/admin/ledger/accounts, .../{accountId}/entries, /v1/admin/credits/expiry-preview, /v1/admin/credit-adjustments (A-09)"},
		{Name: AdminAdjustmentRequest, Kind: authz.KindAdmin, Grants: adjust, StepUp: true, Doc: "POST /v1/admin/credit-adjustments: with a reason; at or under admin_adjust_dual_approval_above_paise it posts, else a pending request; audited"},
		{Name: AdminAdjustmentApprove, Kind: authz.KindAdmin, Grants: notOwn, StepUp: true, Doc: "POST /v1/admin/credit-adjustments/{requestId}/approve, /reject: never the requester (D-05); the requester may only cancel; audited"},
	}})
}
