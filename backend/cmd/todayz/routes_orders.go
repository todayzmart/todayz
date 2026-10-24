package main

import (
	"io"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// orderCodeRand replaces crypto/rand for pickup codes. It is nil in the
// binary; only tests set it (to force a code collision).
var orderCodeRand io.Reader

// mountOrders mounts W2-02's routes: checkout quote and create, the
// customer's orders and actions, the store's order inbox and actions, and
// the admin cancel (/v1/admin/orders/{orderId}/cancel, pulled forward from
// W3-05; there is no routes_admin-orders.go).
// It also registers the orders deletion check (BK-44: no account deletion
// while an order is open) with identity.
func mountOrders(r chi.Router, d *deps) {
	od := orders.Deps{
		Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings,
		Notices: newNotifyService(d), Stock: jobs.OrdersStock(d.Clock), Open: business.OpenAt,
		CodeRand: orderCodeRand, Credits: jobs.OrdersCredits(creditsService(d)),
	}
	if d.DB != nil {
		var hashKey []byte
		if d.Config != nil {
			hashKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		// The admin cancel's audit row (W2-02 finish, D-83).
		od.Audit = audit.New(d.DB, d.Clock, hashKey)
	}
	// Offers price the quote and are counted in the order's transaction (W2-07).
	pricing := offersForOrders{newOffers(d)}
	od.Pricer, od.Redeemer = pricing, pricing
	// A collected order may qualify a referral (W2-03).
	od.Referrals = referralsForOrders{newReferrals(d)}
	if d.Queue != nil {
		// The timers are enqueued in the order's transaction through the
		// API's insert-only River client; without one (router tests) every
		// status change answers 503.
		reg := platformjobs.NewRegistry()
		jd := jobs.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log}
		if d.DB != nil {
			jd.Pool = d.DB.Pool
		}
		jobs.Register(reg, jd)
		od.Jobs = platformjobs.NewEnqueuer(reg, d.Queue, d.Clock)
	}
	svc, err := orders.New(od)
	if err != nil {
		// A missing clock or a malformed order-code or PII key; boot and
		// buildSecurity refuse those before the routes mount.
		panic("orders: " + err.Error())
	}
	var az authz.Authorizer
	if d.Authz != nil {
		az = d.Authz
	}
	orders.Register(r, svc, az)
	if d.Identity != nil {
		d.Identity.AddDeletionCheck(svc)
	}
}
