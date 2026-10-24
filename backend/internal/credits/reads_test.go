package credits

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/sqlc/creditsdb"
)

// CR-04, CR-05: the wallet line texts with an order number and the
// order's end state (the database tests cover lines without an order row).
func TestWalletLineText(t *testing.T) {
	s := func(v string) *string { return &v }
	order, claim := string(RefOrder), string(RefBillClaim)
	for _, tc := range []struct {
		row  creditsdb.WalletLinesRow
		want string
	}{
		{creditsdb.WalletLinesRow{Kind: string(TxSpendHold), RefType: &order, OrderNumber: s("TZ-4821"), ShopName: "Lakshmi Traders"},
			"Held for order TZ-4821 at Lakshmi Traders"},
		{creditsdb.WalletLinesRow{Kind: string(TxSpendCapture), RefType: &order, OrderNumber: s("TZ-4821"), ShopName: "Lakshmi Traders"},
			"Spent at Lakshmi Traders · order TZ-4821"},
		{creditsdb.WalletLinesRow{Kind: string(TxSpendRelease), RefType: &order, OrderNumber: s("TZ-4821"), OrderStatus: s("cancelled")},
			"Returned: order TZ-4821 was cancelled"},
		{creditsdb.WalletLinesRow{Kind: string(TxSpendRelease), RefType: &order, OrderNumber: s("TZ-4821"), OrderStatus: s("expired")},
			"Returned: order TZ-4821 expired"},
		{creditsdb.WalletLinesRow{Kind: string(TxSpendRelease), RefType: &order, OrderNumber: s("TZ-4821"), OrderStatus: s("rejected"), ShopName: "Lakshmi Traders"},
			"Returned: Lakshmi Traders could not take order TZ-4821"},
		{creditsdb.WalletLinesRow{Kind: string(TxSpendRelease), RefType: &order, OrderNumber: s("TZ-4821"), OrderStatus: s("accepted"), Memo: s(memoRecap)},
			"Returned: order TZ-4821 changed"},
		{creditsdb.WalletLinesRow{Kind: string(TxSpendRelease), RefType: &claim},
			"Returned: the bill was not confirmed in time"},
		{creditsdb.WalletLinesRow{Kind: string(TxCashback), RefType: &order, Memo: s("bp:500"), ShopName: "Lakshmi Traders"},
			"5% back at Lakshmi Traders"},
		{creditsdb.WalletLinesRow{Kind: string(TxExpiry)}, "Expired"},
		{creditsdb.WalletLinesRow{Kind: string(TxAdminAdjustment)}, "Adjustment by TodayZ"},
		{creditsdb.WalletLinesRow{Kind: string(TxForfeitDeletion)}, "Forfeited when the account was deleted"},
	} {
		require.Equal(t, tc.want, walletLine(tc.row).Text)
	}
}
