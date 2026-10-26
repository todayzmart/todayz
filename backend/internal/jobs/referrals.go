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
	"github.com/todayz/backend/internal/risk"
)

// KindReferralSweep posts referral rewards that are due but not yet posted
// (a job that never ran, a side whose retry backoff has passed: a ledger
// that was down, a shop with no live store yet) and qualifies new shops
// whose verification came after their last counted sale (W2-03).
const KindReferralSweep = "referral_sweep"

// referralRewardAttempts: a run that hits a database error retries, then the
// sweep takes over. A ledger failure is not one: PostRewards records the
// attempt and the run succeeds.
const referralRewardAttempts = 3

// registerreferrals declares the referrals domain's job kinds.
func registerreferrals(r *Registry, d Deps) {
	// The "referral unlocked" notices enqueue sends and a shop the sweep
	// qualifies enqueues its referral_reward, so the enqueuer is built on the
	// first run (as the orders timers do).
	enqueuer := lazyEnqueuer(r, d, "referrals jobs")
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
		return ReferralsService(d, ns, enq)
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: referrals.KindReward, Owner: "W2-03", Class: platformjobs.ClassMoney, Runbook: ledgerRunbook,
		Description: "Posts a qualified referral's rewards after the purchase commits, each side in its own transaction (D-169)",
		Timeout:     30 * time.Second, MaxAttempts: referralRewardAttempts,
		Handler: func(ctx context.Context, rn platformjobs.Run) (platformjobs.Result, error) {
			var args referrals.RewardArgs
			if err := rn.Decode(&args); err != nil {
				return platformjobs.Result{}, err
			}
			s, err := svc()
			if err != nil {
				return platformjobs.Result{}, err
			}
			res, err := s.PostRewards(ctx, args.ReferralID)
			if err != nil {
				return platformjobs.Result{}, err
			}
			waiting := int64(0)
			if res.Waiting {
				waiting = 1
			}
			return platformjobs.Result{Summary: map[string]int64{"posted": int64(res.Posted), "waiting": waiting}}, nil
		},
	})
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindReferralSweep, Owner: "W2-03", Class: platformjobs.ClassMaintenance, Runbook: ledgerRunbook,
		Description: "Posts due referral rewards that have not posted yet and qualifies new shops that reached their count",
		Schedule:    "*/10 * * * *", UniqueBy: platformjobs.PerTick, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			s, err := svc()
			if err != nil {
				return platformjobs.Result{}, err
			}
			// An error is one referral's: the others were still swept.
			out, err := s.Sweep(ctx)
			return platformjobs.Result{Summary: out}, err
		},
	})
}

// ReferralsService builds the referrals service the API and the referrals
// jobs all use, so all post rewards through the same ledger (D-169). enq
// enqueues referral_reward in the qualifying purchase's transaction (nil:
// the sweep posts). The hasher is nil without PII_HASH_KEY (every route
// then answers 503).
func ReferralsService(d Deps, ns notice.Sender, enq platformjobs.Enqueuer) (*referrals.Service, error) {
	rd := referrals.Deps{Clock: d.Clock, Settings: d.Settings, Notices: ns, Jobs: enq}
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
	// The reward pass runs referral.self_like before it posts (D-237).
	rs, err := risk.New(risk.Deps{DB: rd.DB, Clock: d.Clock, Settings: d.Settings})
	if err != nil {
		return nil, err
	}
	rd.SelfLike = rs
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
