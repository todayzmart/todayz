package jobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/notify"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
)

const ledgerRunbook = "docs/runbooks/credits-ledger.md"

// registercredits declares the credits jobs (W2-01): the hourly expiry, the
// daily "credits expiring" reminder and the nightly ledger reconcile. Each
// run is idempotent: expiry and reminders mark what they did, and the
// reconcile only reads.
func registercredits(r *Registry, d Deps) {
	// Built on the first run, as for the orders timers: the reminder and
	// expiry write notices, which enqueue sends.
	enqueuer := lazyEnqueuer(r, d, "credits jobs")
	var (
		mu  sync.Mutex
		svc *credits.Service
	)
	service := func() (*credits.Service, error) {
		mu.Lock()
		defer mu.Unlock()
		if svc != nil {
			return svc, nil
		}
		if d.Pool == nil {
			return nil, errors.New("credits: no database")
		}
		enq, err := enqueuer()
		if err != nil {
			return nil, err
		}
		pool := db.New(d.Pool)
		ns, err := notify.New(notify.Deps{DB: pool, Clock: d.Clock, Settings: d.Settings, Config: d.Config,
			Log: d.Log, Jobs: enq, Staff: business.NewStaffResolver(pool)})
		if err != nil {
			return nil, err
		}
		s, err := CreditsService(d, ns)
		if err != nil {
			return nil, err
		}
		svc = s
		return svc, nil
	}
	job := func(k platformjobs.Kind, run func(context.Context, *credits.Service) (map[string]int64, error)) {
		k.Owner, k.Runbook = "W2-01", ledgerRunbook
		k.Timeout, k.MaxAttempts = 60*time.Second, 3
		k.Handler = func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			s, err := service()
			if err != nil {
				return platformjobs.Result{}, err
			}
			sum, err := run(ctx, s)
			return platformjobs.Result{Summary: sum}, err
		}
		platformjobs.Define(r, k)
	}

	job(platformjobs.Kind{
		Name: credits.KindExpire, Class: platformjobs.ClassMoney,
		Description: "Expires credit lots past their expires_at to platform_expired, one transaction per customer (01 §7)",
		Schedule:    "5 * * * *", UniqueBy: platformjobs.PerTick,
	}, func(ctx context.Context, s *credits.Service) (map[string]int64, error) {
		cu, n, err := s.ExpireDue(ctx)
		return map[string]int64{"customers": int64(cu), "credits": int64(n)}, err
	})
	job(platformjobs.Kind{
		Name: credits.KindRemind, Class: platformjobs.ClassNotify,
		Description: "Sends one \"credits expiring\" notice per customer for lots expiring within credit_expiry_reminder_days (01 §7, §15)",
		Schedule:    "0 10 * * *", UniqueBy: platformjobs.PerDay,
	}, func(ctx context.Context, s *credits.Service) (map[string]int64, error) {
		n, err := s.RemindExpiring(ctx)
		return map[string]int64{"reminded": int64(n)}, err
	})
	job(platformjobs.Kind{
		Name: credits.KindReconcile, Class: platformjobs.ClassMoney,
		Description: "Checks the ledger invariants (03 §6) and records tz_ledger_reconcile_drift; any drift pages",
		Schedule:    "30 3 * * *", UniqueBy: platformjobs.PerDay,
	}, func(ctx context.Context, s *credits.Service) (map[string]int64, error) {
		dr, err := s.Reconcile(ctx)
		return map[string]int64(dr), err
	})
}

// CreditsService builds the credits service on d's pool with ns as its
// notice sender. The API's routes files use it too.
func CreditsService(d Deps, ns notice.Sender) (*credits.Service, error) {
	cd := credits.Deps{Clock: d.Clock, Settings: d.Settings, Notices: ns}
	if d.Pool != nil {
		cd.DB = db.New(d.Pool)
	}
	return credits.New(cd)
}

// OrdersCredits is the credits ledger as orders.Credits: an order's hold is
// the credits.RefOrder ref of its id.
func OrdersCredits(s *credits.Service) orders.Credits { return ordersCredits{s: s} }

type ordersCredits struct{ s *credits.Service }

func orderRef(id ids.ID) credits.Ref { return credits.Ref{Type: credits.RefOrder, ID: id} }

func allowance(a credits.Allowance) orders.CreditsAllowance {
	return orders.CreditsAllowance{Allowed: a.Allowed, Limit: string(a.Limit)}
}

func (c ordersCredits) Allow(ctx context.Context, customerID, storeID ids.ID, requested int, eligiblePaise int64) (orders.CreditsAllowance, error) {
	a, err := c.s.Allow(ctx, customerID, storeID, requested, eligiblePaise)
	return allowance(a), err
}

func (c ordersCredits) Hold(ctx context.Context, h orders.CreditsHold) (orders.CreditsAllowance, error) {
	a, err := c.s.Hold(ctx, credits.HoldRequest{
		Ref: orderRef(h.OrderID), CustomerID: h.CustomerID, StoreID: h.StoreID,
		Credits: h.Credits, EligiblePaise: h.EligiblePaise,
	})
	return allowance(a), err
}

func (c ordersCredits) Release(ctx context.Context, orderID ids.ID) error {
	_, err := c.s.Release(ctx, orderRef(orderID))
	return err
}

func (c ordersCredits) Recap(ctx context.Context, orderID ids.ID, eligiblePaise int64) (int, error) {
	return c.s.Recap(ctx, orderRef(orderID), eligiblePaise)
}

func (c ordersCredits) Complete(ctx context.Context, orderID, customerID, storeID ids.ID, eligiblePaise int64, cashbackBP int) (int, error) {
	r, err := c.s.Complete(ctx, credits.Completion{
		Ref: orderRef(orderID), CustomerID: customerID, StoreID: storeID, EligiblePaise: eligiblePaise,
		CashbackBP: &cashbackBP,
	})
	return r.Cashback, err
}
