package search

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Basket search on the ASK-09 fixture (08 BSK-03…05, 08, 09, 14).

func pline(t *testing.T, product string, qty int) discoverapi.BasketSearchLine {
	id := ulidOf(t, product)
	return discoverapi.BasketSearchLine{ProductId: &id, Quantity: qty}
}

func tline(text string, qty int) discoverapi.BasketSearchLine {
	return discoverapi.BasketSearchLine{Text: &text, Quantity: qty}
}

func (w *world) basket(lines ...discoverapi.BasketSearchLine) discoverapi.BasketSearchResult {
	w.t.Helper()
	out, err := w.svc.SearchBasket(w.ctx, discoverapi.BasketSearchRequest{Lat: qaLat, Lng: qaLng, Lines: lines})
	require.NoError(w.t, err)
	return out
}

func basketSlugs(r discoverapi.BasketSearchResult) []string {
	out := make([]string, len(r.Results))
	for i, s := range r.Results {
		out[i] = s.StoreSlug
	}
	return out
}

func TestBasketCoverageBSK04(t *testing.T) {
	f := newQA(t)
	// near has butter ×10, curd ×5, atta ×3; mid curd ×8; far atta ×2, curd ×2.
	r := f.basket(pline(t, f.butter100, 1), pline(t, f.curd, 5), pline(t, f.atta, 3))
	require.Equal(t, []string{"near", "mid"}, basketSlugs(r), "far has 2 of each: never enough")
	near := r.Results[0]
	assert.Equal(t, 3, near.CoveredCount)
	assert.Equal(t, 3, near.LineCount)
	assert.Equal(t, 5800+5*3500+3*25000, near.TotalPaise)
	assert.Empty(t, near.Missing)
	require.NotNil(t, near.Badge)
	assert.Equal(t, discoverapi.BasketBadge("has_everything"), *near.Badge)
	assert.True(t, near.Bookable)
	assert.Equal(t, 300, near.DistanceM)
	require.Len(t, near.Covered, 3)
	assert.Equal(t, 1, near.Covered[1].LineIndex)
	assert.Equal(t, 5, near.Covered[1].Quantity)
	assert.Equal(t, 5*3500, near.Covered[1].LineTotalPaise)
	assert.Equal(t, ulidOf(t, f.nearButter), near.Covered[0].StoreItemId)
	assert.NotNil(t, near.Covered[0].ConfirmedAt)

	mid := r.Results[1]
	assert.Equal(t, 1, mid.CoveredCount)
	assert.Equal(t, []discoverapi.BasketMissingLine{{LineIndex: 0}, {LineIndex: 2}}, mid.Missing)
	assert.Nil(t, mid.Badge)
	assert.Nil(t, mid.Covered[0].ConfirmedAt, "stale: older than 72 h")

	assert.Equal(t, discoverapi.BasketSearchSummary{StoresChecked: 4, StoresWithAll: 1}, r.Summary,
		"hidden, unverified and outside stores never count")
	assert.Nil(t, r.Split, "a store has everything: no split (Q-59)")

	// Available 5 for 5 covers; 4 for 5 does not (on hand less reserved).
	f.exec(`UPDATE store_items SET reserved = 1 WHERE store_id = (SELECT id FROM stores WHERE slug = 'near') AND product_id = $1`, f.curd)
	r = f.basket(pline(t, f.butter100, 1), pline(t, f.curd, 5), pline(t, f.atta, 3))
	require.Equal(t, []string{"near", "mid"}, basketSlugs(r))
	assert.Equal(t, 2, r.Results[0].CoveredCount)
	assert.Equal(t, 0, r.Summary.StoresWithAll)

	// 5 km takes in the store outside 2 km.
	m := discoverapi.BasketSearchRequestRadius(5000)
	out, err := f.svc.SearchBasket(f.ctx, discoverapi.BasketSearchRequest{Lat: qaLat, Lng: qaLng, Radius: &m,
		Lines: []discoverapi.BasketSearchLine{pline(t, f.butter100, 1)}})
	require.NoError(t, err)
	assert.Equal(t, []string{"outside", "near"}, basketSlugs(out), "both 1 of 1: the lower total first")
}

func TestBasketSplitBSK08BSK09(t *testing.T) {
	f := newQA(t)
	// Curd ×6: only mid has 8. near covers butter and atta.
	lines := []discoverapi.BasketSearchLine{pline(t, f.butter100, 1), pline(t, f.curd, 6), pline(t, f.atta, 1), tline("paneer", 1)}
	r := f.basket(lines...)
	assert.Equal(t, []string{"near", "mid", "far"}, basketSlugs(r), "2 of 4, then 1 of 4 at ₹180 before ₹240")
	assert.Equal(t, 0, r.Summary.StoresWithAll)
	require.NotNil(t, r.Split)
	require.Len(t, r.Split.Stores, 2)
	a, b := r.Split.Stores[0], r.Split.Stores[1]
	assert.Equal(t, "near", a.StoreSlug)
	assert.Equal(t, []int{0, 2}, a.LineIndexes)
	assert.Equal(t, 5800+25000, a.TotalPaise)
	assert.Equal(t, "mid", b.StoreSlug)
	assert.Equal(t, []int{1}, b.LineIndexes)
	assert.Equal(t, 6*3000, b.TotalPaise)
	assert.Equal(t, []int{3}, r.Split.Uncovered, "no store has paneer")

	// basket_split_max_shops = 1 turns splits off; partial matches still rank.
	f.exec(`UPDATE platform_settings SET value = '1' WHERE key = 'basket_split_max_shops'`)
	f.cache.Invalidate()
	r = f.basket(lines...)
	assert.Nil(t, r.Split)
	assert.Equal(t, []string{"near", "mid", "far"}, basketSlugs(r))

	// basket_results_max caps the stores shown, not the summary.
	f.exec(`UPDATE platform_settings SET value = '3' WHERE key = 'basket_results_max'`)
	f.cache.Invalidate()
	r = f.basket(append(lines, tline("ghee", 1))...)
	assert.Equal(t, []string{"near", "mid", "far"}, basketSlugs(r), "instore (ghee only) is fourth: not shown")
	assert.Equal(t, 4, r.Summary.StoresChecked)
}

func TestBasketFreeTextBSK03(t *testing.T) {
	f := newQA(t)
	r := f.basket(tline("amull buttr", 1), tline("paneer", 2))
	require.Equal(t, []string{"near", "mid"}, basketSlugs(r), "typo tolerance as in search")
	near := r.Results[0]
	assert.Equal(t, ulidOf(t, f.nearButter), near.Covered[0].StoreItemId)
	assert.Equal(t, []discoverapi.BasketMissingLine{{LineIndex: 1}}, near.Missing)
	assert.Nil(t, r.Split, "mid would get no line: the cheaper near takes the butter")

	// A category-only hit never covers a line.
	r = f.basket(tline("Dairy and eggs", 1))
	assert.Empty(t, r.Results)
}

func TestBasketLogsAndValidationBSK14(t *testing.T) {
	f := newQA(t)
	f.user = ""
	f.basket(pline(t, f.butter100, 1), tline("Amull  Buttr!", 1), tline("paneer", 1))
	logs := f.logRows()
	require.Len(t, logs, 3)
	prod := f.butter100
	assert.Equal(t, logRow{query: "amul butter", results: 1, store: 1, product: &prod, source: "basket"}, logs[0])
	assert.Equal(t, "amull buttr", logs[1].query)
	assert.Equal(t, "basket", logs[1].source)
	assert.Equal(t, logRow{query: "paneer", source: "basket"}, logs[2])
	var n int
	require.NoError(t, f.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM stock_reservations) + (SELECT count(*) FROM orders)`).Scan(&n))
	assert.Zero(t, n, "read-only")

	bad := func(path string, lines ...discoverapi.BasketSearchLine) {
		t.Helper()
		_, err := f.svc.SearchBasket(f.ctx, discoverapi.BasketSearchRequest{Lat: qaLat, Lng: qaLng, Lines: lines})
		require.Error(t, err)
		assert.Equal(t, 422, problem.Status(err), path)
		var e *problem.Error
		require.ErrorAs(t, err, &e)
		assert.Contains(t, e.Fields, path)
	}
	id := ulidOf(t, f.curd)
	bad("body.lines[0]", discoverapi.BasketSearchLine{ProductId: &id, Text: sp("curd"), Quantity: 1})
	bad("body.lines[1]", tline("curd", 1), discoverapi.BasketSearchLine{Quantity: 1})
	bad("body.lines[0].text", tline("!!!", 1))
	bad("body.lines[0].productId", discoverapi.BasketSearchLine{ProductId: sp("nope"), Quantity: 1})
	many := make([]discoverapi.BasketSearchLine, 31)
	for i := range many {
		many[i] = tline("curd "+strings.Repeat("x", i%3), 1)
	}
	bad("body.lines", many...)
	f.exec(`UPDATE platform_settings SET value = '5' WHERE key = 'basket_max_items'`)
	f.cache.Invalidate()
	bad("body.lines", many[:6]...)
	f.basket(many[:5]...)

	// A product id that names no product finds nothing and logs nothing.
	before := len(f.logRows())
	ghost := ids.New(time.Now()).String()
	r := f.basket(discoverapi.BasketSearchLine{ProductId: &ghost, Quantity: 1})
	assert.Empty(t, r.Results)
	assert.Len(t, f.logRows(), before)
}
