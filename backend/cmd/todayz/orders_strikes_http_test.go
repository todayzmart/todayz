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

const day = 24 * time.Hour

// noShow places, accepts and lets one pickup order expire no_show: one
// strike for the customer.
func (o *orderShop) noShow() {
	o.t.Helper()
	id := o.accepted()
	_, hold := o.times(id)
	o.set(*hold)
	expiredRun(o.t, o.timer(orders.KindNoShow, id, hold))
}

// recountRun runs the daily strike recount once, for the clock's day.
func (o *orderShop) recountRun() int64 {
	o.t.Helper()
	fr, err := o.runner.RunScheduled(context.Background(), orders.KindStrikesRecount)
	require.NoError(o.t, err)
	require.Equal(o.t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	return fr.Result.Summary["recounted"]
}

// unlimited counts customer profiles with this cached count and no limit.
func (o *orderShop) unlimited(strikes int) int {
	return o.a.count(`SELECT count(*) FROM customer_profiles WHERE strikes_count_30d = $1 AND limited_until IS NULL`, strikes)
}

// BK-16, rolling (lead, W2-02 finish): the 30-day count is recomputed from
// strikes, so a strike older than noshow_window_days stops counting. Two
// no-shows on days 0 and 10, then a third on day 31: the day-0 strike has
// aged out, so the count is 2 and there is no limit, where a count that
// never went down would have limited the customer. The daily recount lowers
// the cached count with no new strike.
func TestBK16StrikesRoll(t *testing.T) {
	o := newOrderShop(t)
	start := o.a.clk.Now()
	o.noShow()
	o.set(start.Add(10 * day))
	o.noShow()
	require.Equal(t, 1, o.unlimited(2))

	// Day 30 and a bit: the daily recount drops the day-0 strike.
	o.set(start.Add(30*day + time.Hour))
	require.EqualValues(t, 1, o.recountRun())
	require.Equal(t, 1, o.unlimited(1))
	fr, err := o.runner.RunScheduled(context.Background(), orders.KindStrikesRecount)
	require.NoError(t, err)
	require.Equal(t, platformjobs.StatusSkipped, fr.Status, "a day runs once")

	// Day 31: the third no-show in all, the second in the window.
	o.set(start.Add(31 * day))
	o.noShow()
	require.Equal(t, 1, o.unlimited(2), "no limit: the day-0 strike no longer counts")
	require.Equal(t, 3, o.strikes())

	// Not limited: a second open order is allowed.
	o.order()
	q := o.quote(line{o.butter, 1})
	require.Equal(t, http.StatusOK, q.code, q.raw)
	o.order()
}

// A reset strike stops counting: three no-shows limit the customer, and
// after support resets one (W3-05 writes reset_at; here the test does) the
// recount lifts the limit at once.
func TestBK16StrikeResetLiftsLimit(t *testing.T) {
	o := newOrderShop(t)
	for range 3 {
		o.noShow()
	}
	limitedUntil := o.a.clk.Now().Add(14 * day)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM customer_profiles WHERE strikes_count_30d = 3 AND limited_until = $1`, limitedUntil))
	o.order()
	q := o.quote(line{o.butter, 1})
	require.Equal(t, http.StatusConflict, q.code, q.raw)
	require.Equal(t, "ordering_suspended", problemCode(q))

	o.a.exec(`UPDATE strikes SET reset_at = $1, reset_reason = 'test' WHERE id = (SELECT id FROM strikes ORDER BY created_at LIMIT 1)`, o.a.clk.Now())
	o.advance(time.Hour) // the next daily run
	require.EqualValues(t, 1, o.recountRun())
	require.Equal(t, 1, o.unlimited(2))
	q = o.quote(line{o.butter, 1})
	require.Equal(t, http.StatusOK, q.code, q.raw)
}

// The limit ends by itself after noshow_limit_days, and the recount then
// clears limited_until too.
func TestBK16LimitEnds(t *testing.T) {
	o := newOrderShop(t)
	for range 3 {
		o.noShow()
	}
	end := o.a.clk.Now().Add(14 * day)
	o.order()
	o.set(end.Add(time.Hour))
	q := o.quote(line{o.butter, 1})
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.EqualValues(t, 1, o.recountRun())
	require.Equal(t, 1, o.unlimited(3), "the strikes are still inside the 30 days")
}
