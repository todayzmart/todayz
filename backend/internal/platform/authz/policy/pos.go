package policy

import "github.com/todayz/backend/internal/platform/authz"

// POS actions (W2-04, 02 §3.3 "pos" rows, D-06). The sync route needs
// pos.sync; each outbox entry is then checked as its own action: a sale as
// pos.sell (and pos.discount when it carries a manual discount), a stock
// change as the catalogue's stock.count, stock.restock or stock.damage.
const (
	// KindCustomer: the signed-in customer (/v1/me).
	MeCounterCode = "me.counter_code" // the device's counter-code secret
	MeBillClaim   = "me.bill_claim"   // preview and confirm a scanned bill QR
	MePurchases   = "me.purchases"    // online orders and linked POS sales

	// KindBusiness: by role in {bid} with access to {sid}.
	PosSell         = "pos.sell"
	PosLinkCustomer = "pos.link_customer"
	PosDiscount     = "pos.discount"
	PosReverse      = "pos.reverse"
	PosSync         = "pos.sync"
	PosSales        = "pos.sales"
	PosSalesSummary = "pos.sales_summary"
	PosSalesExport  = "pos.sales_export"
)

func init() {
	staff := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow, authz.Cashier: authz.Allow}
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "pos", Actions: []authz.Action{
		{Name: MeCounterCode, Kind: authz.KindCustomer, Doc: "GET /v1/me/counter-code-seed (C-23): this device's own secret"},
		{Name: MeBillClaim, Kind: authz.KindCustomer, Doc: "POST /v1/me/bill-claims/{token}/preview|confirm (C-23)"},
		{Name: MePurchases, Kind: authz.KindCustomer, Doc: "GET /v1/me/purchases (C-13, D-45): own orders and linked sales only"},

		{Name: PosSell, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/pos/sales (B-03), a sale entry in {S}/sync"},
		{Name: PosLinkCustomer, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/bill-claims, GET {S}/bill-claims/{claimId}, POST {S}/counter-code/verify"},
		{Name: PosDiscount, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "a manual line or bill discount on a sale, online or at sync (D-06)"},
		{Name: PosReverse, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "POST {S}/pos/sales/{saleId}/reverse within pos_reversal_window_hours (D-74)"},
		{Name: PosSync, Kind: authz.KindBusiness, Grants: staff, Doc: "POST {S}/sync (B-03, B-05); each entry is checked as its own action"},
		{Name: PosSales, Kind: authz.KindBusiness, Grants: staff, Doc: "GET {S}/pos/sales (B-05 sales history, W2-16; Counter sales filters, D-268), GET {S}/pos/sales/{saleId}"},
		// Store-wide totals follow insights.view (owner and manager), D-268.
		{Name: PosSalesSummary, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {S}/pos/sales-summary (B-04 Counter sales stats, D-268)"},
		{Name: PosSalesExport, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {S}/pos/sales-export (B-04 Counter sales CSV, D-268): no phones"},
	}})
}
