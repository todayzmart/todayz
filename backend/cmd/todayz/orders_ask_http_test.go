package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// W3-16: booking from an ask reply (01 §11.2, D-101) through the orders
// routes: the reply's price while it holds, the asked quantity as a cap, the
// catalogue price once the price has ended, and one open order per reply.

// askYes puts store S's Yes on an ask of the customer tok for the atta, qty
// units, replied 4 minutes ago. price 0 is a Yes without a price; otherwise
// it holds until validUntil. It returns the reply id.
func (o *orderShop) askYes(tok string, qty, price int, validUntil time.Time) string {
	o.t.Helper()
	me := o.a.call(tok, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(o.t, http.StatusOK, me.code, me.raw)
	now := o.a.clk.Now()
	askID, replyID := ids.New(now).String(), ids.New(now).String()
	o.a.exec(`INSERT INTO asks (id, customer_id, product_id, query_norm, quantity, radius_m, recipients_count, created_at)
	  SELECT $1, $2, si.product_id, 'aashirvaad atta', $3, 2000, 1, $4 FROM store_items si WHERE si.id = $5`,
		uuidOf(o.t, askID), uuidOf(o.t, me.body["id"].(string)), qty, now.Add(-10*time.Minute), uuidOf(o.t, o.atta))
	var pricePaise, until any
	if price > 0 {
		pricePaise, until = price, validUntil
	}
	o.a.exec(`INSERT INTO ask_recipients (id, ask_id, store_id, sent_at, distance_m, distance_band, reply_by, answer,
	    price_paise, store_item_id, replied_at, price_valid_until)
	  VALUES ($1, $2, $3, $4, 500, 'lt_1km', $5, 'yes', $6, $7, $8, $9)`,
		uuidOf(o.t, replyID), uuidOf(o.t, askID), uuidOf(o.t, o.sid), now.Add(-10*time.Minute), now.Add(5*time.Minute),
		pricePaise, uuidOf(o.t, o.atta), now.Add(-4*time.Minute), until)
	return replyID
}

// askCart is cart with the reply id.
func (o *orderShop) askCart(reply string, qty, expected int) map[string]any {
	c := o.cart([]line{{o.atta, qty}}, expected)
	c["askReplyId"] = reply
	return c
}

func (o *orderShop) askQuote(tok, reply string, qty int) resp {
	o.t.Helper()
	body := o.askCart(reply, qty, 0)
	delete(body, "expectedTotalPaise")
	return o.a.call(tok, http.MethodPost, "/v1/me/checkout/quote", body, nil)
}

func (o *orderShop) askPlace(tok, reply string, qty, expected int) resp {
	o.t.Helper()
	return o.a.call(tok, http.MethodPost, "/v1/me/orders", o.askCart(reply, qty, expected), idem(o.a))
}

func (o *orderShop) events(name, source string) int {
	if source == "" {
		return o.a.count(`SELECT count(*) FROM events WHERE name = $1`, name)
	}
	return o.a.count(`SELECT count(*) FROM events WHERE name = $1 AND props->>'price_source' = $2`, name, source)
}

func lineSource(t *testing.T, r resp) any {
	t.Helper()
	ls, ok := r.body["lines"].([]any)
	require.True(t, ok, r.raw)
	require.Len(t, ls, 1)
	return ls[0].(map[string]any)["priceSource"]
}

// A priced Yes books at the reply's price, at most the asked quantity (422
// on both the quote and the order above it), and writes ask_booked.
func TestAskBookPricedReply(t *testing.T) {
	o := newOrderShop(t)
	reply := o.askYes(o.cust, 2, 27000, o.a.clk.Now().Add(20*time.Minute))

	for _, r := range []resp{o.askQuote(o.cust, reply, 3), o.askPlace(o.cust, reply, 3, 81000)} {
		require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
		require.Contains(t, r.raw, "body.items[0].quantity")
		require.Contains(t, r.raw, "more than the ask")
	}

	q := o.askQuote(o.cust, reply, 2)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.EqualValues(t, 54000, q.body["totalPaise"])
	require.Equal(t, "ask_reply", lineSource(t, q))

	r := o.askPlace(o.cust, reply, 2, 54000)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.EqualValues(t, 54000, r.body["totalPaise"])
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE ask_reply_id = $1`, uuidOf(t, reply)))
	require.Equal(t, 1, o.events("ask_booked", "ask_reply"))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM events WHERE name = 'ask_booked' AND (props->>'minutes_since_reply')::int = 4`))
	require.Zero(t, o.events("ask_price_expired", ""))
}

// The price holds until price_valid_until, exclusive: a second before it the
// reply prices the line; at it the quote is at the catalogue price, and
// placing with the reply is 409 ask_price_expired with that quote, writes
// ask_price_expired, and places nothing. Without the reply id it books.
func TestAskBookExpiredReply(t *testing.T) {
	o := newOrderShop(t)
	until := o.a.clk.Now().Add(20 * time.Minute)
	reply := o.askYes(o.cust, 2, 27000, until)

	o.set(until.Add(-time.Second))
	q := o.askQuote(o.cust, reply, 2)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.Equal(t, "ask_reply", lineSource(t, q))

	o.set(until)
	q = o.askQuote(o.cust, reply, 2)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.EqualValues(t, 58000, q.body["totalPaise"], "at valid-until the catalogue price")
	require.Equal(t, "catalogue", lineSource(t, q))

	r := o.askPlace(o.cust, reply, 2, 54000)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "ask_price_expired", problemCode(r))
	fresh, ok := ext(r, "quote").(map[string]any)
	require.True(t, ok, r.raw)
	require.EqualValues(t, 58000, fresh["totalPaise"])
	require.Zero(t, o.a.count(`SELECT count(*) FROM orders`))
	require.Equal(t, 1, o.events("ask_price_expired", ""))

	r = o.place(o.cust, line{o.atta, 2})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Zero(t, o.a.count(`SELECT count(*) FROM orders WHERE ask_reply_id IS NOT NULL`))
	require.Zero(t, o.events("ask_booked", ""))
}

// A Yes without a price has no expiry: it books at the catalogue price, still
// capped at the asked quantity, and is recorded as a booking from the ask.
func TestAskBookYesWithoutPrice(t *testing.T) {
	o := newOrderShop(t)
	reply := o.askYes(o.cust, 2, 0, time.Time{})
	o.advance(48 * time.Hour)

	r := o.askQuote(o.cust, reply, 3)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, r.raw, "more than the ask")

	q := o.askQuote(o.cust, reply, 2)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.EqualValues(t, 58000, q.body["totalPaise"])
	require.Equal(t, "catalogue", lineSource(t, q))

	r = o.askPlace(o.cust, reply, 2, 58000)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE ask_reply_id = $1`, uuidOf(t, reply)))
	require.Equal(t, 1, o.events("ask_booked", "catalogue"))
}

// Another customer's reply id gets the same answer as an expired one (409
// ask_price_expired with the catalogue quote): nothing tells them the id is
// someone's Yes. It is not counted as an expired price.
func TestAskBookAnotherCustomersReply(t *testing.T) {
	o := newOrderShop(t)
	reply := o.askYes(o.cust, 2, 27000, o.a.clk.Now().Add(20*time.Minute))

	q := o.askQuote(o.cust2, reply, 2)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.EqualValues(t, 58000, q.body["totalPaise"])
	require.Equal(t, "catalogue", lineSource(t, q))

	r := o.askPlace(o.cust2, reply, 2, 54000)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "ask_price_expired", problemCode(r))
	fresh, ok := ext(r, "quote").(map[string]any)
	require.True(t, ok, r.raw)
	require.EqualValues(t, 58000, fresh["totalPaise"])
	ex, _ := r.body["extensions"].(map[string]any)
	require.Len(t, ex, 1, "the same extensions as an expired price")
	require.Zero(t, o.a.count(`SELECT count(*) FROM orders`))
	require.Zero(t, o.events("ask_price_expired", ""))
}

// Two placements from one reply at once: exactly one order, the other is
// 409 ask_reply_used.
func TestAskBookConcurrentOnOneReply(t *testing.T) {
	o := newOrderShop(t)
	reply := o.askYes(o.cust, 2, 27000, o.a.clk.Now().Add(20*time.Minute))

	var a, b resp
	parallel(
		func() { a = o.askPlace(o.cust, reply, 2, 54000) },
		func() { b = o.askPlace(o.cust, reply, 2, 54000) },
	)
	codes := []int{a.code, b.code}
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, codes, "%s | %s", a.raw, b.raw)
	lost := a
	if a.code == http.StatusCreated {
		lost = b
	}
	require.Equal(t, "ask_reply_used", problemCode(lost))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM orders WHERE ask_reply_id = $1`, uuidOf(t, reply)))
	require.Equal(t, 1, o.events("ask_booked", ""))
}

// ask_price_expired is written only when the refusal is the reply's own price
// having ended for an item in the cart: an ended reply sent with a cart
// without its item, or another customer's ended reply, is refused the same
// way and writes nothing.
func TestAskPriceExpiredOnlyForAnEndedPrice(t *testing.T) {
	o := newOrderShop(t)
	until := o.a.clk.Now().Add(20 * time.Minute)
	reply := o.askYes(o.cust, 2, 27000, until)
	o.set(until.Add(time.Minute))

	c := o.cart([]line{{o.butter, 1}}, o.price[o.butter])
	c["askReplyId"] = reply
	r := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", c, idem(o.a))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "ask_price_expired", problemCode(r))
	require.Zero(t, o.events("ask_price_expired", ""), "the reply's item is not in the cart")

	r = o.askPlace(o.cust2, reply, 2, 54000)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "ask_price_expired", problemCode(r))
	require.Zero(t, o.events("ask_price_expired", ""), "another customer's reply")

	r = o.askPlace(o.cust, reply, 2, 54000)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, 1, o.events("ask_price_expired", ""))
	require.Zero(t, o.a.count(`SELECT count(*) FROM orders`))
}
