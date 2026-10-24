package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// The W2-02 delivery half (SC-BK BK-23…27, BK-41): store S delivers within
// 2 km for a ₹30 fee, free from ₹500, minimum ₹100 (item subtotal, D-94).
// The customer has an address inside the radius and one outside it.

const (
	dFee       = 3000
	dMinimum   = 10000
	dFreeAbove = 50000
)

// delivery is o with S delivering and the customer's two addresses.
type delivery struct {
	*orderShop
	near, far string
}

func newDeliveryShop(t *testing.T) *delivery {
	t.Helper()
	o := newOrderShop(t)
	o.a.exec(`UPDATE stores SET accepts_delivery = true, location = ST_SetSRID(ST_MakePoint(77.6, 12.9), 4326)::geography,
	  delivery_radius_m = 2000, delivery_fee_paise = $1, delivery_min_order_paise = $2, delivery_free_above_paise = $3 WHERE id = $4`,
		dFee, dMinimum, dFreeAbove, uuidOf(t, o.sid))
	d := &delivery{orderShop: o}
	d.near = d.address(o.cust, "Home", 77.605, 12.9, true) // about 540 m
	d.far = d.address(o.cust, "Office", 77.7, 12.9, false) // about 10.8 km
	return d
}

// address saves an address for the customer behind tok.
func (d *delivery) address(tok, label string, lng, lat float64, isDefault bool) string {
	d.t.Helper()
	me := d.a.call(tok, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(d.t, http.StatusOK, me.code, me.raw)
	id := ids.New(d.a.clk.Now()).String()
	d.a.exec(`INSERT INTO addresses (id, user_id, label, line1, landmark, pincode, location, is_default)
	  VALUES ($1, $2, $3, '12 4th Cross, BTM Layout', 'Near the temple', '560076', ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography, $6)`,
		uuidOf(d.t, id), uuidOf(d.t, me.body["id"].(string)), label, lng, lat, isDefault)
	return id
}

// dcart is a delivery cart to addr.
func (d *delivery) dcart(addr string, lines []line, expected int) map[string]any {
	b := d.cart(lines, expected)
	b["fulfilment"] = "delivery"
	b["addressId"] = addr
	return b
}

func (d *delivery) dquote(addr string, lines ...line) resp {
	d.t.Helper()
	body := d.dcart(addr, lines, 0)
	delete(body, "expectedTotalPaise")
	return d.a.call(d.cust, http.MethodPost, "/v1/me/checkout/quote", body, nil)
}

func (d *delivery) dplace(addr string, expected int, lines ...line) resp {
	d.t.Helper()
	return d.a.call(d.cust, http.MethodPost, "/v1/me/orders", d.dcart(addr, lines, expected), idem(d.a))
}

// dorder places butter×2 (₹110 + ₹30 fee) to the near address.
func (d *delivery) dorder() string {
	d.t.Helper()
	r := d.dplace(d.near, 11000+dFee, line{d.butter, 2})
	require.Equal(d.t, http.StatusCreated, r.code, r.raw)
	return r.body["id"].(string)
}

// daccepted places a delivery order and has the cashier accept it.
func (d *delivery) daccepted() string {
	d.t.Helper()
	id := d.dorder()
	r := d.staff(d.cashier, id, "accept", map[string]any{})
	require.Equal(d.t, http.StatusOK, r.code, r.raw)
	return id
}

// dispatched is an accepted delivery order sent out.
func (d *delivery) dispatched() string {
	d.t.Helper()
	id := d.daccepted()
	r := d.staff(d.cashier, id, "dispatch", nil)
	require.Equal(d.t, http.StatusOK, r.code, r.raw)
	return id
}

// failed is a dispatched order whose delivery failed; it returns the
// failure's time.
func (d *delivery) failed() (string, time.Time) {
	d.t.Helper()
	id := d.dispatched()
	r := d.staff(d.cashier, id, "fail-delivery", map[string]any{"reason": "customer_unreachable"})
	require.Equal(d.t, http.StatusOK, r.code, r.raw)
	return id, d.a.clk.Now()
}

func (d *delivery) dispatchBy(id string) time.Time {
	d.t.Helper()
	var at *time.Time
	require.NoError(d.t, d.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT dispatch_by FROM orders WHERE id = $1`, uuidOf(d.t, id)).Scan(&at))
	require.NotNil(d.t, at)
	return *at
}

// returnRun runs the return job's step for id now.
func (d *delivery) returnRun(id string, step int) platformjobs.FakeRun {
	d.t.Helper()
	oid, err := ids.Parse(id)
	require.NoError(d.t, err)
	fr, err := d.runner.RunEvent(context.Background(), orders.KindDeliveryReturn, orders.TimerArgs{OrderID: oid, Step: step})
	require.NoError(d.t, err)
	return fr
}

func (d *delivery) reminders(id string) int {
	return d.a.count(`SELECT count(*) FROM notifications WHERE kind = 'delivery_return_pending' AND app = 'business' AND store_id = $1 AND ref_id = $2`,
		uuidOf(d.t, d.sid), uuidOf(d.t, id))
}

func (d *delivery) returnOutcome(id string) *string {
	d.t.Helper()
	var out *string
	require.NoError(d.t, d.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT return_outcome FROM orders WHERE id = $1`, uuidOf(d.t, id)).Scan(&out))
	return out
}

// BK-23: outside the radius the quote and the place refuse delivery; the
// minimum and the free-above amount compare the item subtotal inclusively;
// the fee is added below free-above; delivery needs an address of the
// customer's own, a store that delivers and feature_delivery_enabled; the
// order keeps a snapshot of the address.
func TestBK23DeliveryRadiusMinimumAndFee(t *testing.T) {
	d := newDeliveryShop(t)

	r := d.dquote(d.near, line{d.butter, 2})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 11000, r.body["itemSubtotalPaise"])
	require.EqualValues(t, dFee, r.body["deliveryFeePaise"])
	require.EqualValues(t, 11000+dFee, r.body["totalPaise"])

	r = d.dquote(d.far, line{d.butter, 2})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "store_not_bookable", problemCode(r))
	require.Equal(t, "outside_delivery_radius", ext(r, "reason"))
	r = d.dplace(d.far, 11000+dFee, line{d.butter, 2})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "outside_delivery_radius", ext(r, "reason"))

	// The minimum: ₹55 is under ₹100; exactly ₹100 (salt × 4) passes.
	r = d.dquote(d.near, line{d.butter, 1})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "below_minimum", problemCode(r))
	require.EqualValues(t, dMinimum, ext(r, "minimumPaise"))
	r = d.dplace(d.near, 5500+dFee, line{d.butter, 1})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "below_minimum", problemCode(r))
	r = d.dquote(d.near, line{d.salt, 4})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 10000+dFee, r.body["totalPaise"], "exactly the minimum passes")

	// Free delivery: ₹475 pays the fee, exactly ₹500 does not.
	r = d.dquote(d.near, line{d.salt, 19})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, dFee, r.body["deliveryFeePaise"])
	r = d.dquote(d.near, line{d.salt, 20})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 0, r.body["deliveryFeePaise"], "exactly the free-above amount is free")
	require.EqualValues(t, dFreeAbove, r.body["totalPaise"])

	// The address: required, and only the customer's own.
	body := d.dcart(d.near, []line{{d.butter, 2}}, 0)
	delete(body, "expectedTotalPaise")
	delete(body, "addressId")
	r = d.a.call(d.cust, http.MethodPost, "/v1/me/checkout/quote", body, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.addressId")
	theirs := d.address(d.cust2, "Home", 77.601, 12.9, true)
	r = d.dquote(theirs, line{d.butter, 2})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.addressId")

	// The place: the fee in the total, the address snapshotted.
	r = d.dplace(d.near, 11000+dFee, line{d.butter, 2})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.Equal(t, "delivery", r.body["fulfilment"])
	require.EqualValues(t, dFee, r.body["deliveryFeePaise"])
	require.EqualValues(t, 11000+dFee, r.body["totalPaise"])
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND delivery_fee_paise = $2 AND address_snapshot->>'pincode' = '560076'
	  AND address_snapshot->>'line1' = '12 4th Cross, BTM Layout'`, uuidOf(t, id), dFee))
	d.a.exec(`UPDATE addresses SET line1 = 'Moved away' WHERE id = $1`, uuidOf(t, d.near))
	g := d.a.call(d.cust, http.MethodGet, "/v1/me/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, g.code)
	addr, ok := g.body["deliveryAddress"].(map[string]any)
	require.True(t, ok, "a delivery order shows its address")
	require.Equal(t, "12 4th Cross, BTM Layout", addr["line1"], "the snapshot, not the address as edited")
	require.Equal(t, "Home", addr["label"])
	acceptBy, hold := d.times(id)
	require.NotNil(t, acceptBy)
	require.Nil(t, hold)

	// Delivery switched off platform-wide, or by the store.
	d.a.exec(`UPDATE platform_settings SET value = 'false' WHERE key = 'feature_delivery_enabled'`)
	d.settings.Invalidate()
	r = d.dquote(d.near, line{d.butter, 2})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "store_not_bookable", problemCode(r))
	r = d.quote(line{d.butter, 2})
	require.Equal(t, http.StatusOK, r.code, "pickup is unaffected: %s", r.raw)
	d.a.exec(`UPDATE platform_settings SET value = 'true' WHERE key = 'feature_delivery_enabled'`)
	d.settings.Invalidate()
	d.a.exec(`UPDATE stores SET accepts_delivery = false WHERE id = $1`, uuidOf(t, d.sid))
	r = d.dquote(d.near, line{d.butter, 2})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "store_not_bookable", problemCode(r))
}

// BK-23 (hours): outside the store's delivery hours the quote and the place
// answer delivery_closed with the next window; pickup is still open.
func TestBK23DeliveryClosed(t *testing.T) {
	d := newDeliveryShop(t)
	w := map[string]any{}
	for _, day := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		w[day] = []any{map[string]string{"open": "09:00", "close": "11:00"}}
	}
	hours, err := json.Marshal(map[string]any{"weekly": w, "holidays": []any{}})
	require.NoError(t, err)
	d.a.exec(`UPDATE stores SET delivery_hours = $1 WHERE id = $2`, hours, uuidOf(t, d.sid))

	// Friday 11:30 IST: the next window is Saturday 09:00–11:00 IST.
	r := d.dquote(d.near, line{d.butter, 2})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "delivery_closed", problemCode(r))
	next, ok := ext(r, "nextDeliveryWindow").(map[string]any)
	require.True(t, ok, r.raw)
	require.Equal(t, "2026-10-24T03:30:00Z", next["opensAt"])
	require.Equal(t, "2026-10-24T05:30:00Z", next["closesAt"])
	r = d.dplace(d.near, 11000+dFee, line{d.butter, 2})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "delivery_closed", problemCode(r))
	d.order() // pickup keeps the store's own hours

	d.set(time.Date(2026, 10, 24, 3, 30, 0, 0, time.UTC))
	r = d.dquote(d.near, line{d.butter, 2})
	require.Equal(t, http.StatusOK, r.code, "inside the window: %s", r.raw)
}

// BK-23b (D-94): the free-delivery amount and a minimum-bill offer both
// compare the item subtotal after item offers. ₹500 of salt: free delivery
// and ₹50 off → ₹450. ₹520 with a ₹30 item offer is ₹490: the fee applies
// and the minimum-bill offer does not.
func TestBK23bItemSubtotalForFeeAndBillOffer(t *testing.T) {
	d := newDeliveryShop(t)
	bill := ids.New(d.a.clk.Now()).String()
	d.a.exec(`INSERT INTO offers (id, store_id, kind, value, min_bill_paise, scope, store_item_ids, channels, starts_at, status)
	  VALUES ($1, $2, 'bill_min', 5000, 50000, 'store', '{}', 'both', $3, 'live')`,
		uuidOf(t, bill), uuidOf(t, d.sid), d.a.clk.Now().Add(-time.Hour))

	r := d.dquote(d.near, line{d.salt, 20})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 50000, r.body["itemSubtotalPaise"])
	require.EqualValues(t, 0, r.body["deliveryFeePaise"])
	require.EqualValues(t, 45000, r.body["totalPaise"])
	r = d.dplace(d.near, 45000, line{d.salt, 20})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.EqualValues(t, 0, r.body["deliveryFeePaise"])
	require.EqualValues(t, 45000, r.body["totalPaise"])

	// A ₹520 item with ₹30 off each unit.
	product := ids.New(d.a.clk.Now()).String()
	d.a.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, mrp_paise, barcodes) VALUES
	  ($1, 'Amul Ghee', '0190c47e-0000-7000-8000-000000000101', 1, 'l', 55000, '{8901262030014}')`, uuidOf(t, product))
	ir := d.a.call(d.manager, http.MethodPost, d.S+"/items", map[string]any{"productId": product, "pricePaise": 52000, "openingStock": 10}, nil)
	require.Equal(t, http.StatusCreated, ir.code, ir.raw)
	ghee := ir.body["id"].(string)
	flat := ids.New(d.a.clk.Now()).String()
	d.a.exec(`INSERT INTO offers (id, store_id, kind, value, scope, store_item_ids, channels, starts_at, status)
	  VALUES ($1, $2, 'flat', 3000, 'items', $3, 'both', $4, 'live')`,
		uuidOf(t, flat), uuidOf(t, d.sid), []any{uuidOf(t, ghee)}, d.a.clk.Now().Add(-time.Hour))
	r = d.dquote(d.near, line{ghee, 1})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 52000, r.body["subtotalPaise"])
	require.EqualValues(t, 49000, r.body["itemSubtotalPaise"])
	require.EqualValues(t, dFee, r.body["deliveryFeePaise"])
	require.EqualValues(t, 49000+dFee, r.body["totalPaise"], "no minimum-bill offer on ₹490")
}

// BK-24: accept starts the dispatch clock; dispatch sends it out and tells
// the customer; the code at the door delivers it: the reserved stock is sold
// (sale_online), the credits captured and cashback paid.
func TestBK24DeliverWithCode(t *testing.T) {
	d := newDeliveryShop(t)
	d.a.exec(`UPDATE stores SET cashback_bp = 500 WHERE id = $1`, uuidOf(t, d.sid))
	d.grantCredits(40)
	lines := []line{{d.butter, 2}}
	body := d.dcart(d.near, lines, 11000+dFee-1000)
	body["creditsToUse"] = 10
	r := d.a.call(d.cust, http.MethodPost, "/v1/me/orders", body, idem(d.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)

	r = d.staff(d.cashier, id, "dispatch", nil)
	require.Equal(t, http.StatusConflict, r.code, "a placed order is not dispatched: %s", r.raw)
	now := d.a.clk.Now()
	r = d.staff(d.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.True(t, d.dispatchBy(id).Equal(now.Add(60*time.Minute)), "dispatch_by = accept + delivery_dispatch_timeout_min")
	_, hold := d.times(id)
	require.Nil(t, hold, "a delivery order has no pickup hold")
	require.Equal(t, 1, d.timerJobs(orders.KindDispatchTimeout, id, d.dispatchBy(id)))
	require.Zero(t, d.a.count(`SELECT count(*) FROM river_job WHERE kind = $1 AND args->'a'->>'orderId' = $2`, orders.KindNoShow, id))
	require.Equal(t, 1, d.held(id))

	// The code is no use before dispatch.
	code := d.code(id)
	r = d.verify(d.cashier, id, code)
	require.Equal(t, http.StatusConflict, r.code, "verify before dispatch: status %d", r.code)

	r = d.staff(d.cashier, id, "dispatch", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "out_for_delivery", r.body["status"])
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND status = 'out_for_delivery'`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND params->>'variant' = 'out_for_delivery'`, uuidOf(t, id)))
	r = d.staff(d.cashier, id, "dispatch", nil)
	require.Equal(t, http.StatusConflict, r.code, "a second dispatch")
	r = d.me(d.cust, id, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusConflict, r.code, "no customer cancel once out for delivery")

	r = d.verify(d.cashier, id, code)
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	require.Equal(t, "delivered", r.body["status"])
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND collected_at IS NOT NULL AND payment_status = 'paid' AND cashback_credits > 0`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM stock_movements WHERE source = 'sale_online' AND ref_type = 'order' AND ref_id = $1`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM stock_reservations WHERE order_id = $1 AND status = 'consumed'`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM credit_holds WHERE order_id = $1 AND status = 'captured'`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND params->>'variant' = 'delivered'`, uuidOf(t, id)))
	onHand, reserved := d.stock(d.butter)
	require.Equal(t, 498, onHand)
	require.Zero(t, reserved)

	// A delivered order's dispatch timer finds nothing to do.
	d.set(d.dispatchBy(id))
	noopRun(t, d.timer(orders.KindDispatchTimeout, id, nil))
}

// BK-24 (cancels): before dispatch_by the customer, and a manager (not a
// cashier), may cancel an accepted delivery order; after it they may not.
func TestBK24CancelBeforeDispatch(t *testing.T) {
	d := newDeliveryShop(t)
	id := d.daccepted()
	r := d.me(d.cust, id, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Zero(t, d.held(id))

	id = d.daccepted()
	r = d.staff(d.cashier, id, "cancel", map[string]any{"reason": "out_of_stock"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = d.staff(d.manager, id, "cancel", map[string]any{"reason": "out_of_stock"})
	require.Equal(t, http.StatusOK, r.code, r.raw)

	id = d.daccepted()
	d.set(d.dispatchBy(id))
	r = d.me(d.cust, id, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusConflict, r.code, "past dispatch_by: %s", r.raw)
	r = d.staff(d.manager, id, "cancel", map[string]any{"reason": "out_of_stock"})
	require.Equal(t, http.StatusConflict, r.code, "past dispatch_by: %s", r.raw)
	r = d.staff(d.cashier, id, "dispatch", nil)
	require.Equal(t, http.StatusConflict, r.code, "past dispatch_by the order can only expire: %s", r.raw)
}

// BK-24 (partial): a partly accepted delivery order keeps the fee, and one
// that was free and drops under free-above pays the store's fee.
func TestBK24PartialRecomputesFee(t *testing.T) {
	d := newDeliveryShop(t)
	// ₹290 atta + ₹250 salt = ₹540: free delivery.
	r := d.dplace(d.near, 54000, line{d.atta, 1}, line{d.salt, 10})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.EqualValues(t, 0, r.body["deliveryFeePaise"])
	g := d.a.call(d.cashier, http.MethodGet, d.S+"/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, g.code, g.raw)
	qty := []int{1, 5} // keep the atta and half the salt: ₹415
	if g.body["lines"].([]any)[0].(map[string]any)["storeItemId"] == d.salt {
		qty = []int{5, 1}
	}
	r = d.partial(id, qty...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND delivery_fee_paise = $2 AND total_paise = $3`,
		uuidOf(t, id), dFee, 41500+dFee))
}

// BK-25: an accepted delivery order not dispatched by dispatch_by expires
// dispatch_timeout: the store gets a strike, the customer none; the
// reservation and the held credits go back. Not before; once only.
func TestBK25DispatchTimeout(t *testing.T) {
	d := newDeliveryShop(t)
	d.grantCredits(40)
	lines := []line{{d.butter, 2}}
	body := d.dcart(d.near, lines, 11000+dFee-1000)
	body["creditsToUse"] = 10
	r := d.a.call(d.cust, http.MethodPost, "/v1/me/orders", body, idem(d.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	r = d.staff(d.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	by := d.dispatchBy(id)

	d.set(by.Add(-time.Minute))
	fr := d.timer(orders.KindDispatchTimeout, id, nil)
	require.Equal(t, platformjobs.StatusFailed, fr.Status, "early: the run fails and is retried")
	require.Equal(t, "accepted", d.status(id))

	d.set(by)
	expiredRun(t, d.timer(orders.KindDispatchTimeout, id, nil))
	require.Equal(t, "expired", d.status(id))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND status_reason = 'dispatch_timeout'`, uuidOf(t, id)))
	require.Equal(t, 1, d.storeStrikes())
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM strikes WHERE subject_type = 'store' AND reason = 'dispatch_timeout' AND order_id = $1`, uuidOf(t, id)))
	require.Zero(t, d.strikes(), "no customer strike")
	require.Zero(t, d.held(id))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM stock_reservations WHERE order_id = $1 AND status = 'released' AND released_reason = 'dispatch_timeout'`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM credit_holds WHERE order_id = $1 AND status = 'released'`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND params->>'variant' = 'expired'`, uuidOf(t, id)))
	_, reserved := d.stock(d.butter)
	require.Zero(t, reserved)

	noopRun(t, d.timer(orders.KindDispatchTimeout, id, nil))
	require.Equal(t, 1, d.storeStrikes(), "a second run strikes nobody")

	// The sweep finds one whose timer was lost.
	id2 := d.daccepted()
	d.a.exec(`DELETE FROM river_job WHERE kind = $1`, orders.KindDispatchTimeout)
	d.set(d.dispatchBy(id2))
	fr = d.sweep()
	// grantCredits' own pickup order, never answered, expires in the same sweep.
	require.GreaterOrEqual(t, fr.Result.Summary["expired"], int64(1))
	require.Equal(t, "expired", d.status(id2))
	require.Equal(t, 2, d.storeStrikes())
}

// BK-26: a failed delivery releases the held credits, pays no cashback and
// moves no stock; the reservation stays until the shop settles the return:
// returned releases it, damaged (a manager; a cashier is 403) writes one
// damage movement per line, then releases. Once only.
func TestBK26FailedDeliveryAndReturn(t *testing.T) {
	d := newDeliveryShop(t)
	id := d.daccepted()
	r := d.staff(d.cashier, id, "fail-delivery", map[string]any{"reason": "customer_unreachable"})
	require.Equal(t, http.StatusConflict, r.code, "fail before dispatch: %s", r.raw)
	r = d.staff(d.cashier, id, "dispatch", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	code := d.code(id)

	r = d.staff(d.cashier, id, "fail-delivery", map[string]any{"reason": "dog_ate_it"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = d.staff(d.cashier, id, "fail-delivery", map[string]any{"reason": "other"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "other needs a note: %s", r.raw)
	r = d.staff(d.cashier, id, "confirm-return", map[string]any{"outcome": "returned"})
	require.Equal(t, http.StatusConflict, r.code, "nothing to return yet: %s", r.raw)
	movements := d.a.count(`SELECT count(*) FROM stock_movements`)

	r = d.staff(d.cashier, id, "fail-delivery", map[string]any{"reason": "other", "note": "Gate locked, no answer"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "delivery_failed", r.body["status"])
	require.Equal(t, "other", r.body["statusReason"])
	failedAt := d.a.clk.Now()
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND delivery_failed_at = $2 AND cashback_credits = 0
	  AND return_outcome IS NULL AND payment_status <> 'paid'`, uuidOf(t, id), failedAt))
	require.Equal(t, movements, d.a.count(`SELECT count(*) FROM stock_movements`), "no stock movement")
	require.Equal(t, 1, d.held(id), "the reservation stays held")
	require.Equal(t, 1, d.timerJobs(orders.KindDeliveryReturn, id, failedAt.Add(2*time.Hour)))
	r = d.staff(d.cashier, id, "fail-delivery", map[string]any{"reason": "customer_unreachable"})
	require.Equal(t, http.StatusConflict, r.code, "a second failure")
	r = d.verify(d.cashier, id, code)
	require.Equal(t, http.StatusConflict, r.code, "no hand-over after a failure: status %d", r.code)

	r = d.staff(d.cashier, id, "confirm-return", map[string]any{"outcome": "returned"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "delivery_failed", r.body["status"])
	require.Equal(t, "returned", r.body["returnOutcome"])
	require.NotNil(t, r.body["returnConfirmedAt"])
	require.Zero(t, d.held(id))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM stock_reservations WHERE order_id = $1 AND status = 'released' AND released_reason = 'return_returned'`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND return_confirmed_by IS NOT NULL`, uuidOf(t, id)))
	onHand, reserved := d.stock(d.butter)
	require.Equal(t, 500, onHand)
	require.Zero(t, reserved)
	r = d.staff(d.cashier, id, "confirm-return", map[string]any{"outcome": "returned"})
	require.Equal(t, http.StatusConflict, r.code, "settled once")
	require.Equal(t, "invalid_transition", problemCode(r))

	// Damaged: a cashier cannot record it; a manager writes the damage.
	id, _ = d.failed()
	r = d.staff(d.cashier, id, "confirm-return", map[string]any{"outcome": "damaged"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, 1, d.held(id))
	r = d.staff(d.manager, id, "confirm-return", map[string]any{"outcome": "damaged"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "damaged", r.body["returnOutcome"])
	require.Zero(t, d.held(id))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM stock_movements WHERE source = 'damage' AND store_item_id = $1 AND delta = -2`, uuidOf(t, d.butter)))
	onHand, reserved = d.stock(d.butter)
	require.Equal(t, 498, onHand)
	require.Zero(t, reserved)
	r = d.staff(d.manager, id, "confirm-return", map[string]any{"outcome": "returned"})
	require.Equal(t, http.StatusConflict, r.code, "settled once")
}

// BK-26 (credits): a failed delivery releases the held credits.
func TestBK26FailedDeliveryReleasesCredits(t *testing.T) {
	d := newDeliveryShop(t)
	cust := d.grantCredits(40)
	body := d.dcart(d.near, []line{{d.butter, 2}}, 11000+dFee-1000)
	body["creditsToUse"] = 10
	r := d.a.call(d.cust, http.MethodPost, "/v1/me/orders", body, idem(d.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.True(t, d.walletIs(cust, 30))
	for _, a := range []string{"accept", "dispatch"} {
		r = d.staff(d.cashier, id, a, map[string]any{})
		require.Equal(t, http.StatusOK, r.code, r.raw)
	}
	r = d.staff(d.cashier, id, "fail-delivery", map[string]any{"reason": "address_not_found"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM credit_holds WHERE order_id = $1 AND status = 'released'`, uuidOf(t, id)))
	require.True(t, d.walletIs(cust, 40))
}

// BK-26 (the return job): reminders to the store's inbox at 2 h and 24 h
// (not at 1 h 59 m); at 72 h (not 71 h 59 m) the return is settled
// unconfirmed by nobody, the stock released and a risk flag raised.
func TestBK26ReturnRemindersAndAutoConfirm(t *testing.T) {
	d := newDeliveryShop(t)
	id, at := d.failed()

	d.set(at.Add(2*time.Hour - time.Minute))
	fr := d.returnRun(id, 1)
	require.Equal(t, platformjobs.StatusFailed, fr.Status, "1 h 59 m: early")
	require.Zero(t, d.reminders(id))

	d.set(at.Add(2 * time.Hour))
	fr = d.returnRun(id, 1)
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 1, fr.Result.Summary["reminded"])
	require.Equal(t, 1, d.reminders(id))
	require.Equal(t, 1, d.timerJobs(orders.KindDeliveryReturn, id, at.Add(24*time.Hour)))
	fr = d.returnRun(id, 1)
	require.EqualValues(t, 1, fr.Result.Summary["noop"], "a repeated run reminds nobody")
	require.Equal(t, 1, d.reminders(id))

	d.set(at.Add(24*time.Hour - time.Minute))
	require.Equal(t, platformjobs.StatusFailed, d.returnRun(id, 2).Status)
	d.set(at.Add(24 * time.Hour))
	fr = d.returnRun(id, 2)
	require.EqualValues(t, 1, fr.Result.Summary["reminded"])
	require.Equal(t, 2, d.reminders(id))
	require.Equal(t, 1, d.timerJobs(orders.KindDeliveryReturn, id, at.Add(72*time.Hour)))

	d.set(at.Add(72*time.Hour - time.Minute))
	require.Equal(t, platformjobs.StatusFailed, d.returnRun(id, 3).Status, "71 h 59 m: nothing yet")
	require.Equal(t, 1, d.held(id))
	require.Nil(t, d.returnOutcome(id))

	d.set(at.Add(72 * time.Hour))
	fr = d.returnRun(id, 3)
	require.EqualValues(t, 1, fr.Result.Summary["settled"])
	out := d.returnOutcome(id)
	require.NotNil(t, out)
	require.Equal(t, "unconfirmed", *out)
	require.Equal(t, "delivery_failed", d.status(id))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND return_confirmed_by IS NULL AND return_confirmed_at = $2`, uuidOf(t, id), d.a.clk.Now()))
	require.Zero(t, d.held(id))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM stock_reservations WHERE order_id = $1 AND released_reason = 'return_unconfirmed'`, uuidOf(t, id)))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM risk_flags WHERE subject_type = 'store' AND subject_id = $1 AND rule = 'return_unconfirmed'
	  AND status = 'open' AND details->>'orderId' = $2`, uuidOf(t, d.sid), id))
	fr = d.returnRun(id, 3)
	require.EqualValues(t, 1, fr.Result.Summary["noop"])
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM risk_flags WHERE rule = 'return_unconfirmed'`), "one flag per order")
	r := d.staff(d.manager, id, "confirm-return", map[string]any{"outcome": "returned"})
	require.Equal(t, http.StatusConflict, r.code, "after the auto-confirm: %s", r.raw)
}

// BK-26 (sweep): with every return job lost, the reconcile sweep still
// reminds at 2 h and 24 h and settles at 72 h; a settled return is left.
func TestBK26ReturnSweep(t *testing.T) {
	d := newDeliveryShop(t)
	id, at := d.failed()
	settled, _ := d.failed()
	r := d.staff(d.cashier, settled, "confirm-return", map[string]any{"outcome": "returned"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	d.a.exec(`DELETE FROM river_job WHERE kind = $1`, orders.KindDeliveryReturn)

	d.set(at.Add(2*time.Hour - time.Minute))
	d.sweep()
	require.Zero(t, d.reminders(id))
	d.set(at.Add(2 * time.Hour))
	d.sweep()
	require.Equal(t, 1, d.reminders(id))
	d.set(at.Add(2*time.Hour + time.Minute)) // the sweep runs once a minute
	d.sweep()
	require.Equal(t, 1, d.reminders(id), "once per step")
	d.set(at.Add(24 * time.Hour))
	d.sweep()
	require.Equal(t, 2, d.reminders(id))
	d.set(at.Add(72 * time.Hour))
	d.sweep()
	out := d.returnOutcome(id)
	require.NotNil(t, out)
	require.Equal(t, "unconfirmed", *out)
	require.Zero(t, d.reminders(settled), "a settled return is never reminded")
	out = d.returnOutcome(settled)
	require.NotNil(t, out)
	require.Equal(t, "returned", *out)
}

// BK-26 (D-218): the two reminders follow delivery_return_first_reminder_hours
// and delivery_return_second_reminder_hours, for the timers and the sweep;
// the 72 h auto-confirm does not move.
func TestBK26ReturnReminderSettings(t *testing.T) {
	d := newDeliveryShop(t)
	d.a.exec(`UPDATE platform_settings SET value = '3' WHERE key = 'delivery_return_first_reminder_hours'`)
	d.a.exec(`UPDATE platform_settings SET value = '30' WHERE key = 'delivery_return_second_reminder_hours'`)
	d.settings.Invalidate()
	id, at := d.failed()
	require.Equal(t, 1, d.timerJobs(orders.KindDeliveryReturn, id, at.Add(3*time.Hour)))

	d.set(at.Add(2 * time.Hour))
	require.Equal(t, platformjobs.StatusFailed, d.returnRun(id, 1).Status, "2 h: early under a 3 h setting")
	d.set(at.Add(3 * time.Hour))
	fr := d.returnRun(id, 1)
	require.EqualValues(t, 1, fr.Result.Summary["reminded"], "%v", fr.Err)
	require.Equal(t, 1, d.timerJobs(orders.KindDeliveryReturn, id, at.Add(30*time.Hour)))

	swept, sat := d.failed()
	d.a.exec(`DELETE FROM river_job WHERE kind = $1`, orders.KindDeliveryReturn)
	d.set(sat.Add(24 * time.Hour))
	d.sweep()
	require.Equal(t, 1, d.reminders(swept), "24 h: only the first reminder under a 30 h setting")
	d.set(sat.Add(30 * time.Hour))
	d.sweep()
	require.Equal(t, 2, d.reminders(swept))
	d.set(sat.Add(72*time.Hour - time.Minute))
	d.sweep()
	require.Nil(t, d.returnOutcome(swept))
	d.set(sat.Add(72 * time.Hour))
	d.sweep()
	out := d.returnOutcome(swept)
	require.NotNil(t, out)
	require.Equal(t, "unconfirmed", *out)
}

// BK-27: the shop sees the customer's full phone (detail and list) only
// while the order is out_for_delivery; masked before and after.
func TestBK27PhoneOnlyWhileOutForDelivery(t *testing.T) {
	d := newDeliveryShop(t)
	phone := func(id, tab string) (detail, listed any) {
		t.Helper()
		g := d.a.call(d.cashier, http.MethodGet, d.S+"/orders/"+id, nil, nil)
		require.Equal(t, http.StatusOK, g.code, g.raw)
		c := g.body["customer"].(map[string]any)
		require.NotEmpty(t, c["phoneMasked"])
		l := d.a.call(d.cashier, http.MethodGet, d.S+"/orders?status="+tab, nil, nil)
		require.Equal(t, http.StatusOK, l.code, l.raw)
		found := false
		for _, it := range l.body["items"].([]any) {
			if m := it.(map[string]any); m["id"] == id {
				listed, found = m["customerPhone"], true
			}
		}
		require.True(t, found, "listed in the %s tab", tab)
		return c["phone"], listed
	}
	isFull := func(v any) {
		t.Helper()
		s, ok := v.(string)
		require.True(t, ok, "the full phone is shown")
		require.Contains(t, s, "9000000014")
	}

	id := d.daccepted()
	p, l := phone(id, "accepted")
	require.Nil(t, p, "accepted: masked")
	require.Nil(t, l)
	r := d.staff(d.cashier, id, "dispatch", nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Contains(t, r.body["customer"].(map[string]any)["phone"], "9000000014")
	p, l = phone(id, "out_for_delivery")
	isFull(p)
	isFull(l)
	r = d.verify(d.cashier, id, d.code(id))
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	p, l = phone(id, "done")
	require.Nil(t, p, "delivered: masked")
	require.Nil(t, l)

	id, _ = d.failed()
	p, l = phone(id, "returns")
	require.Nil(t, p, "delivery_failed: masked")
	require.Nil(t, l)

	// A pickup order never shows it.
	pick := d.accepted()
	p, l = phone(pick, "accepted")
	require.Nil(t, p)
	require.Nil(t, l)
}

// BK-41: no-show is pickup only: a delivery order still accepted 30 min
// after the accept is not expired no_show and the customer gets no strike;
// only the dispatch timeout ends it.
func TestBK41NoNoShowForDelivery(t *testing.T) {
	d := newDeliveryShop(t)
	id := d.daccepted()
	d.advance(30 * time.Minute)
	noopRun(t, d.timer(orders.KindNoShow, id, nil))
	d.sweep()
	require.Equal(t, "accepted", d.status(id))
	require.Zero(t, d.strikes())
	require.Zero(t, d.a.count(`SELECT count(*) FROM orders WHERE status_reason = 'no_show'`))

	d.set(d.dispatchBy(id))
	d.sweep()
	require.Equal(t, "expired", d.status(id))
	require.Equal(t, 1, d.a.count(`SELECT count(*) FROM orders WHERE id = $1 AND status_reason = 'dispatch_timeout'`, uuidOf(t, id)))
	require.Zero(t, d.strikes(), "still no customer strike")
}
