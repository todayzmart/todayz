package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/notify"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/referrals"
)

// KindReferralSweep posts referral rewards that are due but not yet posted
// (a ledger that was down, a shop with no live store yet) and qualifies new
// shops whose verification came after their last counted sale (W2-03).
const KindReferralSweep = "referral_sweep"

// registerreferrals declares the referrals domain's job kinds.
func registerreferrals(r *Registry, d Deps) {
	// The "referral unlocked" notices enqueue sends, so the enqueuer is
	// built on the first run (as the orders timers do).
	enqueuer := lazyEnqueuer(r, d, "referral sweep")
	svc := func() (*referrals.Service, error) {
		enq, err := enqueuer()
		if err != nil {
			return nil, err
		}
		nd := notify.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log, Jobs: enq}
		if d.Pool != nil {
			pool := db.New(d.Pool)
			nd.DB, nd.Staff = pool, business.NewStaffResolver(pool)
		}
		ns, err := notify.New(nd)
		if err != nil {
			return nil, err
		}
		return ReferralsService(d, ns)
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindReferralSweep, Owner: "W2-03", Class: platformjobs.ClassMaintenance, Runbook: ledgerRunbook,
		Description: "Posts due referral rewards that have not posted yet and qualifies new shops that reached their count",
		Schedule:    "*/10 * * * *", UniqueBy: platformjobs.PerTick, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			s, err := svc()
			if err != nil {
				return platformjobs.Result{}, err
			}
			out, err := s.Sweep(ctx)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: out}, nil
		},
	})
}

// ReferralsService builds the referrals service the API and the sweep both
// use, so both post rewards through the same ledger (D-169). The hasher is
// nil without PII_HASH_KEY (every route then answers 503).
func ReferralsService(d Deps, ns notice.Sender) (*referrals.Service, error) {
	rd := referrals.Deps{Clock: d.Clock, Settings: d.Settings, Notices: ns}
	if d.Pool != nil {
		rd.DB = db.New(d.Pool)
	}
	if cfg := d.Config; cfg != nil {
		if cfg.Keys.PIIHashKey.IsSet() {
			if h, err := pii.NewHasher(cfg.Keys.PIIHashKey.Bytes()); err == nil {
				rd.Hasher = h
			}
		}
		rd.PublicURL = cfg.APIPublicURL
		if o := cfg.CORSOrigins(); len(o) >= 2 {
			rd.CustomerApp, rd.BusinessApp = o[0], o[1]
		}
	}
	cs, err := CreditsService(d, ns)
	if err != nil {
		return nil, err
	}
	rd.Ledger = ReferralsLedger(cs)
	return referrals.New(rd)
}

// ReferralsLedger is the credits ledger as referrals.Ledger: a reward is one
// TxReferralReward posting from platform_marketing to the customer's wallet
// or the store's store_credit, keyed by the reward's idempotency key
// ("referral:<id>:<side>"). Rewards are whole credits, as ledger amounts are
// (01 §7: 1 credit = ₹1), so the amount moves as it is.
func ReferralsLedger(p credits.Poster) referrals.Ledger { return referralsLedger{p: p} }

type referralsLedger struct{ p credits.Poster }

func (l referralsLedger) PostReward(ctx context.Context, r referrals.Reward) (ids.ID, bool, error) {
	var to credits.Account
	switch r.To.Kind {
	case referrals.BeneficiaryCustomer:
		to = credits.Account{Kind: credits.CustomerWallet, Owner: r.To.ID}
	case referrals.BeneficiaryStore:
		to = credits.Account{Kind: credits.StoreCredit, Owner: r.To.ID}
	default:
		return ids.Zero, false, fmt.Errorf("%w: beneficiary kind %q", referrals.ErrRewardRefused, r.To.Kind)
	}
	amt := int64(r.Credits)
	p, err := l.p.Post(ctx, credits.Posting{
		Kind: credits.TxReferralReward, RefType: "referral", RefID: r.ReferralID,
		IdempotencyKey: r.IdempotencyKey, Memo: "referral reward: " + r.Side,
		Legs: []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -amt}, {Account: to, Amount: amt}},
	})
	switch {
	case errors.Is(err, credits.ErrKeyReused), errors.Is(err, credits.ErrInvalid):
		// Retrying the same posting can never succeed: park it (D-169).
		return ids.Zero, false, fmt.Errorf("%w: %w", referrals.ErrRewardRefused, err)
	case err != nil:
		return ids.Zero, false, err
	}
	// A duplicate is the first posting of this key: the reward is posted.
	return p.TransactionID, true, nil
}
