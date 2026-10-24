package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/ids"
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
// accepted lines, the re-capped credits as a preview (the hold is re-capped
// on the confirm), nothing reserved, the
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
	require.Zero(t, m.credits, "₹55 is under credit_min_order_value_paise: the preview keeps none")
	require.EqualValues(t, 5500, m.total)
	require.True(t, o.walletIs(cust, 20), "the hold stays whole until the customer confirms (01 §7)")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM credit_holds WHERE order_id = $1 AND status = 'held' AND amount = 10 AND released = 0`, uuidOf(t, id2)))
	r = o.me(o.cust, id2, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.True(t, o.walletIs(cust, 30), "the confirm re-caps: the second order's 10 credits are back")
	require.Equal(t, m, o.money(id2), "the re-cap kept the preview")

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

// A partial accept prices the order's offers again on what is left (03 §4.2,
// lead W2-02 finish): 10% of the one butter kept, and the ₹10-off-₹100
// minimum-bill offer drops off because the new item subtotal is under ₹100.
// The 10% offer is one use per customer, and the order's own use does not
// count against it. Both redemptions stay until the order ends.
func TestPartialAcceptRepricesOffers(t *testing.T) {
	o := newOrderShop(t)
	pct := ids.New(o.a.clk.Now()).String()
	o.a.exec(`INSERT INTO offers (id, store_id, kind, value, scope, store_item_ids, channels, per_customer_limit, starts_at, status)
	  VALUES ($1, $2, 'percent', 10, 'store', '{}', 'both', 1, $3, 'live')`, uuidOf(t, pct), uuidOf(t, o.sid), o.a.clk.Now().Add(-time.Hour))
	bill := ids.New(o.a.clk.Now()).String()
	o.a.exec(`INSERT INTO offers (id, store_id, kind, value, min_bill_paise, scope, store_item_ids, channels, starts_at, status)
	  VALUES ($1, $2, 'bill_min', 1000, 10000, 'store', '{}', 'both', $3, 'live')`, uuidOf(t, bill), uuidOf(t, o.sid), o.a.clk.Now().Add(-time.Hour))
	lines := []line{{o.butter, 2}, {o.salt, 1}}
	q := o.quote(lines...)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	// ₹135, 10% off each line (₹13.50), then ₹10 off ₹121.50.
	require.EqualValues(t, 2350, q.body["discountPaise"], q.raw)
	r := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, int(q.body["totalPaise"].(float64))), idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.Equal(t, 1, o.redemptions(pct, false))
	require.Equal(t, 1, o.redemptions(bill, false))

	o.keepOneButter(id)
	m := o.money(id)
	require.EqualValues(t, 5500, m.subtotal)
	require.EqualValues(t, 550, m.discount, "10%% of one butter; no bill offer under ₹100 (scaling would have kept ₹4.07 of it)")
	require.EqualValues(t, 4950, m.itemsAfterOffers)
	require.EqualValues(t, 4950, m.total)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_items WHERE order_id = $1 AND quantity_accepted = 1 AND line_total_paise = 4950`, uuidOf(t, id)))
	require.Equal(t, 1, o.redemptions(pct, false))
	require.Equal(t, 1, o.redemptions(bill, false), "TODO(lead): a dropped offer's use stays until the order ends")

	r = o.me(o.cust, id, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, m, o.money(id), "the confirm keeps the re-priced bill")
	r = o.verify(o.cashier, id, o.code(id))
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	require.Equal(t, 1, o.redemptions(pct, false), "a collected order keeps its offer")

	// A declined partial gives every offer back (the 10% is used up now,
	// so only the bill offer applies).
	q = o.quote(lines...)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.EqualValues(t, 1000, q.body["discountPaise"], q.raw)
	r = o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, int(q.body["totalPaise"].(float64))), idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id2 := r.body["id"].(string)
	o.keepOneButter(id2)
	r = o.me(o.cust, id2, "confirm-partial", map[string]any{"accept": false})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Zero(t, o.a.count(`SELECT count(*) FROM offer_redemptions WHERE order_id = $1 AND released_at IS NULL`, uuidOf(t, id2)))
	require.Equal(t, 1, o.redemptions(bill, true))
}

// The hold is re-capped on the confirm, under the credit rules then (01 §7,
// lead W2-02 finish): the preview kept 10 credits, credit_max_per_order went
// down to 5 while the customer looked, so the confirm keeps 5 and the order's
// figures follow.
func TestConfirmPartialRecapsCredits(t *testing.T) {
	o := newOrderShop(t)
	cust := o.grantCredits(40)
	id := o.placeWithCredits(10)
	qty := []int{2, 0}
	if o.lineStoreItem(id, 0) == o.atta {
		qty = []int{0, 2}
	}
	r := o.partial(id, qty...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 10, o.money(id).credits)
	require.True(t, o.walletIs(cust, 30))

	o.a.exec(`UPDATE platform_settings SET value = '5' WHERE key = 'credit_max_per_order'`)
	o.settings.Invalidate()
	r = o.me(o.cust, id, "confirm-partial", map[string]any{"accept": true})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	m := o.money(id)
	require.Equal(t, 5, m.credits)
	require.EqualValues(t, 11000, m.itemsAfterOffers)
	require.EqualValues(t, 10500, m.total)
	require.True(t, o.walletIs(cust, 35), "5 of the 10 held credits are back")
	c := o.a.call(o.cust, http.MethodGet, "/v1/me/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, c.code, c.raw)
	require.EqualValues(t, 5, c.body["creditsUsed"])
	require.EqualValues(t, 10500, c.body["totalPaise"])
}

// placeQuoted places lines at the quote's total and returns the order id.
func (o *orderShop) placeQuoted(lines ...line) string {
	o.t.Helper()
	q := o.quote(lines...)
	require.Equal(o.t, http.StatusOK, q.code, q.raw)
	r := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, int(q.body["totalPaise"].(float64))), idem(o.a))
	require.Equal(o.t, http.StatusCreated, r.code, r.raw)
	return r.body["id"].(string)
}

// lineQty is the partial-accept quantities for an order's lines, by item.
func (o *orderShop) lineQty(id string, want map[string]int) []int {
	o.t.Helper()
	qty := make([]int, len(o.lineIDs(id)))
	for i := range qty {
		qty[i] = want[o.lineStoreItem(id, i)]
	}
	return qty
}

// A buy-2-get-1 line is priced again on the kept quantity (lead review 1):
// six butters (two free) kept as four give one free, kept as two give none.
func TestPartialAcceptRepricesBuyXGetY(t *testing.T) {
	o := newOrderShop(t)
	bx := ids.New(o.a.clk.Now()).String()
	o.a.exec(`INSERT INTO offers (id, store_id, kind, value, buy_qty, get_qty, scope, store_item_ids, channels, starts_at, status)
	  VALUES ($1, $2, 'bxgy', 0, 2, 1, 'store', '{}', 'both', $3, 'live')`, uuidOf(t, bx), uuidOf(t, o.sid), o.a.clk.Now().Add(-time.Hour))

	id := o.placeQuoted(line{o.butter, 6}, line{o.salt, 1})
	require.EqualValues(t, 2*5500, o.money(id).discount, "two butters free")
	r := o.partial(id, o.lineQty(id, map[string]int{o.butter: 4, o.salt: 1})...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	m := o.money(id)
	require.EqualValues(t, 4*5500+2500, m.subtotal)
	require.EqualValues(t, 5500, m.discount, "four butters keep one free")
	require.EqualValues(t, 3*5500+2500, m.total)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_items WHERE order_id = $1 AND store_item_id = $2 AND quantity_accepted = 4 AND line_total_paise = $3`,
		uuidOf(t, id), uuidOf(t, o.butter), 3*5500))

	id = o.placeQuoted(line{o.butter, 3}, line{o.salt, 1})
	require.EqualValues(t, 5500, o.money(id).discount)
	r = o.partial(id, o.lineQty(id, map[string]int{o.butter: 2, o.salt: 1})...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	m = o.money(id)
	require.Zero(t, m.discount, "two butters no longer make a group")
	require.EqualValues(t, 2*5500+2500, m.total)
}

// An ask reply's line keeps its replied price on a partial accept and takes
// no offer (D-101, lead review 7); the other lines are re-priced as usual.
func TestPartialAcceptKeepsAskReplyPrice(t *testing.T) {
	o := newOrderShop(t)
	o.offerOn("percent", 10, nil)
	id := o.placeQuoted(line{o.butter, 2}, line{o.salt, 1})
	// The order's butter line came from an ask reply at ₹50 a unit.
	o.a.exec(`UPDATE order_items SET price_source = 'ask_reply', unit_price_paise = 5000 WHERE order_id = $1 AND store_item_id = $2`,
		uuidOf(t, id), uuidOf(t, o.butter))

	r := o.partial(id, o.lineQty(id, map[string]int{o.butter: 1, o.salt: 1})...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	m := o.money(id)
	require.EqualValues(t, 5000+2500, m.subtotal)
	require.EqualValues(t, 250, m.discount, "10% of the salt only")
	require.EqualValues(t, 5000+2250, m.total)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_items WHERE order_id = $1 AND store_item_id = $2 AND unit_price_paise = 5000 AND line_total_paise = 5000`,
		uuidOf(t, id), uuidOf(t, o.butter)))
}
