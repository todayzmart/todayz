package main

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/referrals"
)

// referralsDeps is what both the API and the sweep job build the referrals
// service from. The hasher is nil without PII_HASH_KEY (every route then
// answers 503). Ledger is left nil (NoLedger): rewards qualify and wait.
// TODO(merge): when W2-01 credits lands, set Ledger to an adapter over
// credits.Poster (Posting{Kind: TxReferralReward, legs PlatformMarketing →
// CustomerWallet / StoreCredit, IdempotencyKey from the Reward}).
func referralsDeps(cfg *config.Config) referrals.Deps {
	var rd referrals.Deps
	if cfg == nil {
		return rd
	}
	if cfg.Keys.PIIHashKey.IsSet() {
		if h, err := pii.NewHasher(cfg.Keys.PIIHashKey.Bytes()); err == nil {
			rd.Hasher = h
		}
	}
	rd.PublicURL = cfg.APIPublicURL
	if o := cfg.CORSOrigins(); len(o) >= 2 {
		rd.CustomerApp, rd.BusinessApp = o[0], o[1]
	}
	return rd
}

// newReferrals builds the referrals service the API uses.
func newReferrals(d *deps) *referrals.Service {
	rd := referralsDeps(d.Config)
	rd.DB, rd.Clock, rd.Settings = d.DB, d.Clock, d.Settings
	rd.Notices = newNotifyService(d)
	svc, err := referrals.New(rd)
	if err != nil {
		panic("referrals: " + err.Error())
	}
	return svc
}

// referralsForOrders adapts referrals to orders.Referrals: a collected order
// may qualify the buyer's referral and counts toward a new shop's (W2-03).
type referralsForOrders struct{ s *referrals.Service }

func (a referralsForOrders) OnOrderCompleted(ctx context.Context, o orders.CompletedOrder) error {
	return a.s.OnPurchaseCompleted(ctx, referrals.Outcome{
		RefType: referrals.RefOrder, RefID: o.OrderID, CustomerID: o.CustomerID, StoreID: o.StoreID,
		EligiblePaise: o.EligiblePaise, CreditsUsed: o.CreditsUsed,
	})
}

// mountReferrals mounts W2-03's routes: the public link and standee landing
// (/v1/public/ref/{code}), the customer's /v1/me/referrals and claim, and the
// business's /v1/biz/businesses/{bid}/referrals and claim. It also closes a
// deleted user's pending referral through identity's deletion hook.
func mountReferrals(r chi.Router, d *deps) {
	svc := newReferrals(d)
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	referrals.Register(r, svc, az)
	if d.Identity != nil {
		d.Identity.AddDeletionHook(identity.OnDeleteFunc(svc.OnAccountDelete))
	}
}
