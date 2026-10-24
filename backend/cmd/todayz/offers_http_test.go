package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/search"
)

// offerOn inserts a live store-wide percent offer for the shop (the engine
// reads rows; W2-07's own tests cover the manage API).
func (o *orderShop) offerOn(kind string, value int64, maxRedeem *int) string {
	o.t.Helper()
	id := ids.New(o.a.clk.Now()).String()
	o.a.exec(`INSERT INTO offers (id, store_id, kind, value, scope, store_item_ids, channels, max_redemptions, starts_at, status)
	  VALUES ($1, $2, $3, $4, 'store', '{}', 'both', $5, $6, 'live')`,
		uuidOf(o.t, id), uuidOf(o.t, o.sid), kind, value, maxRedeem, o.a.clk.Now().Add(-time.Hour))
	return id
}

func (o *orderShop) redemptions(offer string, released bool) int {
	return o.a.count(`SELECT count(*) FROM offer_redemptions WHERE offer_id = $1 AND (released_at IS NOT NULL) = $2`,
		uuidOf(o.t, offer), released)
}

// W2-07 through orders: the real offers service prices the quote, the order
// redeems it in its transaction, and a cancel gives it back.
func TestOfferPricesQuoteRedeemsAndReleases(t *testing.T) {
	o := newOrderShop(t)
	offer := o.offerOn("percent", 10, nil)
	lines := []line{{o.butter, 2}} // 2 × ₹55 = ₹110 → 10% = ₹11

	r := o.quote(lines...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 11000, r.body["subtotalPaise"])
	require.EqualValues(t, 9900, r.body["itemsAfterOffersPaise"])
	require.EqualValues(t, 1100, r.body["discountPaise"])
	require.EqualValues(t, 9900, r.body["totalPaise"])
	applied, ok := r.body["offersApplied"].([]any)
	require.True(t, ok, r.raw)
	require.Len(t, applied, 1)
	require.Equal(t, offer, applied[0].(map[string]any)["offerId"])
	require.Zero(t, o.redemptions(offer, false), "a quote redeems nothing")

	// The undiscounted total is stale now.
	r = o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, 11000), idem(o.a))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "price_changed", problemCode(r))

	r = o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, 9900), idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.EqualValues(t, 9900, r.body["totalPaise"])
	require.Equal(t, 1, o.redemptions(offer, false))
	var redemptions int
	require.NoError(t, o.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT redemptions FROM offers WHERE id = $1`, uuidOf(t, offer)).Scan(&redemptions))
	require.Equal(t, 1, redemptions)

	r = o.me(o.cust, id, "cancel", map[string]any{"reason": "changed_mind"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 1, o.redemptions(offer, true), "cancel releases the redemption")
	require.Zero(t, o.redemptions(offer, false))
	require.NoError(t, o.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT redemptions FROM offers WHERE id = $1`, uuidOf(t, offer)).Scan(&redemptions))
	require.Zero(t, redemptions)
}

// A capped offer that runs out between two orders stops applying: the second
// customer's quote is full price and an order at the discounted total is
// price_changed, never a silent overcharge or free discount.
func TestOfferRunsOut(t *testing.T) {
	o := newOrderShop(t)
	one := 1
	offer := o.offerOn("percent", 10, &one)
	lines := []line{{o.butter, 2}}

	r := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart(lines, 9900), idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, 1, o.redemptions(offer, false))

	r = o.a.call(o.cust2, http.MethodPost, "/v1/me/checkout/quote", func() map[string]any {
		b := o.cart(lines, 0)
		delete(b, "expectedTotalPaise")
		return b
	}(), nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 11000, r.body["totalPaise"], "the cap is spent: full price")

	r = o.a.call(o.cust2, http.MethodPost, "/v1/me/orders", o.cart(lines, 9900), idem(o.a))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "price_changed", problemCode(r))
	require.Equal(t, 1, o.redemptions(offer, false), "nothing was redeemed twice")
}

// An offer past its end by the injected clock does not price, whatever its
// status label says (01 §12: expiry is by the clock, the tidy job only moves
// labels).
func TestOfferExpiredByClockDoesNotApply(t *testing.T) {
	o := newOrderShop(t)
	o.offerOn("percent", 10, nil)
	o.a.exec(`UPDATE offers SET ends_at = $1`, o.a.clk.Now().Add(30*time.Minute))
	r := o.quote(line{o.butter, 2})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 9900, r.body["totalPaise"])

	o.advance(time.Hour)
	r = o.quote(line{o.butter, 2})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 11000, r.body["totalPaise"], "ended by the clock, label still live")
}

// Basket search prices item-level offers through the same engine as checkout
// (search.Pricer adapter): the discount equals what the quote shows, and a
// bill-minimum offer is left to checkout.
func TestOfferBasketSearchAdapter(t *testing.T) {
	o := newOrderShop(t)
	o.offerOn("percent", 10, nil)
	bill := ids.New(o.a.clk.Now()).String()
	o.a.exec(`INSERT INTO offers (id, store_id, kind, value, min_bill_paise, scope, store_item_ids, channels, starts_at, status)
	  VALUES ($1, $2, 'bill_min', 500, 1000, 'store', '{}', 'both', $3, 'live')`,
		uuidOf(t, bill), uuidOf(t, o.sid), o.a.clk.Now().Add(-time.Hour))

	d := &deps{
		Config: o.cfg, Log: slog.New(slog.NewJSONHandler(io.Discard, nil)), Clock: o.a.clk,
		DB:       db.New(o.a.tdb.Pool),
		Settings: settings.NewCache(settings.PGLoader(o.a.tdb.Pool), settings.Options{}),
	}
	pricer := offersForSearch{newOffers(d)}
	storeID, err := ids.Parse(o.sid)
	require.NoError(t, err)
	butter, err := ids.Parse(o.butter)
	require.NoError(t, err)
	got, err := pricer.LineDiscounts(context.Background(), ids.ID{}, storeID,
		[]search.PriceLine{{StoreItemID: butter, UnitPricePaise: 5500, Quantity: 2}}, o.a.clk.Now())
	require.NoError(t, err)
	require.Equal(t, []int64{1100}, got, "10% of ₹110, no bill offer")
	got, err = pricer.LineDiscounts(context.Background(), ids.ID{}, storeID,
		[]search.PriceLine{{StoreItemID: butter, UnitPricePaise: 5500, Quantity: 2}}, o.a.clk.Now().Add(-2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, []int64{0}, got, "before the offer began")
}
