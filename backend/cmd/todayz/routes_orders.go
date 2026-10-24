package main

import (
	"io"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/authz"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// orderCodeRand replaces crypto/rand for pickup codes. It is nil in the
// binary; only tests set it (to force a code collision).
var orderCodeRand io.Reader

// mountOrders mounts W2-02's routes: checkout quote and create, the
// customer's orders and actions, and the store's order inbox and actions.
// It also registers the orders deletion check (BK-44: no account deletion
// while an order is open) with identity.
func mountOrders(r chi.Router, d *deps) {
	od := orders.Deps{
		Config: d.Config, DB: d.DB, Clock: d.Clock, Log: d.Log, Settings: d.Settings,
		Notices: newNotifyService(d), Stock: jobs.OrdersStock(d.Clock), Open: business.OpenAt,
		CodeRand: orderCodeRand,
	}
	// Offers price the quote and are counted in the order's transaction (W2-07).
	pricing := offersForOrders{newOffers(d)}
	od.Pricer, od.Redeemer = pricing, pricing
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
