package pos

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/internal/platform/ids"
)

// The seams other work packages fill: the catalogue's stock writer (wired by
// jobs.PosStock), the offers engine (W2-07), credits (W2-01) and referrals.
// Each but Stock has a default that does nothing, so the counter works
// before they land. Every seam runs in the transaction carried in ctx.

// --- stock ---------------------------------------------------------------------

// StockSale is one catalogued sale line: a sale_pos movement of Qty units,
// idempotent on ClientID (the line's client id).
type StockSale struct {
	StoreID, ItemID ids.ID
	Qty             int32
	SaleID          ids.ID // the movement's ref (pos_sale)
	ClientID        ids.ID
	UserID          ids.ID
	DeviceID        ids.ID // zero unless a devices row
}

// StockReversal puts a sale line's units back (a pos_reversal movement).
type StockReversal struct {
	StoreID    ids.ID
	MovementID ids.ID // the line's sale_pos movement
	Qty        int32
	UserID     ids.ID
	Note       *string
}

// Stock change kinds a sync entry may carry.
const (
	ChangeCount   = "count"
	ChangeRestock = "restock"
	ChangeDamage  = "damage"
	ChangeExpired = "expired"
)

// StockChange is an offline stock entry: a count (Qty is the counted
// on-hand) or a restock, damage or expired movement of Qty units. Damage
// and expired are not capped at what is available: the device saw them
// happen (02 §4.3).
type StockChange struct {
	Kind            string
	StoreID, ItemID ids.ID
	Qty             int32
	ClientID        ids.ID
	UserID          ids.ID
	DeviceID        ids.ID
	Note            *string
}

// StockResult is what a change wrote. Wrote is false for a count that
// matched on-hand (nothing to write); Replayed is true when ClientID was
// already written.
type StockResult struct {
	MovementID ids.ID
	Wrote      bool
	Replayed   bool
}

// Errors a Stock returns (the adapter maps the catalogue's).
var (
	ErrItemNotFound   = errors.New("pos: no such item in the store")
	ErrClientIDReused = errors.New("pos: client id already used for another movement")
	ErrStockInvalid   = errors.New("pos: stock change out of range")
)

// Stock writes the stock side of the counter.
type Stock interface {
	Sell(ctx context.Context, tx pgx.Tx, in StockSale) (movementID ids.ID, err error)
	Reverse(ctx context.Context, tx pgx.Tx, in StockReversal) (movementID ids.ID, err error)
	Change(ctx context.Context, tx pgx.Tx, in StockChange) (StockResult, error)
}

// --- offers --------------------------------------------------------------------

// PriceLine is one catalogued bill line as the offers engine sees it.
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

// Pricing is what the offers engine took off: per line in the order given,
// then off the bill. The service clamps both, so a bad pricer can never make
// a line or the bill negative.
type Pricing struct {
	LineDiscounts []int64
	BillDiscount  int64
	Offers        []AppliedOffer
}

// Pricer applies the store's offers to a counter sale. customerID is zero
// for a walk-in (the link is not known when the bill is priced). at is the
// time to price at: the zero time for an online sale (now), the sale's
// sold_at for an offline one, to check what the device took off (D-131).
type Pricer interface {
	Price(ctx context.Context, customerID, storeID ids.ID, at time.Time, lines []PriceLine) (Pricing, error)
}

// NoOffers is the Pricer until offers land: nothing off.
type NoOffers struct{}

// Price implements Pricer.
func (NoOffers) Price(_ context.Context, _, _ ids.ID, _ time.Time, lines []PriceLine) (Pricing, error) {
	return Pricing{LineDiscounts: make([]int64, len(lines))}, nil
}

// ErrOfferGone is what a Redeemer returns when an offer ran out between
// pricing the bill and writing the sale; the sale is not written.
var ErrOfferGone = errors.New("pos: an offer is no longer available")

// Redeemer records the offers an online sale used against their limits and
// gives them back when the sale is reversed. Both run in the sale's
// transaction (the context's).
type Redeemer interface {
	Redeem(ctx context.Context, saleID, customerID ids.ID, applied []AppliedOffer) error
	Release(ctx context.Context, saleID ids.ID) error
}

// NoRedeem is the Redeemer without offers: nothing to count.
type NoRedeem struct{}

// Redeem implements Redeemer.
func (NoRedeem) Redeem(context.Context, ids.ID, ids.ID, []AppliedOffer) error { return nil }

// Release implements Redeemer.
func (NoRedeem) Release(context.Context, ids.ID) error { return nil }

// --- credits -------------------------------------------------------------------

// CreditsAllowance is how many credits a bill may use. Limit names why it
// is capped (a BillClaimPreviewCreditsLimit value), "" when it is not.
type CreditsAllowance struct {
	Allowed int
	Limit   string
}

// Cashback is the cashback a linked sale earns (D-07).
type Cashback struct {
	CustomerID, StoreID, SaleID ids.ID
	Credits                     int
}

// Credits is the customer's credit ledger (W2-01).
type Credits interface {
	// Allow is how many of requested credits the customer may use on a
	// bill of eligiblePaise at the store (balance, per-order, monthly and
	// store caps).
	Allow(ctx context.Context, customerID, storeID ids.ID, requested int, eligiblePaise int64) (CreditsAllowance, error)
	// Hold reserves credits for a confirmed bill claim and returns how many
	// it held (at most credits). Holding the same claim again returns the
	// first hold.
	Hold(ctx context.Context, customerID, storeID, claimID ids.ID, credits int) (held int, err error)
	// Capture spends a claim's hold on the sale it was bound to. It must
	// fail (not do nothing) for a hold that was released, so a sale never
	// takes credits the customer got back; a claim with no hold is a no-op.
	Capture(ctx context.Context, claimID, saleID ids.ID) error
	// Release returns a claim's hold that no sale used: the claim was
	// superseded by a newer claim for the bill, another claim was bound, or
	// it expired (D-131). A claim with no hold, or released before, is a
	// no-op.
	Release(ctx context.Context, claimID ids.ID) error
	// Cashback posts a linked sale's cashback and returns what it posted.
	Cashback(ctx context.Context, in Cashback) (posted int, err error)
	// ReverseSale returns the credits a reversed sale spent and takes back
	// its cashback.
	ReverseSale(ctx context.Context, saleID, customerID ids.ID) error
}

// NoCredits is Credits until credits are live: none may be used and no
// cashback is posted.
type NoCredits struct{}

// Allow implements Credits.
func (NoCredits) Allow(context.Context, ids.ID, ids.ID, int, int64) (CreditsAllowance, error) {
	return CreditsAllowance{Limit: "not_available"}, nil
}

// Hold implements Credits.
func (NoCredits) Hold(context.Context, ids.ID, ids.ID, ids.ID, int) (int, error) { return 0, nil }

// Capture implements Credits.
func (NoCredits) Capture(context.Context, ids.ID, ids.ID) error { return nil }

// Release implements Credits.
func (NoCredits) Release(context.Context, ids.ID) error { return nil }

// Cashback implements Credits.
func (NoCredits) Cashback(context.Context, Cashback) (int, error) { return 0, nil }

// ReverseSale implements Credits.
func (NoCredits) ReverseSale(context.Context, ids.ID, ids.ID) error { return nil }

// --- referrals -----------------------------------------------------------------

// Referrals qualifies a referred customer's first purchase (the referrals
// WP). The service calls Qualify only for a bill_qr or counter_code sale of
// at least RefQualifyingMinOrderPaise after discounts and credits.
type Referrals interface {
	Qualify(ctx context.Context, customerID, saleID ids.ID, qualifyingPaise int64) error
	// Revoke undoes a qualification when the sale is reversed; a reward
	// already paid is the referrals WP's to flag.
	Revoke(ctx context.Context, customerID, saleID ids.ID) error
}

// NoReferrals is Referrals until referrals land.
type NoReferrals struct{}

// Qualify implements Referrals.
func (NoReferrals) Qualify(context.Context, ids.ID, ids.ID, int64) error { return nil }

// Revoke implements Referrals.
func (NoReferrals) Revoke(context.Context, ids.ID, ids.ID) error { return nil }
