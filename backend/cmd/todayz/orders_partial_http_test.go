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

// The partial accept (BK-08–10, D-28) through the real router: the shop
// takes fewer, the customer confirms or declines before partial_confirm_by,
// or the timer cancels the order with no strike.

// partial has the cashier accept qty[i] of the order's i-th line.
func (o *orderShop) partial(id string, qty ...int) resp {
	o.t.Helper()
	lines := o.lineIDs(id)
	require.Len(o.t, lines, len(qty))
	body := make([]any, 0, len(qty))
	for i, q := range qty {
		body = append(body, map[string]any{"orderItemId": lines[i], "quantity": q})
	}
	return o.staff(o.cashier, id, "accept", map[string]any{"lines": body})
}

// money is an order's stored totals.
type money struct {
	subtotal, discount, itemsAfterOffers, total int64
	credits                                     int
	confirmBy                                   *time.Time
}

func (o *orderShop) money(id string) money {
	o.t.Helper()
	var m money
	require.NoError(o.t, o.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT subtotal_paise, discount_paise, items_after_offers_paise, total_paise, credits_used, partial_confirm_by FROM orders WHERE id = $1`,
		uuidOf(o.t, id)).Scan(&m.subtotal, &m.discount, &m.itemsAfterOffers, &m.total, &m.credits, &m.confirmBy))
	return m
}

// BK-08: the shop takes some lines or fewer of one: the order waits in
// awaiting_customer for partial_confirm_timeout_min with the totals on the
// accepted lines, the held credits re-capped, nothing reserved, the
// customer told and the timer queued. Taking nothing is refused.
func TestBK08PartialAccept(t *testing.T) {
	o := newOrderShop(t)
	cust := o.grantCredits(40)
	id := o.placeWithCredits(10) // butter×2 (₹110) + atta (₹290), 10 credits
	require.True(t, o.walletIs(cust, 30))

	r := o.partial(id, 0, 0)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.lines", r.raw)
	require.Equal(t, "placed", o.status(id))

	now := o.a.clk.Now()
	g := o.a.call(o.cashier, http.MethodGet, o.S+"/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, g.code, g.raw)
	// Keep the butter, drop the atta, whichever line comes first.
	qty := []int{2, 0}
	if g.body["lines"].([]any)[0].(map[string]any)["storeItemId"] == o.atta {
		qty = []int{0, 2}
	}
	r = o.partial(id, qty...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "awaiting_customer", r.body["status"])
	noCode(t, r.body, "an awaiting order shows no code")
	m := o.money(id)
	require.EqualValues(t, 11000, m.subtotal)
	require.EqualValues(t, 11000, m.itemsAfterOffers)
	require.Equal(t, 10, m.credits, "₹110 is over the credit minimum: the 10 credits stay")
	require.EqualValues(t, 10000, m.total)
	require.NotNil(t, m.confirmBy)
	require.True(t, m.confirmBy.Equal(now.Add(5*time.Minute)), "partial_confirm_by = now + partial_confirm_timeout_min")
	require.Zero(t, o.held(id), "nothing is reserved until the customer confirms")
	_, reserved := o.stock(o.butter)
	require.Zero(t, reserved)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_items WHERE order_id = $1 AND quantity_accepted = 0 AND line_total_paise = 0`, uuidOf(t, id)))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_items WHERE order_id = $1 AND quantity_accepted = 2 AND line_total_paise = 11000`, uuidOf(t, id)))
	require.Equal(t, 1, o.timerJobs(orders.KindPartialTimeout, id, *m.confirmBy))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND params->>'variant' = 'partial'`, uuidOf(t, id)))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'awaiting_customer' AND actor_kind = 'staff'`, uuidOf(t, id)))
	c := o.a.call(o.cust, http.MethodGet, "/v1/me/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, c.code)
	require.Equal(t, "awaiting_customer", c.body["status"])
	require.NotNil(t, c.body["partialConfirmBy"])

	r = o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusConflict, r.code, "an awaiting order is not accepted again: %s", r.raw)

	// Under the credit minimum every credit goes back (D-28).
	id2 := o.placeWithCredits(10)
	require.True(t, o.walletIs(cust, 20))
	qty2 := []int{1, 0}
	if o.lineStoreItem(id2, 0) == o.atta {
		qty2 = []int{0, 1}
	}
	r = o.partial(id2, qty2...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	m = o.money(id2)
	require.EqualValues(t, 5500, m.subtotal)
	require.Zero(t, m.credits, "₹55 is under credit_min_order_value_paise")
	require.EqualValues(t, 5500, m.total)
	require.True(t, o.walletIs(cust, 30), "the second order's 10 credits are back")

	// An accepted quantity over what is available is 409 naming the line.
	id3 := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
	o.a.exec(`UPDATE store_items SET on_hand = 0 WHERE id = $1`, uuidOf(t, o.salt))
	qty3 := []int{1, 1}
	r = o.partial(id3, qty3...)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "insufficient_stock", problemCode(r))
	require.Equal(t, "placed", o.status(id3))
}

func (o *orderShop) lineStoreItem(id string, i int) string {
	o.t.Helper()
	g := o.a.call(o.cashier, http.MethodGet, o.S+"/orders/"+id, nil, nil)
	require.Equal(o.t, http.StatusOK, g.code, g.raw)
	return g.body["lines"].([]any)[i].(map[string]any)["storeItemId"].(string)
}

// keepOneButter partly accepts an order of butter×2 + salt by keeping 1 butter
// and no salt.
func (o *orderShop) keepOneButter(id string) {
	o.t.Helper()
	qty := []int{1, 0}
	if o.lineStoreItem(id, 0) != o.butter {
		qty = []int{0, 1}
	}
	r := o.partial(id, qty...)
	require.Equal(o.t, http.StatusOK, r.code, r.raw)
}

// BK-09: the customer confirms: the accepted quantities are held, the
// pickup hold starts and the code shows; a confirm after
// partial_confirm_by, or of an order not awaiting, is 409.
func TestBK09ConfirmPartial(t *testing.T) {
	o := newOrderShop(t)
	id := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
	r := o.me(o.cust, id, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusConflict, r.code, "a placed order: %s", r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))

	o.keepOneButter(id)
	r = o.me(o.cust2, id, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusNotFound, r.code, "another customer's order: %s", r.raw)

	o.advance(time.Minute)
	now := o.a.clk.Now()
	r = o.me(o.cust, id, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "accepted", r.body["status"])
	noCode(t, r.body, "")
	_, hold := o.times(id)
	require.NotNil(t, hold)
	require.True(t, hold.Equal(now.Add(30*time.Minute)), "hold_until = confirmed_at + pickup_hold_min")
	require.Equal(t, 1, o.held(id), "only the accepted line is held")
	_, reserved := o.stock(o.butter)
	require.Equal(t, 1, reserved)
	_, reserved = o.stock(o.salt)
	require.Zero(t, reserved)
	o.code(id)
	require.Equal(t, 1, o.timerJobs(orders.KindNoShow, id, *hold))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'accepted' AND actor_kind = 'customer'`, uuidOf(t, id)))
	// The partial timer finds the order answered.
	o.advance(5 * time.Minute)
	noopRun(t, o.timer(orders.KindPartialTimeout, id, nil))
	require.Equal(t, "accepted", o.status(id))
	r = o.verify(o.cashier, id, o.code(id))
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	require.Equal(t, "collected", r.body["status"])
	onHand, _ := o.stock(o.butter)
	require.Equal(t, 499, onHand, "one butter sold")

	// Too late: strict at partial_confirm_by (D-108).
	id2 := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
	o.keepOneButter(id2)
	o.advance(5 * time.Minute)
	r = o.me(o.cust, id2, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "awaiting_customer", o.status(id2))
	require.Zero(t, o.held(id2))

	// The stock went while the customer looked: 409, and the order waits.
	id3 := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
	o.keepOneButter(id3)
	o.a.exec(`UPDATE store_items SET on_hand = reserved WHERE id = $1`, uuidOf(t, o.butter))
	r = o.me(o.cust, id3, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "insufficient_stock", problemCode(r))
	require.Equal(t, "awaiting_customer", o.status(id3))
}

// BK-09 decline, and BK-10: the customer declines (declined_partial), or
// nobody answers and the timer cancels with partial_timeout; the credits
// go back and nobody gets a strike either way.
func TestBK10PartialDeclineAndTimeout(t *testing.T) {
	o := newOrderShop(t)
	cust := o.grantCredits(40)

	id := o.placeWithCredits(10)
	qty := []int{2, 0}
	if o.lineStoreItem(id, 0) == o.atta {
		qty = []int{0, 2}
	}
	r := o.partial(id, qty...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = o.me(o.cust, id, "confirm-partial", map[string]any{"accept": false})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "cancelled", r.body["status"])
	require.Equal(t, "declined_partial", r.body["statusReason"])
	require.True(t, o.walletIs(cust, 40), "the declined order's credits are back")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'cancelled' AND actor_kind = 'customer' AND reason = 'declined_partial'`, uuidOf(t, id)))

	id2 := o.placeWithCredits(10)
	r = o.partial(id2, qty...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	confirmBy := o.money(id2).confirmBy
	o.advance(5*time.Minute - time.Second)
	fr := o.timer(orders.KindPartialTimeout, id2, nil)
	require.Equal(t, platformjobs.StatusFailed, fr.Status, "early: fails (retried) and changes nothing")
	require.Equal(t, "awaiting_customer", o.status(id2))
	o.set(*confirmBy)
	expiredRun(t, o.timer(orders.KindPartialTimeout, id2, nil))
	require.Equal(t, "cancelled", o.status(id2))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND status_reason = 'partial_timeout'`, uuidOf(t, id2)))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'cancelled' AND actor_kind = 'system' AND reason = 'partial_timeout'`, uuidOf(t, id2)))
	require.True(t, o.walletIs(cust, 40), "the timed-out order's credits are back")
	noopRun(t, o.timer(orders.KindPartialTimeout, id2, nil))
	require.Zero(t, o.strikes(), "no strike for a declined or timed-out partial (D-68)")
	r = o.me(o.cust, id2, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
}

// A partial accept scales the offer the order got: 10% of 2 butters, one
// kept, is 10% of one, and the redemption stays.
func TestPartialAcceptScalesOffer(t *testing.T) {
	o := newOrderShop(t)
	offer := o.offerOn("percent", 10, nil)
	lines := []line{{o.butter, 2}, {o.salt, 1}}
	q := o.quote(lines...)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	r := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, int(q.body["totalPaise"].(float64))), idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	o.keepOneButter(id)
	m := o.money(id)
	require.EqualValues(t, 5500, m.subtotal)
	require.EqualValues(t, 550, m.discount)
	require.EqualValues(t, 4950, m.itemsAfterOffers)
	require.EqualValues(t, 4950, m.total)
	require.Equal(t, 1, o.redemptions(offer, false))
	r = o.me(o.cust, id, "confirm-partial", map[string]any{"accept": false})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 1, o.redemptions(offer, true), "a declined partial gives the offer back")
}
