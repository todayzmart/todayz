package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/notify"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/queue"
)

// The orders timers run up to this many attempts. An attempt that runs
// before its deadline (queue and clock skew) fails and is retried, and a
// timer kind is never retried by hand (D-95), so the cap is generous.
const ordersTimerAttempts = 25

// registerorders declares the orders timers (W2-02): the accept timeout
// (BK-06), the pickup no-show (BK-14) and the partial-confirm timeout
// (BK-10). All are event-triggered: create, accept (and extend) enqueue them in the order's transaction. A late or
// repeated run finds the order moved on and does nothing (BK-21). The
// reconcile sweep runs every minute and applies the same outcomes to an
// order whose timer was lost or discarded.
func registerorders(r *Registry, d Deps) {
	// The timers' own enqueuer (the notices they write enqueue sends) is
	// built on the first run, not here: the API registers these kinds only
	// to name them (A-21, and its own enqueuer over d.Queue), so it never
	// opens a client for them.
	enqueuer := lazyEnqueuer(r, d, "orders timers")
	svc := func() (*orders.Service, error) {
		enq, err := enqueuer()
		if err != nil {
			return nil, err
		}
		nd := notify.Deps{Clock: d.Clock, Settings: d.Settings, Config: d.Config, Log: d.Log, Jobs: enq}
		od := orders.Deps{Config: d.Config, Clock: d.Clock, Log: d.Log, Settings: d.Settings,
			Jobs: enq, Stock: OrdersStock(d.Clock), Open: business.OpenAt}
		if d.Pool != nil {
			pool := db.New(d.Pool)
			nd.DB, nd.Staff, od.DB = pool, business.NewStaffResolver(pool), pool
		}
		ns, err := notify.New(nd)
		if err != nil {
			return nil, err
		}
		od.Notices = ns
		// An expired order's held credits go back (W2-01).
		cs, err := CreditsService(d, ns)
		if err != nil {
			return nil, err
		}
		od.Credits = OrdersCredits(cs)
		return orders.New(od)
	}
	timer := func(name, desc string, run func(*orders.Service, context.Context, platformjobs.Run) (platformjobs.Result, error)) {
		platformjobs.Define(r, platformjobs.Kind{
			Name: name, Owner: "W2-02", Class: platformjobs.ClassTimer, Description: desc,
			Timeout: 30 * time.Second, MaxAttempts: ordersTimerAttempts,
			Runbook: "docs/runbooks/orders-timers.md",
			Handler: func(ctx context.Context, rn platformjobs.Run) (platformjobs.Result, error) {
				s, err := svc()
				if err != nil {
					return platformjobs.Result{}, err
				}
				return run(s, ctx, rn)
			},
		})
	}
	timer(orders.KindAcceptTimeout,
		"Expires a placed order the shop did not accept or reject by accept_by (BK-06)",
		(*orders.Service).ExpireUnanswered)
	timer(orders.KindNoShow,
		"Expires a held pickup order nobody collected by hold_until, returns its stock and records a no-show strike (BK-14)",
		(*orders.Service).ExpireNoShow)
	timer(orders.KindPartialTimeout,
		"Cancels a partly accepted order the customer did not confirm or decline by partial_confirm_by, releasing its credits, with no strike (BK-10)",
		(*orders.Service).ExpirePartial)
	platformjobs.Define(r, platformjobs.Kind{
		Name: orders.KindReconcile, Owner: "W2-02", Class: platformjobs.ClassTimer,
		Description: "Applies the orders timers' outcomes to orders still waiting past accept_by, partial_confirm_by or hold_until (BK-20, BK-21)",
		Schedule:    "* * * * *", UniqueBy: platformjobs.PerTick, Timeout: 50 * time.Second, MaxAttempts: 3, Concurrency: 1,
		Runbook: "docs/runbooks/orders-timers.md",
		Handler: func(ctx context.Context, rn platformjobs.Run) (platformjobs.Result, error) {
			s, err := svc()
			if err != nil {
				return platformjobs.Result{}, err
			}
			return s.Reconcile(ctx, rn)
		},
	})
}

// lazyEnqueuer returns a func that builds one insert-only client and its
// enqueuer on first use and returns the same one after. A failed build is
// returned (the run fails and River retries it, or the next tick runs it)
// and tried again next time. Without a pool (tests that only list kinds) it
// returns nil. who names the caller in the error.
func lazyEnqueuer(r *Registry, d Deps, who string) func() (platformjobs.Enqueuer, error) {
	var (
		mu  sync.Mutex
		enq platformjobs.Enqueuer
	)
	return func() (platformjobs.Enqueuer, error) {
		mu.Lock()
		defer mu.Unlock()
		if enq != nil || d.Pool == nil {
			return enq, nil
		}
		c, err := queue.NewInsertClient(d.Pool, queue.Options{Log: d.Log})
		if err != nil {
			return nil, fmt.Errorf("%s: queue client: %w", who, err)
		}
		enq = platformjobs.NewEnqueuer(r, c, d.Clock)
		return enq, nil
	}
}

// OrdersStock is the catalogue's stock writer as orders.Stock: a collected
// line is one sale_online movement referring to the order, idempotent on the
// reservation id. The API's routes file uses it too.
func OrdersStock(c clock.Clock) orders.Stock { return ordersStock{w: catalogue.NewWriter(c)} }

type ordersStock struct{ w *catalogue.Writer }

func (s ordersStock) Sell(ctx context.Context, tx pgx.Tx, in orders.Sale) (ids.ID, error) {
	m, err := s.w.Sell(ctx, tx, catalogue.Sale{
		StoreID: in.StoreID, ItemID: in.ItemID, Qty: in.Qty,
		Source: catalogue.SourceSaleOnline, RefType: "order", RefID: in.OrderID,
		ClientID: in.ClientID, Who: catalogue.Who{UserID: in.UserID},
	})
	if err != nil {
		return ids.Zero, err
	}
	return m.ID, nil
}
