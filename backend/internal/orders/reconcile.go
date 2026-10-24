package orders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// KindReconcile is the timers' reconcile sweep (BK-20, BK-21): every minute
// it finds orders past accept_by, partial_confirm_by, hold_until or
// dispatch_by, and failed deliveries with a return step due, that are
// still waiting (a timer discarded after its attempts, or lost) and applies
// the timer's outcome, through the same code, so a run that meets the
// timer itself changes nothing twice.
const KindReconcile = "order_timer_reconcile"

// The sweep acts at the deadline itself, with no grace (lead, W2-02
// finish): a timer that runs too meets an order already moved on and is a
// no-op, and since every transition refuses after its deadline, the lag is
// at most the sweep's minute.
const (
	// reconcilePage is one query's page; reconcileMax caps one walk per run
	// (the next minute's run takes the rest).
	reconcilePage = 100
	reconcileMax  = 500
)

// outcomeFailed is the sweep's third summary key, beside expired and noop:
// an order it could not apply this run.
const outcomeFailed = "failed"

// dueRow is one order a walk found past its deadline, with when the sweep
// last failed on it (the zero time for never).
type dueRow struct {
	id     pgtype.UUID
	at     time.Time
	failed time.Time
}

// dueKey is a walk's keyset: the sweep's order is never-failed orders
// first, oldest deadline first, then the failed ones, least recently failed
// first (migration 20203), so orders that keep failing cannot hold newer
// due orders behind reconcileMax.
type dueKey struct {
	failed, at time.Time
	id         pgtype.UUID
}

// duePage reads one page of a walk after the keyset.
type duePage func(ctx context.Context, q *ordersdb.Queries, before time.Time, k dueKey) ([]dueRow, error)

func dueUnanswered(ctx context.Context, q *ordersdb.Queries, before time.Time, k dueKey) ([]dueRow, error) {
	rows, err := q.DueUnanswered(ctx, ordersdb.DueUnansweredParams{Before: before, AfterFailed: k.failed, AfterAt: k.at, AfterID: k.id, Lim: reconcilePage})
	out := make([]dueRow, len(rows))
	for i, r := range rows {
		out[i] = dueRow{r.ID, r.Due, r.Failed}
	}
	return out, err
}

func duePartials(ctx context.Context, q *ordersdb.Queries, before time.Time, k dueKey) ([]dueRow, error) {
	rows, err := q.DuePartials(ctx, ordersdb.DuePartialsParams{Before: before, AfterFailed: k.failed, AfterAt: k.at, AfterID: k.id, Lim: reconcilePage})
	out := make([]dueRow, len(rows))
	for i, r := range rows {
		out[i] = dueRow{r.ID, r.Due, r.Failed}
	}
	return out, err
}

func dueHolds(ctx context.Context, q *ordersdb.Queries, before time.Time, k dueKey) ([]dueRow, error) {
	rows, err := q.DueHolds(ctx, ordersdb.DueHoldsParams{Before: before, AfterFailed: k.failed, AfterAt: k.at, AfterID: k.id, Lim: reconcilePage})
	out := make([]dueRow, len(rows))
	for i, r := range rows {
		out[i] = dueRow{r.ID, r.Due, r.Failed}
	}
	return out, err
}

func dueDispatches(ctx context.Context, q *ordersdb.Queries, before time.Time, k dueKey) ([]dueRow, error) {
	rows, err := q.DueDispatches(ctx, ordersdb.DueDispatchesParams{Before: before, AfterFailed: k.failed, AfterAt: k.at, AfterID: k.id, Lim: reconcilePage})
	out := make([]dueRow, len(rows))
	for i, r := range rows {
		out[i] = dueRow{r.ID, r.Due, r.Failed}
	}
	return out, err
}

// dueReturns reads failed deliveries with a return step due at before.
func dueReturns(rt returnTimes) duePage {
	return func(ctx context.Context, q *ordersdb.Queries, before time.Time, k dueKey) ([]dueRow, error) {
		rows, err := q.DueReturns(ctx, ordersdb.DueReturnsParams{
			First: before.Add(-rt.first), Second: before.Add(-rt.second), Settle: before.Add(-returnSettleAfter),
			Before: before, AfterFailed: k.failed, AfterAt: k.at, AfterID: k.id, Lim: reconcilePage,
		})
		out := make([]dueRow, len(rows))
		for i, r := range rows {
			out[i] = dueRow{r.ID, r.Due, r.Failed}
		}
		return out, err
	}
}

type reconciled struct{ expired, noop, failed int64 }

// Reconcile is the KindReconcile handler. Each order is locked and checked
// again in a transaction of its own, as its timer does: an order that moved
// on meanwhile is a no-op, and one that fails is logged with its id,
// counted (the summary's failed and tz_order_reconcile_failed_total, alert
// tz-order-reconcile-failed), marked with the run's time and left for the
// next run, behind the orders that never failed, while the walk goes on.
func (s *Service) Reconcile(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
	if err := s.readyToWrite(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	rules, err := s.noShowRules(ctx)
	if err != nil {
		return platformjobs.Result{}, err
	}
	threshold, err := s.count(ctx, settings.StoreStrikeRiskThreshold)
	if err != nil {
		return platformjobs.Result{}, err
	}
	rt, err := s.returnTimes(ctx)
	if err != nil {
		return platformjobs.Result{}, err
	}
	before := s.now()
	var out reconciled
	walks := []struct {
		name  string
		page  duePage
		apply func(context.Context, ids.ID) (bool, error)
	}{
		{KindAcceptTimeout, dueUnanswered, s.expireUnanswered},
		{KindPartialTimeout, duePartials, s.expirePartial},
		{KindNoShow, dueHolds, func(ctx context.Context, id ids.ID) (bool, error) { return s.expireNoShow(ctx, id, nil, rules) }},
		{KindDispatchTimeout, dueDispatches, func(ctx context.Context, id ids.ID) (bool, error) { return s.expireDispatch(ctx, id, threshold) }},
		{KindDeliveryReturn, dueReturns(rt), func(ctx context.Context, id ids.ID) (bool, error) {
			did, err := s.returnStep(ctx, id, 0)
			return did != "", err
		}},
	}
	for _, w := range walks {
		if err := s.walk(ctx, w.name, before, w.page, w.apply, &out); err != nil {
			return platformjobs.Result{}, err
		}
	}
	return platformjobs.Result{Summary: map[string]int64{
		outcomeExpired: out.expired, outcomeNoop: out.noop, outcomeFailed: out.failed,
	}}, nil
}

// walk applies one timer's outcome to its due orders in the sweep's order,
// at most reconcileMax of them. An order whose deadline moved since the page
// was read (an extended hold) answers errEarly and is a no-op.
func (s *Service) walk(ctx context.Context, name string, before time.Time, page duePage, apply func(context.Context, ids.ID) (bool, error), out *reconciled) error {
	k := dueKey{id: pgtype.UUID{Valid: true}} // before every order
	for seen := 0; seen < reconcileMax; {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := page(ctx, s.q(ctx), before, k)
		if err != nil {
			return fmt.Errorf("orders: reconcile %s: %w", name, err)
		}
		for _, r := range rows {
			if seen == reconcileMax {
				return nil
			}
			seen++
			done, err := apply(ctx, mustID(r.id))
			switch {
			case err == nil && done:
				out.expired++
			case err == nil, errors.Is(err, errEarly):
				out.noop++
			case ctx.Err() != nil:
				return ctx.Err()
			default:
				out.failed++
				reconcileFailed(ctx, name)
				log := telemetry.Logger(ctx)
				log.ErrorContext(ctx, "orders: reconcile could not apply a timer",
					"kind", name, "order_id", mustID(r.id).String(), "error", err)
				// Outside the failed transaction; a mark that fails only
				// leaves the order where it sorts now.
				if err := s.q(ctx).SetSweepFailed(ctx, ordersdb.SetSweepFailedParams{At: &before, ID: r.id}); err != nil {
					log.ErrorContext(ctx, "orders: reconcile could not mark a failed order",
						"kind", name, "order_id", mustID(r.id).String(), "error", err)
				}
			}
			k = dueKey{r.failed, r.at, r.id}
		}
		if len(rows) < reconcilePage {
			return nil
		}
	}
	return nil
}
