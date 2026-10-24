package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
// by the reconcile sweep once past their deadline and its grace, and get
// the timer's own outcome: a placed order expires not_accepted, a partial
// accept is cancelled partial_timeout with no strike, a held pickup expires
// no_show with one strike. An order not yet past the grace is left to its
// timer, a second sweep changes nothing, and the timer running after the
// sweep is a no-op.
func TestBK21ReconcileSweep(t *testing.T) {
	o := newOrderShop(t)
	unanswered := o.order()
	partial := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
	r := o.partial(partial, 1, 1)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	held := o.accepted()
	_, hold := o.times(held)

	// Past every deadline (the hold is the last) and the grace.
	o.set(hold.Add(3 * time.Minute))
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

	// One minute past accept_by is still the timer's; past the grace it is
	// the sweep's.
	acceptBy, _ := o.times(fresh)
	o.set(acceptBy.Add(time.Minute))
	fr = o.sweep()
	require.Zero(t, fr.Result.Summary["expired"])
	require.Equal(t, "placed", o.status(fresh))
	o.set(acceptBy.Add(2 * time.Minute))
	fr = o.sweep()
	require.EqualValues(t, 1, fr.Result.Summary["expired"])
	require.Equal(t, "expired", o.status(fresh))
}
