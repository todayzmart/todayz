package orders

import (
	"context"
	"errors"
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
	// Fixed marks an ask reply's price: no item offer applies to the line
	// (D-101).
	Fixed bool
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

// ErrOfferGone is what a Redeemer returns when an offer ran out between the
// quote and the order; the checkout prices again and answers price_changed.
var ErrOfferGone = errors.New("orders: an offer is no longer available")

// Redeemer counts the offers an order used against their limits, in the
// order's transaction (W2-07). Release gives them back when the order ends
// without being collected.
type Redeemer interface {
	Redeem(ctx context.Context, orderID, customerID ids.ID, applied []AppliedOffer) error
	Release(ctx context.Context, orderID ids.ID) error
}

// NoRedeem is the Redeemer when there are no offers: nothing to count.
type NoRedeem struct{}

// Redeem implements Redeemer.
func (NoRedeem) Redeem(context.Context, ids.ID, ids.ID, []AppliedOffer) error { return nil }

// Release implements Redeemer.
func (NoRedeem) Release(context.Context, ids.ID) error { return nil }

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

// CreditsHold holds credits for one order: EligiblePaise is the items after
// offers (D-02).
type CreditsHold struct {
	OrderID, CustomerID, StoreID ids.ID
	Credits                      int
	EligiblePaise                int64
}

// Credits is the credits ledger as orders uses it (W2-01). Hold, Release and
// Complete run in the order's transaction and are idempotent per order.
//
// The delivery WP must call Complete on delivered and Release on
// delivery_failed (D-127): no transition reaches either yet, so a delivery
// order's hold is neither captured nor released and earns no cashback.
type Credits interface {
	// Allow answers how many credits a checkout may use; it writes nothing.
	Allow(ctx context.Context, customerID, storeID ids.ID, requested int, itemsAfterOffersPaise int64) (CreditsAllowance, error)
	// Hold holds all of h.Credits or none: a refusal is Allowed 0 and the
	// Limit, with a nil error.
	Hold(ctx context.Context, h CreditsHold) (CreditsAllowance, error)
	// Release returns an order's held credits (any end but collection).
	Release(ctx context.Context, orderID ids.ID) error
	// Complete captures the hold to the store and pays cashback at the
	// rate the order snapshotted when it was placed (cashbackBP, D-124); it
	// returns the cashback credits.
	Complete(ctx context.Context, orderID, customerID, storeID ids.ID, itemsAfterOffersPaise int64, cashbackBP int) (int, error)
}

// NoCredits is Credits when credits are not live: none may be used.
type NoCredits struct{}

// Allow implements Credits.
func (NoCredits) Allow(context.Context, ids.ID, ids.ID, int, int64) (CreditsAllowance, error) {
	return CreditsAllowance{Limit: "not_available"}, nil
}

// Hold implements Credits.
func (NoCredits) Hold(context.Context, CreditsHold) (CreditsAllowance, error) {
	return CreditsAllowance{Limit: "not_available"}, nil
}

// Release implements Credits.
func (NoCredits) Release(context.Context, ids.ID) error { return nil }

// Complete implements Credits.
func (NoCredits) Complete(context.Context, ids.ID, ids.ID, ids.ID, int64, int) (int, error) {
	return 0, nil
}

// CompletedOrder is an order that reached collected (or delivered), for
// referral qualification (W2-03). EligiblePaise is items after offers;
// CreditsUsed the credits spent on it.
type CompletedOrder struct {
	OrderID, CustomerID, StoreID ids.ID
	EligiblePaise                int64
	CreditsUsed                  int
}

// Referrals is told of a completed order inside the order's transaction
// (01 §8: customer rewards post in the same transaction). An error rolls the
// handover back.
type Referrals interface {
	OnOrderCompleted(ctx context.Context, o CompletedOrder) error
}

// NoReferrals is Referrals when the referrals domain is not wired.
type NoReferrals struct{}

// OnOrderCompleted implements Referrals.
func (NoReferrals) OnOrderCompleted(context.Context, CompletedOrder) error { return nil }
