package offers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	offersapi "github.com/todayz/backend/gen/api/offers"
	"github.com/todayz/backend/internal/platform/ids"
)

// SC-OF: item offer, bill offer and rounding in paise, priced from the database.
func TestPriceFromDatabase(t *testing.T) {
	w := newWorld(t, nil)
	store := w.store("a", 100)
	rice := w.item(store, "Rice", 20000)
	dal := w.item(store, "Dal", 3333)
	pct := w.offer(store, offerRow{kind: "percent", value: 10, scope: "items", items: []ids.ID{rice}})
	w.offer(store, offerRow{kind: "flat", value: 1000, scope: "items", items: []ids.ID{rice}}) // loses to 10% of 2 x 200
	bill := w.offer(store, offerRow{kind: "bill_min", value: 5000, minBill: i64(30000)})
	// Not live: paused, ended, scheduled in the future, POS-only.
	w.offer(store, offerRow{kind: "percent", value: 90, status: "paused"})
	w.offer(store, offerRow{kind: "percent", value: 90, startsAt: testNow.Add(-2 * time.Hour), endsAt: tp(testNow.Add(-time.Hour))})
	w.offer(store, offerRow{kind: "percent", value: 90, status: "scheduled", startsAt: testNow.Add(3 * time.Hour)})
	w.offer(store, offerRow{kind: "percent", value: 90, channels: "pos"})

	q, err := w.svc.Price(w.ctx, Cart{StoreID: store, Channel: ChannelOnline, Lines: []Line{line(rice, 20000, 2), line(dal, 3333, 1)}})
	require.NoError(t, err)
	require.EqualValues(t, 4000, q.Lines[0].DiscountPaise)
	require.Equal(t, pct, q.Lines[0].OfferID)
	require.EqualValues(t, 0, q.Lines[1].DiscountPaise)
	require.EqualValues(t, 43333, q.GrossPaise)
	require.EqualValues(t, 39333, q.ItemSubtotalPaise)
	require.EqualValues(t, 5000, q.BillDiscountPaise)
	require.Equal(t, bill, q.BillOffer.OfferID)
	require.EqualValues(t, 34333, q.TotalPaise)
	require.Len(t, q.Applied, 2)

	// The offer ends by the injected clock, with no job having run.
	w.offer(store, offerRow{kind: "percent", value: 50, scope: "items", items: []ids.ID{dal}, endsAt: tp(testNow.Add(30 * time.Minute))})
	q, err = w.svc.Price(w.ctx, Cart{StoreID: store, Lines: []Line{line(dal, 3333, 1)}})
	require.NoError(t, err)
	require.EqualValues(t, 1666, q.Lines[0].DiscountPaise, "floor of half of 33.33")
	w.clk.Advance(time.Hour)
	q, err = w.svc.Price(w.ctx, Cart{StoreID: store, Lines: []Line{line(dal, 3333, 1)}})
	require.NoError(t, err)
	require.EqualValues(t, 0, q.Lines[0].DiscountPaise)
}

// SC-OF: Price at a given time reads that time (basket search prices "as of").
func TestPriceAtTime(t *testing.T) {
	w := newWorld(t, nil)
	store := w.store("a", 100)
	it := w.item(store, "Rice", 10000)
	w.offer(store, offerRow{kind: "percent", value: 20, startsAt: testNow.Add(time.Hour), status: "scheduled"})
	q, _ := w.svc.Price(w.ctx, Cart{StoreID: store, Lines: []Line{line(it, 10000, 1)}})
	require.EqualValues(t, 0, q.TotalPaise-q.ItemSubtotalPaise+q.ItemDiscountPaise)
	require.EqualValues(t, 0, q.ItemDiscountPaise)
	q, err := w.svc.Price(w.ctx, Cart{StoreID: store, At: testNow.Add(2 * time.Hour), Lines: []Line{line(it, 10000, 1)}})
	require.NoError(t, err)
	require.EqualValues(t, 2000, q.ItemDiscountPaise)
}

func TestPricePerCustomerLimit(t *testing.T) {
	w := newWorld(t, nil)
	store := w.store("a", 100)
	it := w.item(store, "Rice", 10000)
	off := w.offer(store, offerRow{kind: "percent", value: 10, perCustomer: ip(1)})
	cust := w.customer()
	cart := Cart{StoreID: store, CustomerID: cust, Lines: []Line{line(it, 10000, 1)}}
	q, err := w.svc.Price(w.ctx, cart)
	require.NoError(t, err)
	require.EqualValues(t, 1000, q.ItemDiscountPaise)
	require.NoError(t, w.svc.Redeem(w.ctx, Ref{OrderID: w.id()}, cust, q))
	q, err = w.svc.Price(w.ctx, cart)
	require.NoError(t, err)
	require.EqualValues(t, 0, q.ItemDiscountPaise, "used up for this customer")
	// Another customer still gets it; nobody-known does not.
	cart.CustomerID = w.customer()
	q, _ = w.svc.Price(w.ctx, cart)
	require.EqualValues(t, 1000, q.ItemDiscountPaise)
	cart.CustomerID = ids.Zero
	q, _ = w.svc.Price(w.ctx, cart)
	require.EqualValues(t, 0, q.ItemDiscountPaise, "a per-customer limit needs a known customer")
	_ = off
}

// SC-OF: redemption caps, idempotent per ref, and given back on release.
func TestRedeemCapAndRelease(t *testing.T) {
	w := newWorld(t, nil)
	store := w.store("a", 100)
	it := w.item(store, "Rice", 10000)
	off := w.offer(store, offerRow{kind: "percent", value: 10, maxRedeem: ip(1)})
	cust := w.customer()
	q, err := w.svc.Price(w.ctx, Cart{StoreID: store, CustomerID: cust, Lines: []Line{line(it, 10000, 1)}})
	require.NoError(t, err)
	require.Len(t, q.Applied, 1)

	o1, o2 := w.id(), w.id()
	require.NoError(t, w.svc.Redeem(w.ctx, Ref{OrderID: o1}, cust, q))
	require.NoError(t, w.svc.Redeem(w.ctx, Ref{OrderID: o1}, cust, q), "redeeming a ref twice is a no-op")
	require.ErrorIs(t, w.svc.Redeem(w.ctx, Ref{OrderID: o2}, cust, q), ErrUnavailable)
	require.Equal(t, 1, w.redemptions(off))

	// Spent offers stop pricing.
	q2, _ := w.svc.Price(w.ctx, Cart{StoreID: store, CustomerID: cust, Lines: []Line{line(it, 10000, 1)}})
	require.EqualValues(t, 0, q2.ItemDiscountPaise)

	require.NoError(t, w.svc.Release(w.ctx, Ref{OrderID: o1}))
	require.NoError(t, w.svc.Release(w.ctx, Ref{OrderID: o1}), "releasing twice is a no-op")
	require.NoError(t, w.svc.Release(w.ctx, Ref{OrderID: o2}), "a ref that redeemed nothing is a no-op")
	require.Equal(t, 0, w.redemptions(off))
	require.NoError(t, w.svc.Redeem(w.ctx, Ref{OrderID: o2}, cust, q))
	require.Equal(t, 1, w.redemptions(off))

	require.Error(t, w.svc.Redeem(w.ctx, Ref{}, cust, q), "exactly one of order and POS sale")
	require.Error(t, w.svc.Redeem(w.ctx, Ref{OrderID: o1, POSSaleID: o2}, cust, q))
}

func (w *world) redemptions(off ids.ID) int {
	var n int
	require.NoError(w.t, w.tdb.OwnerPool.QueryRow(w.ctx, `SELECT redemptions FROM offers WHERE id = $1`, ids.UUID(off)).Scan(&n))
	return n
}

// SC-OF: the cap holds under concurrent orders; the per-customer limit too.
func TestRedeemConcurrent(t *testing.T) {
	w := newWorld(t, nil)
	store := w.store("a", 100)
	it := w.item(store, "Rice", 10000)
	off := w.offer(store, offerRow{kind: "percent", value: 10, maxRedeem: ip(5)})
	lim := w.offer(store, offerRow{kind: "flat", value: 100, scope: "items", items: []ids.ID{it}, perCustomer: ip(2)})
	cust := w.customer()
	q, err := w.svc.Price(w.ctx, Cart{StoreID: store, CustomerID: cust, Lines: []Line{line(it, 10000, 1)}})
	require.NoError(t, err)
	require.NotEmpty(t, q.Applied)

	var ok, gone atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := w.svc.db.InTx(w.ctx, func(ctx2 context.Context) error { return w.svc.Redeem(ctx2, Ref{OrderID: w.idSafe()}, cust, q) })
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrUnavailable):
				gone.Add(1)
			default:
				t.Errorf("redeem: %v", err)
			}
		}()
	}
	wg.Wait()
	// Either offer may be the one applied to the line; the winner's cap decides.
	require.Equal(t, int32(20), ok.Load()+gone.Load())
	require.LessOrEqual(t, ok.Load(), int32(5))
	require.GreaterOrEqual(t, ok.Load(), int32(2))
	for _, id := range []ids.ID{off, lim} {
		require.LessOrEqual(t, w.redemptions(id), 5)
	}
}

// SC-OF: the scheduling job only moves labels, and a second run changes nothing.
func TestTidy(t *testing.T) {
	w := newWorld(t, nil)
	store := w.store("a", 100)
	start := w.offer(store, offerRow{kind: "percent", value: 10, status: "scheduled", startsAt: testNow.Add(-time.Minute)})
	later := w.offer(store, offerRow{kind: "percent", value: 10, status: "scheduled", startsAt: testNow.Add(3 * time.Hour)})
	over := w.offer(store, offerRow{kind: "percent", value: 10, startsAt: testNow.Add(-2 * time.Hour), endsAt: tp(testNow.Add(-time.Minute))})
	spent := w.offer(store, offerRow{kind: "percent", value: 10, maxRedeem: ip(2), redemptions: 2})
	open := w.offer(store, offerRow{kind: "percent", value: 10})
	paused := w.offer(store, offerRow{kind: "percent", value: 10, status: "paused", startsAt: testNow.Add(-2 * time.Hour), endsAt: tp(testNow.Add(-time.Minute))})

	res, err := Tidy(w.ctx, w.svc.db, w.clk)
	require.NoError(t, err)
	require.Equal(t, TidyResult{Started: 1, Ended: 1, Spent: 1}, res)
	status := func(id ids.ID) string {
		var s string
		require.NoError(t, w.tdb.OwnerPool.QueryRow(w.ctx, `SELECT status FROM offers WHERE id = $1`, ids.UUID(id)).Scan(&s))
		return s
	}
	require.Equal(t, "live", status(start))
	require.Equal(t, "scheduled", status(later))
	require.Equal(t, "ended", status(over))
	require.Equal(t, "ended", status(spent))
	require.Equal(t, "live", status(open))
	require.Equal(t, "paused", status(paused))
	res, err = Tidy(w.ctx, w.svc.db, w.clk)
	require.NoError(t, err)
	require.Equal(t, TidyResult{}, res)
}

// SC-OF: the has-offer flags the search pages show.
func TestItemAndStoreOffers(t *testing.T) {
	w := newWorld(t, nil)
	a, b, c := w.store("a", 100), w.store("b", 200), w.store("c", 300)
	ia, ia2, ib := w.item(a, "Rice", 100), w.item(a, "Dal", 100), w.item(b, "Rice", 100)
	w.offer(a, offerRow{kind: "percent", value: 10, scope: "items", items: []ids.ID{ia}})
	w.offer(b, offerRow{kind: "bill_min", value: 100, minBill: i64(500)})
	w.offer(c, offerRow{kind: "percent", value: 10, channels: "pos"})

	items, err := w.svc.ItemOffers(w.ctx, []ids.ID{ia, ia2, ib}, testNow)
	require.NoError(t, err)
	require.Equal(t, map[ids.ID]bool{ia: true}, items, "a bill offer is the shop's, not the item's")
	stores, err := w.svc.StoreOffers(w.ctx, []ids.ID{a, b, c}, testNow)
	require.NoError(t, err)
	require.Equal(t, map[ids.ID]bool{a: true, b: true}, stores, "a POS-only offer does not show online")
	items, err = w.svc.ItemOffers(w.ctx, []ids.ID{ia}, testNow.Add(-48*time.Hour))
	require.NoError(t, err)
	require.Empty(t, items, "before the offer began")
}

// SC-OF: create, then only the allowed fields change; terms never do.
func TestCreateUpdatePreview(t *testing.T) {
	w := newWorld(t, nil)
	store := w.store("a", 100)
	it := w.item(store, "Rice", 20000)
	sid := store.String()

	val := 10
	created, ver, err := w.svc.createOffer(w.ctx, sid, &offersapi.OfferCreate{Kind: "percent", Scope: "store", Value: &val})
	require.NoError(t, err)
	require.Equal(t, offersapi.OfferStatusLive, created.Status)
	require.EqualValues(t, 1, ver)

	// Invalid terms are field errors, not 500s.
	bad := 95
	_, _, err = w.svc.createOffer(w.ctx, sid, &offersapi.OfferCreate{Kind: "percent", Scope: "store", Value: &bad})
	require.Error(t, err)
	_, _, err = w.svc.createOffer(w.ctx, sid, &offersapi.OfferCreate{Kind: "flat", Scope: "items", Value: &val})
	require.Error(t, err, "items scope needs items")

	// Pause needs a matching If-Match.
	paused := offersapi.OfferUpdateStatus("paused")
	_, _, err = w.svc.updateOffer(w.ctx, sid, created.Id, nil, &offersapi.OfferUpdate{Status: &paused}, func(k string) bool { return k == "status" })
	require.Error(t, err, "If-Match required")
	stale := `"v9"`
	_, _, err = w.svc.updateOffer(w.ctx, sid, created.Id, &stale, &offersapi.OfferUpdate{Status: &paused}, func(k string) bool { return k == "status" })
	require.Error(t, err)
	v1 := `"v1"`
	up, ver, err := w.svc.updateOffer(w.ctx, sid, created.Id, &v1, &offersapi.OfferUpdate{Status: &paused}, func(k string) bool { return k == "status" })
	require.NoError(t, err)
	require.Equal(t, offersapi.OfferStatusPaused, up.Status)
	require.EqualValues(t, 2, ver)

	// A paused offer does not price; resume brings it back; ended is final.
	q, _ := w.svc.Price(w.ctx, Cart{StoreID: store, Lines: []Line{line(it, 20000, 1)}})
	require.EqualValues(t, 0, q.ItemDiscountPaise)
	live := offersapi.OfferUpdateStatus("live")
	v2 := `"v2"`
	_, ver, err = w.svc.updateOffer(w.ctx, sid, created.Id, &v2, &offersapi.OfferUpdate{Status: &live}, func(k string) bool { return k == "status" })
	require.NoError(t, err)
	q, _ = w.svc.Price(w.ctx, Cart{StoreID: store, Lines: []Line{line(it, 20000, 1)}})
	require.EqualValues(t, 2000, q.ItemDiscountPaise)
	ended := offersapi.OfferUpdateStatus("ended")
	v3 := `"v3"`
	_, ver, err = w.svc.updateOffer(w.ctx, sid, created.Id, &v3, &offersapi.OfferUpdate{Status: &ended}, func(k string) bool { return k == "status" })
	require.NoError(t, err)
	v4 := `"v4"`
	_, _, err = w.svc.updateOffer(w.ctx, sid, created.Id, &v4, &offersapi.OfferUpdate{Status: &live}, func(k string) bool { return k == "status" })
	require.Error(t, err, "an ended offer cannot change")
	_ = ver

	// Another shop's offer is a 404 here.
	other := w.store("b", 200)
	_, _, err = w.svc.getOffer(w.ctx, other.String(), created.Id)
	require.Error(t, err)

	list, err := w.svc.listOffers(w.ctx, sid, offersapi.ListOffersParams{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
}

// SC-OF: the feed shows nearest first, one card per shop, banner over offer.
func TestFeed(t *testing.T) {
	src := &fakeBanners{}
	w := newWorld(t, src)
	a, b, c, d := w.store("a", 100), w.store("b", 300), w.store("c", 500), w.store("d", 700)
	w.offer(a, offerRow{kind: "percent", value: 10})
	w.offer(b, offerRow{kind: "flat", value: 500})
	w.offer(c, offerRow{kind: "percent", value: 20, channels: "pos"}) // not online: no card
	w.offer(d, offerRow{kind: "bxgy", buy: i64(1), get: i64(1)})
	bn := w.id()
	src.cards = []BannerCard{{ID: bn, StoreID: d, Headline: "Diwali sale", TargetType: "store"}}

	params := offersapi.ListNearbyOffersParams{Lat: 12.9719, Lng: 77.6412}
	feed, err := w.svc.listNearby(w.ctx, params)
	require.NoError(t, err)
	require.Len(t, feed.Items, 3)
	kinds := map[string]offersapi.NearbyOfferKind{}
	for _, it := range feed.Items {
		kinds[it.StoreSlug] = it.Kind
	}
	require.Equal(t, offersapi.NearbyOfferKindBanner, kinds["d"], "a banner replaces the offer card")
	require.Equal(t, offersapi.NearbyOfferKindOffer, kinds["a"])
	require.NotContains(t, kinds, "c")

	// Fair rotation: across the day every shop in range leads in turn.
	lead := map[string]bool{}
	for i := range 3 {
		w.clk.Set(testNow.Add(time.Duration(i) * feedRotation))
		f, err := w.svc.listNearby(w.ctx, params)
		require.NoError(t, err)
		require.Len(t, f.Items, 3)
		lead[f.Items[0].StoreSlug] = true
	}
	require.Len(t, lead, 3)

	// Outside the radius there is nothing.
	far, err := w.svc.listNearby(w.ctx, offersapi.ListNearbyOffersParams{Lat: 13.2, Lng: 77.9})
	require.NoError(t, err)
	require.Empty(t, far.Items)
}
