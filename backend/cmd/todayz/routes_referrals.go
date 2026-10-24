package main

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/referrals"
)

// newReferrals builds the referrals service the API uses: the same
// constructor as the sweep job, so both post rewards to the credits ledger
// (D-169).
func newReferrals(d *deps) *referrals.Service {
	jd := jobs.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
	if d.DB != nil {
		jd.Pool = d.DB.Pool
	}
	svc, err := jobs.ReferralsService(jd, newNotifyService(d))
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
