// Package credits is the credits ledger (W2-01; 01 §7, §9; 03 §6; 04
// /v1/me/wallet and {B}/credit-account): double-entry accounts, holds for
// orders and bill claims, FIFO lots with expiry, cashback, the spend caps,
// the account-deletion forfeiture and the nightly reconcile.
//
// Two small interfaces are the whole surface other packages build on:
//
//   - Holder: what a checkout does with credits (orders W2-02, bill claims
//     W2-04). Allow answers without writing; Hold, Reduce, Release and
//     Complete run in the caller's transaction (db.TxFrom(ctx)).
//   - Poster: one balanced posting of kind referral_reward (W2-03),
//     admin_adjustment (W3-01) or payout (W3). Spend, cashback, expiry and
//     forfeiture postings are this package's own and go through Holder,
//     the jobs and OnAccountDelete.
//
// Rules this package keeps (and the database keeps again, see migration
// 20101):
//
//   - Every posting is one ledger_transactions row whose entries sum to 0;
//     the entries move account_balances in the same statement.
//   - Every posting locks the account_balances rows it touches FOR UPDATE,
//     in account id order, and checks customer_wallet and customer_held
//     there: they never go below 0.
//   - Every credit into a customer_wallet creates a lot; spending and debits
//     consume lots by earliest expires_at (then id). A release goes back to
//     the same lots; the part on a lot whose expires_at has passed goes to
//     platform_expired at once.
//   - A repeat of a posting (same idempotency key) returns the first and
//     posts nothing.
package credits

import (
	"context"
	"errors"

	"github.com/todayz/backend/internal/platform/ids"
)

// RefType is what a hold or a posting is for (ledger_transactions.ref_type).
type RefType string

// The hold refs (D-35): exactly one per hold.
const (
	RefOrder     RefType = "order"
	RefBillClaim RefType = "bill_claim"
)

// Ref names one order or one bill claim.
type Ref struct {
	Type RefType
	ID   ids.ID
}

func (r Ref) valid() bool { return (r.Type == RefOrder || r.Type == RefBillClaim) && r.ID != ids.Zero }

func (r Ref) key(op string) string { return op + ":" + string(r.Type) + ":" + r.ID.String() }

// Limit says why a checkout may use fewer credits than it asked for. The
// values are the contract's CreditsLimit enum (orders.yaml).
type Limit string

// The limits, in the order Allow checks them.
const (
	LimitNone         Limit = ""
	LimitNotAvailable Limit = "not_available"        // credits are not live for this checkout
	LimitBelowMinimum Limit = "below_credit_minimum" // eligible amount under credit_min_order_value_paise
	LimitStoreCap     Limit = "store_credit_cap"     // the store's store_credit_redemptions_per_day is reached
	LimitMonthlyCap   Limit = "credit_monthly_cap"   // the customer's credit_orders_per_month is reached
	LimitMaxExceeded  Limit = "credit_max_exceeded"  // over credit_max_per_order, or the ₹1 floor
	LimitNoBalance    Limit = "no_balance"           // not enough available credits
)

// Blocks reports a limit under which no credits at all may be used.
func (l Limit) Blocks() bool {
	return l == LimitNotAvailable || l == LimitBelowMinimum || l == LimitStoreCap || l == LimitMonthlyCap
}

// Allowance is how many credits a checkout may use. Limit is why Allowed is
// below the request ("" when it is not). Max is the most this checkout could
// use whatever the request; Available is the customer's spendable balance.
type Allowance struct {
	Allowed   int
	Max       int
	Available int
	Limit     Limit
}

// HoldRequest holds credits for one order or bill claim. EligiblePaise is
// the eligible amount (D-02): items after offers, before credits, without
// the delivery fee.
type HoldRequest struct {
	Ref           Ref
	CustomerID    ids.ID
	StoreID       ids.ID
	Credits       int
	EligiblePaise int64
}

// Completion settles a ref that completed (collected, delivered or a
// completed bill QR sale): the hold, if any, is captured to the store and
// the store's cashback is paid on EligiblePaise − credits captured × 100.
type Completion struct {
	Ref           Ref
	CustomerID    ids.ID
	StoreID       ids.ID
	EligiblePaise int64
}

// Completed is what Complete posted.
type Completed struct {
	Captured int
	Cashback int
}

// Holder is the checkout side of credits. Hold, Reduce, Release and
// Complete must run inside the caller's transaction; each is idempotent per
// Ref.
type Holder interface {
	// Allow answers how many of requested credits the checkout may use. It
	// writes nothing.
	Allow(ctx context.Context, customerID, storeID ids.ID, requested int, eligiblePaise int64) (Allowance, error)
	// Hold holds req.Credits (> 0) for req.Ref. It holds all of them or
	// none: a refusal returns Allowed 0 and the Limit, with a nil error (a
	// cap refusal also raises a risk flag that survives the caller's
	// rollback). A repeat for a ref already held returns its amount.
	Hold(ctx context.Context, req HoldRequest) (Allowance, error)
	// Reduce lowers the held amount to `to` (a partial accept re-cap, D-28),
	// releasing the difference to the same lots, latest expiry first. to 0
	// is Release. It returns the credits released (0 when nothing changed).
	Reduce(ctx context.Context, ref Ref, to int) (int, error)
	// Release returns the whole hold (any terminal state but completion). A
	// ref with no open hold releases nothing.
	Release(ctx context.Context, ref Ref) (int, error)
	// Complete captures the hold and pays cashback. A repeat posts nothing
	// and returns what the first posted.
	Complete(ctx context.Context, c Completion) (Completed, error)
}

// AccountKind is a ledger account kind (03 §6).
type AccountKind string

// The account kinds.
const (
	CustomerWallet      AccountKind = "customer_wallet"
	CustomerHeld        AccountKind = "customer_held"
	StoreCredit         AccountKind = "store_credit"
	PlatformMarketing   AccountKind = "platform_marketing"
	PlatformExpired     AccountKind = "platform_expired"
	PlatformAdjustments AccountKind = "platform_adjustments"
	PlatformPayoutsCash AccountKind = "platform_payouts_cash"
)

func (k AccountKind) platform() bool {
	return k == PlatformMarketing || k == PlatformExpired || k == PlatformAdjustments || k == PlatformPayoutsCash
}

// Account names an account: Owner is the customer's user id or the store id,
// zero for a platform account.
type Account struct {
	Kind  AccountKind
	Owner ids.ID
}

// TxKind is a ledger transaction kind.
type TxKind string

// The transaction kinds (03 §6).
const (
	TxSpendHold       TxKind = "spend_hold"
	TxSpendCapture    TxKind = "spend_capture"
	TxSpendRelease    TxKind = "spend_release"
	TxCashback        TxKind = "cashback"
	TxReferralReward  TxKind = "referral_reward"
	TxExpiry          TxKind = "expiry"
	TxAdminAdjustment TxKind = "admin_adjustment"
	TxPayout          TxKind = "payout"
	TxReversal        TxKind = "reversal"
	TxForfeitDeletion TxKind = "forfeit_deletion"
)

// Leg is one entry of a posting: + increases the account.
type Leg struct {
	Account Account
	Amount  int64
}

// Posting is one balanced transaction for Poster.Post.
type Posting struct {
	Kind TxKind // referral_reward, admin_adjustment or payout
	// RefType and RefID name what it is for (a referral, an adjustment
	// request, a payout); both or neither.
	RefType string
	RefID   ids.ID
	// IdempotencyKey is unique per posting (≤ 200 characters): a repeat
	// posts nothing and returns the first.
	IdempotencyKey string
	CreatedBy      ids.ID // the admin for an adjustment; zero for the system
	Memo           string
	Legs           []Leg
}

// Posted is the result of Post. Duplicate is true when the key had posted
// before; nothing new was written.
type Posted struct {
	TransactionID ids.ID
	Duplicate     bool
}

// Poster posts one balanced transaction in the caller's transaction.
//
//   - Legs sum to 0, each is non-zero, and an account appears once.
//   - customer_held is never a leg (only holds move it).
//   - A customer_wallet credit creates a lot (source = Kind); a debit
//     consumes lots FIFO and is refused with ErrInsufficient when the
//     available balance is short.
//   - payout moves store_credit and platform_payouts_cash only; a
//     referral_reward is funded by platform_marketing; an admin_adjustment
//     is against platform_adjustments.
type Poster interface {
	Post(ctx context.Context, p Posting) (Posted, error)
}

// Errors.
var (
	// ErrInsufficient: a debit would take a customer_wallet below 0.
	ErrInsufficient = errors.New("credits: not enough available credits")
	// ErrInvalid: a request breaks the rules above (unbalanced legs, a
	// forbidden account or kind, a non-positive amount).
	ErrInvalid = errors.New("credits: invalid request")
	// ErrUnavailable: the service has no database or settings.
	ErrUnavailable = errors.New("credits: not configured")
)

// Rules are the credit settings in force (01 §7, §17).
type Rules struct {
	MaxPerOrder            int   // credit_max_per_order
	MinOrderPaise          int64 // credit_min_order_value_paise
	ExpiryDays             int   // credit_expiry_days
	OrdersPerMonth         int   // credit_orders_per_month
	StoreRedemptionsPerDay int   // store_credit_redemptions_per_day
	ReminderDays           int   // credit_expiry_reminder_days
}

// usage is how many credit-using holds count against the caps now.
type usage struct {
	CustomerMonth int // the customer's held or captured holds this IST month
	StoreDay      int // the store's held or captured holds this IST day
}
