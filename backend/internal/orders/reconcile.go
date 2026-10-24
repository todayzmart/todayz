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
	"github.com/todayz/backend/internal/platform/telemetry"
)

// KindReconcile is the timers' reconcile sweep (BK-20, BK-21): every minute
// it finds orders past accept_by, partial_confirm_by or hold_until that are
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

// dueRow is one order a walk found past its deadline.
type dueRow struct {
	id pgtype.UUID
	at time.Time
}

// duePage reads one page of a walk after the keyset (at, id).
type duePage func(ctx context.Context, q *ordersdb.Queries, before, at time.Time, id pgtype.UUID) ([]dueRow, error)

func dueUnanswered(ctx context.Context, q *ordersdb.Queries, before, at time.Time, id pgtype.UUID) ([]dueRow, error) {
	rows, err := q.DueUnanswered(ctx, ordersdb.DueUnansweredParams{Before: before, AfterAt: at, AfterID: id, Lim: reconcilePage})
	out := make([]dueRow, len(rows))
	for i, r := range rows {
		out[i] = dueRow{r.ID, r.Due}
	}
	return out, err
}

func duePartials(ctx context.Context, q *ordersdb.Queries, before, at time.Time, id pgtype.UUID) ([]dueRow, error) {
	rows, err := q.DuePartials(ctx, ordersdb.DuePartialsParams{Before: before, AfterAt: at, AfterID: id, Lim: reconcilePage})
	out := make([]dueRow, len(rows))
	for i, r := range rows {
		out[i] = dueRow{r.ID, r.Due}
	}
	return out, err
}

func dueHolds(ctx context.Context, q *ordersdb.Queries, before, at time.Time, id pgtype.UUID) ([]dueRow, error) {
	rows, err := q.DueHolds(ctx, ordersdb.DueHoldsParams{Before: before, AfterAt: at, AfterID: id, Lim: reconcilePage})
	out := make([]dueRow, len(rows))
	for i, r := range rows {
		out[i] = dueRow{r.ID, r.Due}
	}
	return out, err
}

type reconciled struct{ expired, noop, failed int64 }

// Reconcile is the KindReconcile handler. Each order is locked and checked
// again in a transaction of its own, as its timer does: an order that moved
// on meanwhile is a no-op, and one that fails is logged with its id,
// counted and left for the next run, while the walk goes on.
func (s *Service) Reconcile(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
	if err := s.readyToWrite(ctx); err != nil {
		return platformjobs.Result{}, err
	}
	rules, err := s.noShowRules(ctx)
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

// walk applies one timer's outcome to its due orders, oldest first, at most
// reconcileMax of them. An order whose deadline moved since the page was
// read (an extended hold) answers errEarly and is a no-op.
func (s *Service) walk(ctx context.Context, name string, before time.Time, page duePage, apply func(context.Context, ids.ID) (bool, error), out *reconciled) error {
	at, after := time.Time{}, pgtype.UUID{Valid: true} // before every order
	for seen := 0; seen < reconcileMax; {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := page(ctx, s.q(ctx), before, at, after)
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
				telemetry.Logger(ctx).ErrorContext(ctx, "orders: reconcile could not apply a timer",
					"kind", name, "order_id", mustID(r.id).String(), "error", err)
			}
			at, after = r.at, r.id
		}
		if len(rows) < reconcilePage {
			return nil
		}
	}
	return nil
}
