package referrals

import (
	"context"
	"errors"

	"github.com/todayz/backend/internal/platform/ids"
)

// The seams to other domains. Referrals depends on no other domain's
// package: credits (W2-01) implements Ledger, and orders and POS (W2-04)
// call OnPurchaseCompleted / OnPurchaseReversed through small interfaces of
// their own (cmd/todayz/routes_referrals.go adapts them).

// Beneficiary kinds: a customer wallet (owner = the user) or a store's
// credit account (owner = the store).
const (
	BeneficiaryCustomer = "customer"
	BeneficiaryStore    = "store"
)

// Beneficiary is the account a reward is paid into.
type Beneficiary struct {
	Kind string // BeneficiaryCustomer or BeneficiaryStore
	ID   ids.ID
}

// Sides of a referral.
const (
	SideReferrer = "referrer"
	SideReferee  = "referee"
)

// Reward is one side's referral reward (01 §8). It is funded by TodayZ
// (platform_marketing) and posted once: IdempotencyKey is
// "referral:<referral id>:<side>", so a retry returns the first posting.
type Reward struct {
	ReferralID     ids.ID
	Side           string // SideReferrer or SideReferee
	To             Beneficiary
	Credits        int
	IdempotencyKey string
}

// Ledger posts referral rewards. PostReward runs inside the side's own
// posting transaction (ctx carries it, db.TxFrom), never a purchase's: the
// referral_reward job and the sweep call it after the purchase committed
// (D-169), and it must take its accounts' locks in one call. It returns the
// ledger transaction id; a repeat of the same key returns the first
// posting's id. posted false with a nil error means the ledger did not post
// this time: like an error, the side's transaction rolls back, the failure
// is counted in tz_referral_reward_failed_total and the side is retried
// with the backoff. An error wrapping ErrRewardRefused is permanent: the
// referral gets a referral.reward_refused risk flag and the sweep leaves it
// until a person clears the flag.
//
// W2-01's credits.Poster is the Ledger through jobs.ReferralsLedger, which
// the API and the sweep both get from jobs.ReferralsService.
type Ledger interface {
	PostReward(ctx context.Context, r Reward) (txID ids.ID, posted bool, err error)
}

// ErrRewardRefused marks a posting the ledger will never accept as it is
// (an idempotency key reused for other legs, an invalid posting): retrying
// cannot help, so the side waits for a person (D-169).
var ErrRewardRefused = errors.New("referrals: reward refused by the ledger")

// Purchase kinds a referral can be qualified by (referrals.qualifying_ref_type).
const (
	RefOrder   = "order"
	RefPOSSale = "pos_sale"
)

// POS link methods (D-07). Only a verified link (bill_qr, counter_code)
// qualifies a referral; phone_unverified never does.
const (
	LinkBillQR          = "bill_qr"
	LinkCounterCode     = "counter_code"
	LinkPhoneUnverified = "phone_unverified"
)

// Outcome is a completed purchase: an order that reached collected or
// delivered, or a POS sale. The caller passes it inside the transaction
// that records the outcome; the rewards post after it commits (D-169).
type Outcome struct {
	RefType string // RefOrder or RefPOSSale
	RefID   ids.ID
	// CustomerID is the buyer; ids.Zero for an unlinked POS sale.
	CustomerID ids.ID
	StoreID    ids.ID
	// EligiblePaise is the amount cashback and referrals count (items after
	// offers, before credits); CreditsUsed is the credits spent on it.
	// Qualifying amount = EligiblePaise − CreditsUsed × 100 (D-32).
	EligiblePaise int64
	CreditsUsed   int
	// LinkMethod is how a POS sale was linked to the customer
	// (LinkBillQR, LinkCounterCode, LinkPhoneUnverified); "" for an order.
	LinkMethod string
}
