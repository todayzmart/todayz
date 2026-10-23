package orders

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// The seams later work packages fill. Each has a default that keeps W2-02's
// behaviour: no offers, no ask-reply prices, no credits.

// PriceLine is one checkout line as the offers engine sees it.
type PriceLine struct {
	StoreItemID, ProductID ids.ID
	UnitPricePaise         int64
	Quantity               int
}

// AppliedOffer is one offer that took money off the bill.
type AppliedOffer struct {
	OfferID       ids.ID
	Title         string
	DiscountPaise int64
}

// Pricing is what the offers engine took off. LineDiscounts is per line, in
// the order of the lines given; BillDiscount comes off the bill after them.
// The service clamps both to what they discount, so a bad pricer can never
// make a line or the bill negative.
type Pricing struct {
	LineDiscounts []int64
	BillDiscount  int64
	Offers        []AppliedOffer
}

// Pricer applies the store's offers to a checkout (W3 offers).
type Pricer interface {
	Price(ctx context.Context, customerID, storeID ids.ID, lines []PriceLine) (Pricing, error)
}

// NoOffers is the Pricer until offers land: nothing off.
type NoOffers struct{}

// Price implements Pricer.
func (NoOffers) Price(_ context.Context, _, _ ids.ID, lines []PriceLine) (Pricing, error) {
	return Pricing{LineDiscounts: make([]int64, len(lines))}, nil
}

// AskPrice is a shop's replied price for one item (D-101).
type AskPrice struct {
	StoreItemID    ids.ID
	UnitPricePaise int64
	ValidUntil     time.Time
}

// AskPricer reads an ask reply's price (W3-16). ok is false when the reply
// is not the customer's, not for the store, not a Yes, or past ValidUntil.
type AskPricer interface {
	AskPrice(ctx context.Context, customerID, storeID, askReplyID ids.ID, now time.Time) (p AskPrice, ok bool, err error)
}

// CreditsAllowance is how many credits a checkout may use. Limit names why
// it is capped (a CreditsLimit value), "" when it is not.
type CreditsAllowance struct {
	Allowed int
	Limit   string
}

// Credits decides how many credits a checkout may use (the credits WP). It
// only answers; holding and settling credits arrive with that WP.
type Credits interface {
	Allow(ctx context.Context, customerID, storeID ids.ID, requested int, itemsAfterOffersPaise int64) (CreditsAllowance, error)
}

// NoCredits is Credits until credits are live: none may be used.
type NoCredits struct{}

// Allow implements Credits.
func (NoCredits) Allow(context.Context, ids.ID, ids.ID, int, int64) (CreditsAllowance, error) {
	return CreditsAllowance{Limit: "not_available"}, nil
}
