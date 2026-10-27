package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// D-270: an online counter sale never takes more than is available (on hand
// less what accepted online orders hold), summed over the bill's lines of an
// item; nothing is written when it would.
func TestPOSOnlineSaleCappedAtAvailable(t *testing.T) {
	p := newPosShop(t)
	before, _ := p.stock(p.butter)
	r := p.sync(p.manager, p.stockEntry(1, "damage", p.butter, before-3))
	require.Equal(t, []string{"applied"}, statuses(r), r.raw)
	p.accepted() // holds 1 butter for an online order
	onHand, reserved := p.stock(p.butter)
	require.Equal(t, 3, onHand)
	require.Equal(t, 1, reserved)
	sales := func() int { return p.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1`, uuidOf(t, p.sid)) }

	over := p.sell(p.cashier, p.saleBody(p.newID(), line{p.butter, 3}))
	require.Equal(t, http.StatusConflict, over.code, over.raw)
	require.Equal(t, "insufficient_stock", problemCode(over))
	require.Equal(t, p.butter, ext(over, "storeItemId"))
	require.EqualValues(t, 2, ext(over, "available"))

	// Two lines of one item count together; available is the item's before the sale.
	split := p.sell(p.cashier, p.saleBody(p.newID(), line{p.butter, 1}, line{p.salt, 1}, line{p.butter, 2}))
	require.Equal(t, http.StatusConflict, split.code, split.raw)
	require.Equal(t, p.butter, ext(split, "storeItemId"))
	require.EqualValues(t, 2, ext(split, "available"))

	require.Equal(t, 0, sales(), "a refused sale writes nothing")
	onHand, _ = p.stock(p.butter)
	require.Equal(t, 3, onHand)

	ok := p.sell(p.cashier, p.saleBody(p.newID(), line{p.butter, 2}))
	require.Equal(t, http.StatusOK, ok.code, ok.raw)
	onHand, reserved = p.stock(p.butter)
	require.Equal(t, 1, onHand)
	require.Equal(t, 1, reserved, "the online order's unit is still there")
	last := p.sell(p.cashier, p.saleBody(p.newID(), line{p.butter, 1}))
	require.Equal(t, http.StatusConflict, last.code, last.raw)
	require.EqualValues(t, 0, ext(last, "available"))
}

// D-270: a synced offline sale is never refused for stock (it already
// happened); the race leaves the stored on hand below 0, and every read shows
// 0 with countNeeded until a count fixes it.
func TestPOSOfflineRaceShowsZero(t *testing.T) {
	p := newPosShop(t)
	before, _ := p.stock(p.atta)
	r := p.sync(p.manager, p.stockEntry(1, "damage", p.atta, before-1))
	require.Equal(t, []string{"applied"}, statuses(r), r.raw)

	at := p.a.clk.Now().Add(-10 * time.Minute)
	first := p.sync(p.cashier, p.saleEntry(1, at, nil, line{p.atta, 1}))
	require.Equal(t, []string{"applied"}, statuses(first), first.raw)
	second := p.sync(p.manager, p.saleEntry(2, at.Add(time.Minute), nil, line{p.atta, 1}))
	require.Equal(t, []string{"applied"}, statuses(second), second.raw)
	onHand, _ := p.stock(p.atta)
	require.Equal(t, -1, onHand, "both real sales are kept")

	item := p.a.call(p.cashier, http.MethodGet, p.S+"/items/"+p.atta, nil, nil)
	require.Equal(t, http.StatusOK, item.code, item.raw)
	require.EqualValues(t, 0, item.body["onHand"])
	require.EqualValues(t, 0, item.body["available"])
	require.Equal(t, true, item.body["countNeeded"])

	count := p.sync(p.cashier, p.stockEntry(3, "count", p.atta, 4))
	require.Equal(t, []string{"applied"}, statuses(count), count.raw)
	onHand, _ = p.stock(p.atta)
	require.Equal(t, 4, onHand)
	item = p.a.call(p.cashier, http.MethodGet, p.S+"/items/"+p.atta, nil, nil)
	require.EqualValues(t, 4, item.body["onHand"])
	require.Equal(t, false, item.body["countNeeded"])
}

// D-270: synced damage and expired are refused above on hand (they were not
// capped before), and allowed up to it.
func TestPOSSyncDamageCappedAtOnHand(t *testing.T) {
	p := newPosShop(t)
	onHand, _ := p.stock(p.salt)
	r := p.sync(p.manager,
		p.stockEntry(1, "damage", p.salt, onHand+1),
		p.stockEntry(2, "expired", p.salt, onHand+1),
	)
	require.Equal(t, []string{"rejected:invalid", "rejected:invalid"}, statuses(r), r.raw)
	after, _ := p.stock(p.salt)
	require.Equal(t, onHand, after)

	r = p.sync(p.manager, p.stockEntry(3, "expired", p.salt, onHand-1), p.stockEntry(4, "damage", p.salt, 1))
	require.Equal(t, []string{"applied", "applied"}, statuses(r), r.raw)
	after, _ = p.stock(p.salt)
	require.Equal(t, 0, after)
}
