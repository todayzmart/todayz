package policy

import "github.com/todayz/backend/internal/platform/authz"

// Credits actions (W2-01, 02 §3.3 "credits" rows). The admin rows
// (admin.ledger.view, admin.adjustment.request|approve) arrive with W3-01.
const (
	// KindCustomer: the signed-in customer's own wallet (/v1/me).
	MeWalletView = "me.wallet.view"

	// KindBusiness: by role in {bid}.
	CreditAccountView = "credit_account.view"
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "credits", Actions: []authz.Action{
		{Name: MeWalletView, Kind: authz.KindCustomer, Doc: "GET /v1/me/wallet, /v1/me/wallet/transactions (C-16): own wallet only"},
		{Name: CreditAccountView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {B}/credit-account (B-10): store balances and lines"},
	}})
}
