package pos

import (
	"fmt"

	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// The contract's limits on a bill (schemas/pos.yaml and sync.yaml).
const (
	maxLines     = 200
	maxQuantity  = 9999
	maxUnitPaise = 100_000_000
	maxNameLen   = 120
)

// billLine is one line as sold.
type billLine struct {
	ClientID  ids.ID
	ItemID    ids.ID // zero for a custom line
	ProductID ids.ID
	Name      string
	Unit      int64
	Qty       int32
	Offer     int64 // offers off the line
	Manual    int64 // the cashier's discount off the line
	Total     int64 // Unit × Qty − Offer (manual discounts are the bill's)
}

// bill is a priced bill, checked against ck_pos_sales_money.
type bill struct {
	Lines        []billLine
	ItemSubtotal int64 // Σ line totals before manual discounts
	BillOffer    int64 // offers off the bill
	Subtotal     int64 // ItemSubtotal − BillOffer
	Discount     int64 // Σ manual line discounts + the manual bill discount
	Eligible     int64 // Subtotal − Discount: the bill a QR is shown for
	Credits      int
	RoundOff     int32
	Total        int64
}

// manual reports whether the bill carries a cashier's discount (pos.discount).
func (b bill) manual() bool { return b.Discount > 0 }

// lineInput is a bill line before pricing.
type lineInput struct {
	ClientID  ids.ID
	ItemID    ids.ID
	ProductID ids.ID
	Name      string
	Unit      int64
	Qty       int
	Offer     int64
	Manual    int64
	path      string // the request field the line came from, for errors
}

// price checks and totals a bill (ck_pos_sale_lines_money and
// ck_pos_sales_money). A line total is after its offers; the line totals make
// the item subtotal; the cashier's line and bill discounts together are the
// bill's discount. A discount above what it discounts is a 422 on that
// field, never clamped silently; an offer engine's bill discount is clamped
// to the item subtotal.
func price(in []lineInput, billOffer, billManual int64, credits int, path string) (bill, *problem.Error) {
	if len(in) == 0 || len(in) > maxLines {
		return bill{}, fieldErr(path+".lines", "count")
	}
	b := bill{Lines: make([]billLine, 0, len(in)), Credits: credits}
	seen := make(map[ids.ID]bool, len(in))
	var manual int64
	for i, l := range in {
		p := l.path
		if p == "" {
			p = fmt.Sprintf("%s.lines[%d]", path, i)
		}
		switch {
		case l.ClientID == ids.Zero || seen[l.ClientID]:
			return bill{}, fieldErr(p+".clientId", "duplicate")
		case l.Qty < 1 || l.Qty > maxQuantity:
			return bill{}, fieldErr(p+".quantity", "range")
		case l.Unit < 1 || l.Unit > maxUnitPaise:
			return bill{}, fieldErr(p+".unitPricePaise", "range")
		case len(l.Name) == 0 || len([]rune(l.Name)) > maxNameLen:
			return bill{}, fieldErr(p+".name", "length")
		}
		seen[l.ClientID] = true
		gross := l.Unit * int64(l.Qty)
		if l.Offer < 0 || l.Offer > gross {
			return bill{}, fieldErr(p+".offerDiscountPaise", "range")
		}
		afterOffer := gross - l.Offer
		if l.Manual < 0 || l.Manual > afterOffer {
			return bill{}, fieldErr(p+".manualDiscountPaise", "range")
		}
		b.ItemSubtotal += afterOffer
		manual += l.Manual
		b.Lines = append(b.Lines, billLine{
			ClientID: l.ClientID, ItemID: l.ItemID, ProductID: l.ProductID, Name: l.Name, Unit: l.Unit, Qty: int32(l.Qty), //nolint:gosec // checked above
			Offer: l.Offer, Manual: l.Manual, Total: afterOffer,
		})
	}
	b.BillOffer = min(max(billOffer, 0), b.ItemSubtotal)
	b.Subtotal = b.ItemSubtotal - b.BillOffer
	if billManual < 0 || manual+billManual > b.Subtotal {
		return bill{}, fieldErr(path+".billDiscountPaise", "range")
	}
	b.Discount = manual + billManual
	b.Eligible = b.Subtotal - b.Discount
	if credits < 0 || (credits > 0 && b.Eligible-int64(credits)*100 < 100) {
		return bill{}, fieldErr(path+".credits", "range")
	}
	payable := b.Eligible - int64(credits)*100
	b.RoundOff = roundOff(payable)
	b.Total = payable + int64(b.RoundOff)
	return b, nil
}

// roundOff takes a bill to the nearest ₹1, half up (D-94): −49…+50 paise.
func roundOff(payable int64) int32 {
	r := payable % 100
	if r >= 50 {
		return int32(100 - r) //nolint:gosec // 1..50
	}
	return int32(-r) //nolint:gosec // −49..0
}

// cashbackCredits is a bill's cashback at bp basis points of the paid
// total, in whole credits (1 credit = ₹1), rounded down:
// floor(paise × bp / 10000 / 100), without overflow.
func cashbackCredits(paise int64, bp int32) int {
	if paise <= 0 || bp <= 0 {
		return 0
	}
	const d = 1_000_000 // 10000 bp × 100 paise per credit
	b := int64(bp)
	return int((paise/d)*b + (paise%d)*b/d)
}
