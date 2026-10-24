package offers

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// The pricing interface (W2-07; 01 §5.5, §12, D-94). Orders (W2-02), the POS
// (W2-04) and basket search (W2-14) all price a cart through Pricer, so a
// quote, a bill and a basket total agree to the paisa. The interface is the
// whole contract: callers never see an offer row.
//
// What Price does, in this order:
//
//  1. Every line gets its one best item-level offer (percent, flat or buy X
//     get Y), the one that takes the most off; ties go to the older offer.
//  2. The item subtotal is the sum of the line totals after step 1.
//  3. The one best minimum-bill offer whose threshold the item subtotal
//     reaches (inclusive, D-94) takes its amount off the item subtotal.
//  4. The bill never goes below ₹1: the bill discount gives way first, then
//     line discounts from the last line backwards.
//
// The delivery fee and credits come after, in the caller (orders, 01 §5.5).
// Price never writes: it consumes no redemption. Placing the order or ringing
// the sale calls Redeemer.Redeem in its own transaction.

// Channel is where a cart is priced.
type Channel string

// The channels of an offer (offers.channels: online, pos or both).
const (
	ChannelOnline Channel = "online"
	ChannelPOS    Channel = "pos"
)

// Line is one cart line.
type Line struct {
	StoreItemID    ids.ID
	UnitPricePaise int64
	Quantity       int
	// Fixed marks a price that is not the catalogue price (an ask reply,
	// D-101): no item-level offer applies to the line. It still counts in the
	// bill the minimum-bill offer is compared with.
	Fixed bool
}

// Cart is what Price prices.
type Cart struct {
	StoreID ids.ID
	// CustomerID is the signed-in or linked customer; the zero ID is "nobody
	// known". An offer with a per-customer limit needs a customer, so without
	// one it does not apply.
	CustomerID ids.ID
	Channel    Channel
	Lines      []Line
	// At is the time to price at; the zero time is the service clock's now.
	// Expiry and scheduling read this time, never the system clock.
	At time.Time
}

// Applied is one offer that took money off, with what it took in total.
type Applied struct {
	OfferID       ids.ID
	Title         string
	Kind          Kind
	DiscountPaise int64
}

// LineQuote is the result for one cart line, in cart order.
type LineQuote struct {
	GrossPaise    int64
	DiscountPaise int64
	// OfferID is the offer that discounted the line; the zero ID when none.
	OfferID ids.ID
	// FreeUnits is how many units a buy-X-get-Y offer gave free.
	FreeUnits int
}

// Quote is what Price returns. Every amount is in paise.
type Quote struct {
	Lines []LineQuote
	// GrossPaise is the lines at list price.
	GrossPaise int64
	// ItemDiscountPaise is the item-level offers' total.
	ItemDiscountPaise int64
	// ItemSubtotalPaise is the gross less the item-level offers, before the
	// bill offer and before credits: the D-94 base for the delivery minimum,
	// free delivery and the minimum-bill offer.
	ItemSubtotalPaise int64
	// BillDiscountPaise is the minimum-bill offer's amount; BillOffer names it.
	BillDiscountPaise int64
	BillOffer         *Applied
	// TotalPaise is the item subtotal less the bill discount: at least 100
	// (₹1) whenever the gross is.
	TotalPaise int64
	// Applied lists each offer that took money off once: the item-level
	// offers in the order their first line appears, then the bill offer.
	Applied []Applied
}

// DiscountPaise is everything the offers took off.
func (q Quote) DiscountPaise() int64 { return q.ItemDiscountPaise + q.BillDiscountPaise }

// Pricer prices a cart. It is safe for concurrent use.
type Pricer interface {
	Price(ctx context.Context, cart Cart) (Quote, error)
}

// Ref names what a redemption belongs to: exactly one of the two is set.
type Ref struct {
	OrderID   ids.ID
	POSSaleID ids.ID
}

// Redeemer uses and gives back offer redemptions. Both run in the caller's
// transaction (the context's), so an order that does not commit uses nothing.
type Redeemer interface {
	// Redeem records the quote's offers against ref and counts them against
	// their limits. It returns ErrUnavailable when an offer ran out between
	// the quote and now; the caller prices again and tells the customer.
	Redeem(ctx context.Context, ref Ref, customerID ids.ID, q Quote) error
	// Release gives back every redemption of ref (cancel, reject, expiry,
	// reversal). Releasing twice, or a ref that redeemed nothing, is a no-op.
	Release(ctx context.Context, ref Ref) error
}
