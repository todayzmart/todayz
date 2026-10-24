package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/orders"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// sweep runs the orders reconcile sweep once, for the clock's minute (a
// minute runs once).
func (o *orderShop) sweep() platformjobs.FakeRun {
	o.t.Helper()
	fr, err := o.runner.RunScheduled(context.Background(), orders.KindReconcile)
	require.NoError(o.t, err)
	require.Equal(o.t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.Zero(o.t, fr.Result.Summary["failed"])
	return fr
}

// BK-20/BK-21: orders whose timers never ran (lost or discarded) are found
// by the reconcile sweep at their deadline (no grace), and get
// the timer's own outcome: a placed order expires not_accepted, a partial
// accept is cancelled partial_timeout with no strike, a held pickup expires
// no_show with one strike. An order before its deadline is left,
// a second sweep changes nothing, and the timer running after the sweep is
// a no-op.
func TestBK21ReconcileSweep(t *testing.T) {
	o := newOrderShop(t)
	unanswered := o.order()
	partial := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
	r := o.partial(partial, 1, 1)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	held := o.accepted()
	_, hold := o.times(held)

	// Past every deadline (the hold is the last).
	o.set(hold.Add(time.Minute))
	fresh := o.order()

	fr := o.sweep()
	require.EqualValues(t, 3, fr.Result.Summary["expired"])
	require.Equal(t, "expired", o.status(unanswered))
	require.Equal(t, "cancelled", o.status(partial))
	require.Equal(t, "expired", o.status(held))
	require.Equal(t, "placed", o.status(fresh))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'expired' AND actor_kind = 'system' AND reason = 'not_accepted'`, uuidOf(t, unanswered)))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'cancelled' AND actor_kind = 'system' AND reason = 'partial_timeout'`, uuidOf(t, partial)))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'expired' AND actor_kind = 'system' AND reason = 'no_show'`, uuidOf(t, held)))
	require.Equal(t, 1, o.strikes(), "the no-show only")
	require.Zero(t, o.held(held))

	// Nothing more is due next minute; the timers that run late change
	// nothing.
	o.advance(time.Minute)
	fr = o.sweep()
	require.Zero(t, fr.Result.Summary["expired"])
	require.Zero(t, fr.Result.Summary["noop"])
	noopRun(t, o.timer(orders.KindAcceptTimeout, unanswered, nil))
	noopRun(t, o.timer(orders.KindPartialTimeout, partial, nil))
	noopRun(t, o.timer(orders.KindNoShow, held, hold))
	require.Equal(t, 1, o.strikes())

	// The minute before accept_by the order is left; at accept_by itself
	// the sweep expires it (a scheduled minute runs once, so the two runs
	// are a minute apart).
	acceptBy, _ := o.times(fresh)
	o.set(acceptBy.Add(-time.Minute))
	fr = o.sweep()
	require.Zero(t, fr.Result.Summary["expired"])
	require.Equal(t, "placed", o.status(fresh))
	o.set(*acceptBy)
	fr = o.sweep()
	require.EqualValues(t, 1, fr.Result.Summary["expired"])
	require.Equal(t, "expired", o.status(fresh))
}

// BK-21 (lead review, W2-02 finish): an order the sweep cannot apply does
// not stop the others. The run still succeeds, expires the rest and counts
// the order in failed; the order is marked (sweep_failed_at), so the next
// run takes every order that never failed before it, however old its
// deadline, and once the cause is fixed a run applies it.
func TestBK21ReconcileFailingOrderDoesNotBlockTheOthers(t *testing.T) {
	o := newOrderShop(t)
	ctx := context.Background()
	stuck := o.order() // the oldest deadline
	o.advance(time.Minute)
	first := []string{o.order(), o.order()}

	// The guard-like trigger refuses only stuck's status change, the way a
	// bug or a bad row would.
	o.a.exec(`CREATE FUNCTION test_refuse_stuck() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'test: refused'; END $$`)
	o.a.exec(`CREATE TRIGGER test_refuse_stuck BEFORE UPDATE OF status ON orders
		FOR EACH ROW WHEN (OLD.id = '` + uuidOf(t, stuck).String() + `'::uuid) EXECUTE FUNCTION test_refuse_stuck()`)

	acceptBy, _ := o.times(first[1])
	o.set(*acceptBy)
	fr, err := o.runner.RunScheduled(ctx, orders.KindReconcile)
	require.NoError(t, err)
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 2, fr.Result.Summary["expired"])
	require.EqualValues(t, 1, fr.Result.Summary["failed"])
	for _, id := range first {
		require.Equal(t, "expired", o.status(id))
	}
	require.Equal(t, "placed", o.status(stuck))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND sweep_failed_at = $2`, uuidOf(t, stuck), *acceptBy))

	// Newer due orders now come first, the failed one last.
	o.advance(time.Minute)
	later := []string{o.order(), o.order()}
	acceptBy, _ = o.times(later[1])
	o.set(*acceptBy)
	rows, err := ordersdb.New(o.a.tdb.OwnerPool).DueUnanswered(ctx, ordersdb.DueUnansweredParams{
		Before: *acceptBy, AfterID: pgtype.UUID{Valid: true}, Lim: 10,
	})
	require.NoError(t, err)
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.ID.String()
	}
	want := []string{uuidOf(t, later[0]).String(), uuidOf(t, later[1]).String(), uuidOf(t, stuck).String()}
	require.Equal(t, want, got)

	// The cause is fixed: the next run applies it with the rest.
	o.a.exec(`DROP TRIGGER test_refuse_stuck ON orders`)
	fr = o.sweep()
	require.EqualValues(t, 3, fr.Result.Summary["expired"])
	require.Equal(t, "expired", o.status(stuck))
}
