package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// D-220: the customer's own GET of an open order carries the shop's contact
// phone; a finished order, a shop without one, lists and action responses do
// not. The phone is compared, never printed.

const shopPhone = "+919000000777"

// setShopPhone seals phone as S's contact phone the way business does.
func (o *orderShop) setShopPhone(phone string) {
	o.t.Helper()
	seal, err := pii.NewSealer(o.cfg.Keys.PIIEncKey.Bytes())
	require.NoError(o.t, err)
	sid, err := ids.Parse(o.sid)
	require.NoError(o.t, err)
	enc, err := seal.Seal(business.SealField(business.FieldPhone, sid), []byte(phone))
	require.NoError(o.t, err)
	o.a.exec(`UPDATE stores SET phone_enc = $1 WHERE id = $2`, enc, uuidOf(o.t, o.sid))
}

// shopPhoneOf is store.phone on the customer's GET of order id.
func (o *orderShop) shopPhoneOf(id string) any {
	o.t.Helper()
	g := o.a.call(o.cust, http.MethodGet, "/v1/me/orders/"+id, nil, nil)
	require.Equal(o.t, http.StatusOK, g.code, "status %d", g.code)
	st := g.body["store"].(map[string]any)
	v, ok := st["phone"]
	require.True(o.t, ok, "store.phone is always present")
	return v
}

func isShopPhone(t *testing.T, v any, msg string) {
	t.Helper()
	s, ok := v.(string)
	require.True(t, ok && s == shopPhone, msg)
}

func TestD220ShopPhoneWhileTheOrderIsOpen(t *testing.T) {
	o := newOrderShop(t)
	o.setShopPhone(shopPhone)

	placed := o.place(o.cust, line{o.butter, 1})
	require.Equal(t, http.StatusCreated, placed.code, "status %d", placed.code)
	require.Nil(t, placed.body["store"].(map[string]any)["phone"], "the place response has no phone")
	id := placed.body["id"].(string)
	isShopPhone(t, o.shopPhoneOf(id), "placed: the shop's phone")

	l := o.a.call(o.cust, http.MethodGet, "/v1/me/orders", nil, nil)
	require.Equal(t, http.StatusOK, l.code, "status %d", l.code)
	for _, it := range l.body["items"].([]any) {
		require.Nil(t, it.(map[string]any)["store"].(map[string]any)["phone"], "lists carry no shop phone")
	}

	r := o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, "status %d", r.code)
	isShopPhone(t, o.shopPhoneOf(id), "accepted: the shop's phone")

	r = o.verify(o.cashier, id, o.code(id))
	require.Equal(t, http.StatusOK, r.code, "status %d", r.code)
	require.Nil(t, o.shopPhoneOf(id), "collected: no phone")

	open := o.order()
	isShopPhone(t, o.shopPhoneOf(open), "a second open order: the shop's phone")
	c := o.me(o.cust, open, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusOK, c.code, "status %d", c.code)
	require.Nil(t, c.body["store"].(map[string]any)["phone"], "the cancel response has no phone")
	require.Nil(t, o.shopPhoneOf(open), "cancelled: no phone")
}

func TestD220NoShopPhoneNoFallback(t *testing.T) {
	o := newOrderShop(t)
	// S has no contact phone: the owner's login phone is never used instead.
	o.a.exec(`UPDATE stores SET phone_enc = NULL WHERE id = $1`, uuidOf(t, o.sid))
	id := o.order()
	require.Nil(t, o.shopPhoneOf(id), "no shop phone: null, never the owner's")
}

// storePhone is store.phone in an order body (an action response).
func storePhone(t *testing.T, r resp) any {
	t.Helper()
	require.Equal(t, http.StatusOK, r.code, "status %d", r.code)
	st := r.body["store"].(map[string]any)
	v, ok := st["phone"]
	require.True(t, ok, "store.phone is always present")
	return v
}

// The other open statuses show the phone; the other finished ones and the
// confirm-partial and extend responses do not.
func TestD220ShopPhoneByStatus(t *testing.T) {
	o := newOrderShop(t)
	o.setShopPhone(shopPhone)

	id := o.orderOf(line{o.butter, 2}, line{o.salt, 1})
	require.Equal(t, http.StatusOK, o.partial(id, 1, 1).code)
	require.Equal(t, "awaiting_customer", o.status(id))
	isShopPhone(t, o.shopPhoneOf(id), "awaiting_customer: the shop's phone")
	r := o.me(o.cust, id, "confirm-partial", map[string]any{"accept": true})
	require.Nil(t, storePhone(t, r), "the confirm-partial response has no phone")
	isShopPhone(t, o.shopPhoneOf(id), "accepted after confirm-partial: the shop's phone")

	r = o.staff(o.cashier, id, "ready", nil)
	require.Equal(t, http.StatusOK, r.code, "status %d", r.code)
	isShopPhone(t, o.shopPhoneOf(id), "ready: the shop's phone")

	id = o.order()
	r = o.staff(o.cashier, id, "reject", map[string]any{"reason": "out_of_stock"})
	require.Equal(t, http.StatusOK, r.code, "status %d", r.code)
	require.Nil(t, o.shopPhoneOf(id), "rejected: no phone")

	id = o.order()
	o.advance(10 * time.Minute)
	expiredRun(t, o.timer(orders.KindAcceptTimeout, id, nil))
	require.Equal(t, "expired", o.status(id))
	require.Nil(t, o.shopPhoneOf(id), "expired: no phone")

	id = o.accepted()
	o.advance(25 * time.Minute)
	r = o.me(o.cust, id, "extend", nil)
	require.Nil(t, storePhone(t, r), "the extend response has no phone")
	isShopPhone(t, o.shopPhoneOf(id), "accepted after extend: the shop's phone")
}

func TestD220ShopPhoneOnDelivery(t *testing.T) {
	d := newDeliveryShop(t)
	d.setShopPhone(shopPhone)

	id := d.dispatched()
	require.Equal(t, "out_for_delivery", d.status(id))
	isShopPhone(t, d.shopPhoneOf(id), "out_for_delivery: the shop's phone")

	id, _ = d.failed()
	require.Equal(t, "delivery_failed", d.status(id))
	require.Nil(t, d.shopPhoneOf(id), "delivery_failed: no phone")
}
