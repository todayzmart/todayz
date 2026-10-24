package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCR02PlacingRefusesCreditsItCannotUse (CR-02, 04 §2): the quote clamps
// the credits asked and says why (D-76), but placing never uses fewer than
// asked. 31 credits with the max at 30 is credit_max_exceeded even when the
// client's total matches the clamped quote; credits on a bill under ₹100 are
// below_credit_minimum, even when they are also over the max (the quote's
// order of the rules); a wallet that no longer covers them is
// price_changed. Nothing is held or placed by a refusal.
func TestCR02PlacingRefusesCreditsItCannotUse(t *testing.T) {
	o := newOrderShop(t)
	cust := o.grantCredits(40)
	lines := []line{{o.butter, 2}, {o.atta, 1}}
	orders := func() int { return o.a.count(`SELECT count(*) FROM orders WHERE customer_id = $1`, cust) }
	before := orders()

	q := o.a.call(o.cust, http.MethodPost, "/v1/me/checkout/quote", func() map[string]any {
		b := o.cart(lines, 0)
		delete(b, "expectedTotalPaise")
		b["creditsToUse"] = 31
		return b
	}(), nil)
	require.Equal(t, http.StatusOK, q.code, q.raw)
	require.EqualValues(t, 30, q.body["creditsUsed"], "the quote clamps to the max")
	require.Equal(t, "credit_max_exceeded", q.body["creditsLimit"])

	placeAsking := func(n int, ls []line, expected int) resp {
		t.Helper()
		body := o.cart(ls, expected)
		body["creditsToUse"] = n
		return o.a.call(o.cust, http.MethodPost, "/v1/me/orders", body, idem(o.a))
	}
	r := placeAsking(31, lines, int(q.body["totalPaise"].(float64)))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "credit_max_exceeded", problemCode(r))

	small := []line{{o.butter, 1}} // ₹55, under the ₹100 credit minimum
	r = placeAsking(5, small, o.total(small))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "below_credit_minimum", problemCode(r))

	// Over the max on a bill under the minimum: the quote's order of the
	// rules decides (the minimum first), so placing names the same rule the
	// quote does (lead review 3).
	sq := o.a.call(o.cust, http.MethodPost, "/v1/me/checkout/quote", func() map[string]any {
		b := o.cart(small, 0)
		delete(b, "expectedTotalPaise")
		b["creditsToUse"] = 31
		return b
	}(), nil)
	require.Equal(t, http.StatusOK, sq.code, sq.raw)
	require.Equal(t, "below_credit_minimum", sq.body["creditsLimit"])
	r = placeAsking(31, small, o.total(small))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "below_credit_minimum", problemCode(r))

	require.Equal(t, before, orders(), "a refusal places nothing")
	require.True(t, o.walletIs(cust, 40), "and holds nothing")

	// 30 is fine; the wallet then has 10 left, so 20 more is price_changed
	// with the quote as it is now (10 credits).
	o.placeWithCredits(30)
	r = placeAsking(20, lines, o.total(lines)-2000)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "price_changed", problemCode(r))
	quote, ok := ext(r, "quote").(map[string]any)
	require.True(t, ok, r.raw)
	require.EqualValues(t, 10, quote["creditsUsed"])

	// 31 with 10 in the wallet: the quote's limit is the wallet, which has
	// no problem of its own, and 31 is over the max, so credit_max_exceeded.
	r = placeAsking(31, lines, o.total(lines)-1000)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "credit_max_exceeded", problemCode(r))
}
