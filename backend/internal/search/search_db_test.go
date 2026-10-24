package search

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

func radius(m discoverapi.SearchDiscoverParamsRadius) func(*discoverapi.SearchDiscoverParams) {
	return func(p *discoverapi.SearchDiscoverParams) { p.Radius = &m }
}

func sortBy(s discoverapi.SearchDiscoverParamsSort) func(*discoverapi.SearchDiscoverParams) {
	return func(p *discoverapi.SearchDiscoverParams) { p.Sort = &s }
}

func pickupOnly(p *discoverapi.SearchDiscoverParams)   { p.Pickup = bp(true) }
func deliveryOnly(p *discoverapi.SearchDiscoverParams) { p.Delivery = bp(true) }

func badgeOf(r discoverapi.DiscoverSearchResult, slug string) string {
	for _, s := range r.Results {
		if s.Store.StoreSlug == slug && s.Badge != nil {
			return string(*s.Badge)
		}
	}
	return ""
}

func card(t *testing.T, r discoverapi.DiscoverSearchResult, slug string) discoverapi.DiscoverSearchStore {
	t.Helper()
	for _, s := range r.Results {
		if s.Store.StoreSlug == slug {
			return s
		}
	}
	t.Fatalf("no result for %s in %v", slug, resultSlugs(r))
	return discoverapi.DiscoverSearchStore{}
}

// logRow is the latest search_log row.
type logRow struct {
	query          string
	results, store int
	product        *string
	user           *string
	source         string
}

func (w *world) logRows() []logRow {
	w.t.Helper()
	rows, err := w.tdb.OwnerPool.Query(w.ctx, `SELECT query_norm, results_count, store_count, product_id::text, user_id::text, source
	  FROM search_log ORDER BY created_at, id`)
	require.NoError(w.t, err)
	defer rows.Close()
	var out []logRow
	for rows.Next() {
		var r logRow
		require.NoError(w.t, rows.Scan(&r.query, &r.results, &r.store, &r.product, &r.user, &r.source))
		out = append(out, r)
	}
	require.NoError(w.t, rows.Err())
	return out
}

// eventsNamed returns the props of every event called name.
func (w *world) eventsNamed(name string) []map[string]any {
	w.t.Helper()
	rows, err := w.tdb.OwnerPool.Query(w.ctx, `SELECT props FROM events WHERE name = $1 ORDER BY occurred_at, id`, name)
	require.NoError(w.t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		require.NoError(w.t, rows.Scan(&raw))
		m := map[string]any{}
		require.NoError(w.t, json.Unmarshal(raw, &m))
		out = append(out, m)
	}
	require.NoError(w.t, rows.Err())
	return out
}

// ulidOf is the ULID text of a fixture uuid.
func ulidOf(t *testing.T, uuid string) string {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	require.NoError(t, err)
	require.Len(t, b, 16)
	return ids.ID(b).String()
}

// ASK-09: the summary line at the QA point.
func TestSearchSummaryASK09(t *testing.T) {
	f := newQA(t)
	r := f.search("Amul Butter 100 g")
	require.NotNil(t, r.Summary)
	assert.Equal(t, discoverapi.DiscoverSearchSummary{StoresChecked: 4, StoresWithIt: 1, MinPricePaise: 5800}, *r.Summary)
	assert.Equal(t, []string{"near"}, resultSlugs(r), "500 g and out-of-stock rows never show")

	// 5 km takes in the shop 3 km away.
	r = f.search("Amul Butter 100 g", radius(discoverapi.SearchDiscoverParamsRadiusN5000))
	require.NotNil(t, r.Summary)
	assert.Equal(t, discoverapi.DiscoverSearchSummary{StoresChecked: 5, StoresWithIt: 2, MinPricePaise: 4500}, *r.Summary)

	// No store has it: no summary, logged as zero results.
	r = f.search("paneer")
	assert.Nil(t, r.Summary)
	assert.Empty(t, r.Results)
	assert.Empty(t, r.Chips)
	assert.False(t, r.RefineHint)
	logs := f.logRows()
	require.Len(t, logs, 3)
	assert.Equal(t, logRow{query: "paneer", source: "typed"}, logs[2])
	zero := f.eventsNamed("search_zero_results")
	require.Len(t, zero, 1)
	assert.NotContains(t, zero[0], "query")
}

// SR-01: only live stores of verified businesses, anywhere.
func TestHiddenStoresNeverShowSR01(t *testing.T) {
	f := newQA(t)
	for _, q := range []string{"amul butter", "8901262150019", "butter"} {
		for _, s := range resultSlugs(f.search(q, radius(discoverapi.SearchDiscoverParamsRadiusN1000))) {
			assert.NotContains(t, s, "hidden", q)
			assert.NotEqual(t, "unverified", s, q)
		}
	}
	sum, err := f.svc.Summary(f.ctx, discoverapi.GetDiscoverSummaryParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	assert.Equal(t, 4, sum.StoresNearby)
	list, err := f.svc.ListStores(f.ctx, discoverapi.ListDiscoverStoresParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	var slugs []string
	for _, c := range list.Items {
		slugs = append(slugs, c.StoreSlug)
	}
	assert.Equal(t, []string{"near", "instore", "mid", "far"}, slugs)
	for _, slug := range []string{"hidden-paused", "hidden-draft", "hidden-suspended", "hidden-pending-review", "unverified", "nope"} {
		_, err := f.svc.GetStore(f.ctx, slug, discoverapi.GetDiscoverStoreParams{})
		require.Error(t, err, slug)
		assert.Equal(t, 404, problem.Status(err), slug)
		_, err = f.svc.ListStoreItems(f.ctx, slug, discoverapi.ListDiscoverStoreItemsParams{})
		assert.Equal(t, 404, problem.Status(err), slug)
	}
}

func TestMatching(t *testing.T) {
	f := newQA(t)
	assert.Equal(t, []string{"near", "mid"}, resultSlugs(f.search("amull buttr")), "typo")
	assert.Equal(t, []string{"near", "mid", "far"}, resultSlugs(f.search("dahi")), "synonym")
	assert.Equal(t, []string{"mid", "near", "far"}, resultSlugs(f.search("Nandini Dahi")), "the store's own name is an exact match")
	assert.Equal(t, []string{"near", "mid"}, resultSlugs(f.search("ಬೆಣ್ಣೆ")), "Kannada synonym")
	assert.Equal(t, []string{"near"}, resultSlugs(f.search("8901262150019")), "barcode")
	assert.ElementsMatch(t, []string{"near", "mid", "far", "instore"}, resultSlugs(f.search("Dairy and eggs")), "category name")
	assert.Equal(t, []string{"near", "far"}, resultSlugs(f.search("wheat flour")), "multi-word synonym")
	assert.Empty(t, f.search("'; DROP TABLE stores; --").Results)
	assert.Empty(t, f.search("!!!").Results)
	assert.Len(t, f.logRows(), 8, "the punctuation-only query is not logged")
}

func TestOutOfStockAndInStoreOnlySR02SR05(t *testing.T) {
	f := newQA(t)
	r := f.search("ghee")
	require.Equal(t, []string{"instore"}, resultSlugs(r))
	c := r.Results[0]
	assert.False(t, c.Store.Bookable, "In-store only")
	assert.Equal(t, "closest", badgeOf(r, "instore"), "never Lowest price")
	assert.Nil(t, c.DeliveryFeePaise)

	f.exec(`UPDATE store_items SET on_hand = 0 WHERE id = $1`, f.nearButter)
	assert.Empty(t, f.search("Amul Butter 100 g").Results)
}

func TestBadgesAndModesASK10(t *testing.T) {
	f := newQA(t)
	r := f.search("curd")
	assert.Equal(t, []string{"near", "mid", "far"}, resultSlugs(r))
	assert.Equal(t, "closest", badgeOf(r, "near"))
	assert.Equal(t, "lowest_price", badgeOf(r, "mid"))
	assert.Empty(t, badgeOf(r, "far"))
	assert.Nil(t, card(t, r, "mid").DeliveryFeePaise, "pickup is the cheaper mode: no fee shown")

	r = f.search("curd", deliveryOnly)
	require.Equal(t, []string{"mid"}, resultSlugs(r))
	require.NotNil(t, r.Results[0].DeliveryFeePaise)
	assert.Equal(t, 2000, *r.Results[0].DeliveryFeePaise)
	assert.Equal(t, "lowest_price", badgeOf(r, "mid"), "one card wins both: Lowest price only")

	r = f.search("curd", pickupOnly)
	assert.Equal(t, []string{"near", "mid", "far"}, resultSlugs(r))
	assert.Empty(t, f.search("ghee", pickupOnly).Results)

	// Equal totals go to the nearer store.
	f.exec(`UPDATE store_items SET price_paise = 3000 WHERE store_id = (SELECT id FROM stores WHERE slug = 'far') AND product_id = $1`, f.curd)
	f.exec(`UPDATE store_items SET price_paise = 3000 WHERE store_id = (SELECT id FROM stores WHERE slug = 'near') AND product_id = $1`, f.curd)
	r = f.search("curd")
	assert.Equal(t, "lowest_price", badgeOf(r, "near"))
	assert.Empty(t, badgeOf(r, "mid"))
	assert.Empty(t, badgeOf(r, "far"))
}

func TestConfirmedAtASK12(t *testing.T) {
	f := newQA(t)
	r := f.search("curd")
	near := card(t, r, "near").Items[0]
	require.NotNil(t, near.ConfirmedAt)
	assert.True(t, near.ConfirmedAt.Equal(testNow.Add(-time.Hour)))
	assert.Nil(t, card(t, r, "mid").Items[0].ConfirmedAt, "96 h > stock_stale_after_hours")
	assert.Equal(t, "Nandini Dahi", card(t, r, "mid").Items[0].Name, "the store's own name")

	// The window is a setting.
	f.exec(`UPDATE platform_settings SET value = '100' WHERE key = 'stock_stale_after_hours'`)
	f.cache.Invalidate()
	r = f.search("curd")
	assert.NotNil(t, card(t, r, "mid").Items[0].ConfirmedAt)
}

func TestSortsAndWeightsSR02SR04(t *testing.T) {
	f := newQA(t)
	assert.Equal(t, []string{"near", "mid", "far"}, resultSlugs(f.search("curd", sortBy(discoverapi.SearchDiscoverParamsSortNearest))))
	assert.Equal(t, []string{"mid", "far", "near"}, resultSlugs(f.search("curd", sortBy(discoverapi.SearchDiscoverParamsSortCheapest))))

	// near loses reliability and rating: by default mid outranks it...
	f.exec(`UPDATE stores SET reliability_score = 0, rating_avg = 1.0 WHERE slug = 'near'`)
	assert.Equal(t, []string{"mid", "near", "far"}, resultSlugs(f.search("curd")))
	// ...until the weights say only text and distance count.
	f.exec(`UPDATE platform_settings SET value = '0' WHERE key IN ('search_w_reliability', 'search_w_rating')`)
	f.cache.Invalidate()
	assert.Equal(t, []string{"near", "mid", "far"}, resultSlugs(f.search("curd")))
	// nearest stays nearest whatever the weights.
	assert.Equal(t, []string{"near", "mid", "far"}, resultSlugs(f.search("curd", sortBy(discoverapi.SearchDiscoverParamsSortNearest))))
}

func TestFilters(t *testing.T) {
	f := newQA(t)
	max := 3300
	assert.Equal(t, []string{"mid", "far"}, resultSlugs(f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.MaxPricePaise = &max })))
	assert.Equal(t, []string{"near", "mid", "far"}, resultSlugs(f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.Brand = sp("nandini") })))
	r := f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.Brand = sp("amul") })
	assert.Empty(t, r.Results)
	assert.Nil(t, r.Summary)
	assert.Equal(t, []string{"near", "far"}, resultSlugs(f.search("atta", func(p *discoverapi.SearchDiscoverParams) { p.Category = sp("staples") })))
	assert.Empty(t, f.search("atta", func(p *discoverapi.SearchDiscoverParams) { p.Category = sp("dairy-eggs") }).Results)
	// productId: only that master product's listings, counted in the summary.
	for _, tc := range []struct {
		name, q, product string
		want             []string
		withIt           int
	}{
		{"same name", "curd", f.curd, []string{"near", "mid", "far"}, 3},
		{"q about another product", "atta", f.curd, []string{"near", "mid", "far"}, 3},
		{"one pack size only", "amul butter", f.butter100, []string{"near"}, 1},
		{"no stock anywhere", "ghee", f.atta, []string{"near", "far"}, 2},
	} {
		r := f.search(tc.q, func(p *discoverapi.SearchDiscoverParams) { p.ProductId = sp(ulidOf(t, tc.product)) })
		assert.ElementsMatch(t, tc.want, resultSlugs(r), tc.name)
		for _, st := range r.Results {
			for _, it := range st.Items {
				assert.Equal(t, ulidOf(t, tc.product), it.ProductId, tc.name)
			}
		}
		require.NotNil(t, r.Summary, tc.name)
		assert.Equal(t, tc.withIt, r.Summary.StoresWithIt, tc.name)
	}
	assert.Empty(t, f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.ProductId = sp("01JB0S4A5B6C7D8E9F0G1H2J3K") }).Results, "an unknown product")
	before := len(f.logRows())
	assert.Empty(t, f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.Brand = sp("no-such-brand") }).Results)
	assert.Empty(t, f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.Category = sp("no-such-category") }).Results)
	assert.Len(t, f.logRows(), before, "an unknown brand or category is not a search")

	// Open now: a manual close drops the store.
	until := testNow.Add(time.Hour)
	f.exec(`UPDATE stores SET closed_until = $1 WHERE slug = 'far'`, until)
	r = f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.OpenNow = bp(true) })
	assert.Equal(t, []string{"near", "mid"}, resultSlugs(r))
	r = f.search("curd")
	assert.False(t, card(t, r, "far").Store.Open)
}

func TestPagination(t *testing.T) {
	f := newQA(t)
	lim := 2
	r := f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.Limit = &lim })
	assert.Equal(t, []string{"near", "mid"}, resultSlugs(r))
	require.NotNil(t, r.NextCursor)
	require.NotNil(t, r.Summary)
	assert.Equal(t, 3, r.Summary.StoresWithIt)
	r2 := f.search("curd", func(p *discoverapi.SearchDiscoverParams) { p.Limit = &lim; p.Cursor = r.NextCursor })
	assert.Equal(t, []string{"far"}, resultSlugs(r2))
	assert.Nil(t, r2.NextCursor)
	assert.Nil(t, r2.Results[0].Badge, "badges are for the first page")
	assert.Len(t, f.logRows(), 1, "a next page is the same search")

	bad := "garbage"
	_, err := f.svc.Search(f.ctx, discoverapi.SearchDiscoverParams{Q: "curd", Lat: qaLat, Lng: qaLng, Cursor: &bad})
	assert.Equal(t, 422, problem.Status(err))
}

// IN-28 and D-99: the product_id of a search.
func TestSearchLogProductIN28(t *testing.T) {
	f := newQA(t)
	f.exec(`INSERT INTO product_synonyms (id, term, synonym, lang, product_id) VALUES ($1, 'ghee', 'tuppa', 'kn', $2)`, f.id(), f.ghee)
	f.user = ids.New(testNow).String()
	voice := discoverapi.Voice
	cases := []struct {
		q    string
		want string
	}{
		{"8901262150019", f.butter100},     // barcode
		{"Amul Butter 100 g", f.butter100}, // name + pack
		{"amul butter 100gm", f.butter100}, // the same, normalised
		{"Nandini Curd", f.curd},           // name
		{"tuppa", f.ghee},                  // a synonym row that names the product
		{"Amul Butter", ""},                // two packs: ambiguous (TODO(lead) D-99)
		{"amull buttr", ""},                // typo
		{"amul", ""},                       // partial
		{"something for breakfast", ""},    // free text
		{"curd", ""},                       // a generic synonym term
	}
	for _, c := range cases {
		f.search(c.q, func(p *discoverapi.SearchDiscoverParams) { p.Source = &voice })
	}
	logs := f.logRows()
	perf := f.eventsNamed("search_performed")
	require.Len(t, logs, len(cases))
	require.Len(t, perf, len(cases))
	for i, c := range cases {
		if c.want == "" {
			assert.Nil(t, logs[i].product, c.q)
			assert.NotContains(t, perf[i], "product_id", c.q)
		} else {
			require.NotNil(t, logs[i].product, c.q)
			assert.Equal(t, c.want, *logs[i].product, c.q)
			assert.Equal(t, ulidOf(t, c.want), perf[i]["product_id"], c.q)
		}
		assert.Equal(t, "voice", logs[i].source, c.q)
		require.NotNil(t, logs[i].user, c.q)
		assert.Equal(t, "voice", perf[i]["source"], c.q)
		assert.Equal(t, "none", perf[i]["chips"], c.q)
		assert.NotContains(t, perf[i], "query", c.q)
		assert.NotContains(t, perf[i], "lat", c.q)
	}
	assert.Equal(t, "amul butter 100 g", logs[1].query)
}

// TestShopPageWithoutCategory: a business with no category (every dev seed shop)
// still has a shop page, with the category fields null (W2-13 B-1: was a 500).
func TestShopPageWithoutCategory(t *testing.T) {
	w := newWorld(t)
	biz := w.id()
	w.exec(`INSERT INTO businesses (id, brand_name, referral_code, status) VALUES ($1, 'Biz', $2, 'verified')`, biz, shortCode(w.n))
	w.store(biz, "plain", 300, 0, storeOpt{pickup: true})
	pg, err := w.svc.GetStore(w.ctx, "plain", discoverapi.GetDiscoverStoreParams{})
	require.NoError(t, err)
	assert.Equal(t, "plain", pg.StoreSlug)
	assert.Nil(t, pg.CategorySlug)
	assert.Nil(t, pg.CategoryName)
	assert.Nil(t, pg.CategoryTint)
	_, err = w.svc.ListStoreItems(w.ctx, "plain", discoverapi.ListDiscoverStoreItemsParams{})
	require.NoError(t, err)
}

func TestShopPage(t *testing.T) {
	f := newQA(t)
	lat, lng := qaLat, qaLng
	pg, err := f.svc.GetStore(f.ctx, "near", discoverapi.GetDiscoverStoreParams{Lat: &lat, Lng: &lng})
	require.NoError(t, err)
	assert.Equal(t, "near", pg.StoreSlug)
	// Checkout takes the store id, so the page, the list and search all carry it.
	assert.True(t, ids.Valid(pg.StoreId), pg.StoreId)
	list, err := f.svc.ListStores(f.ctx, discoverapi.ListDiscoverStoresParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	require.NotEmpty(t, list.Items)
	assert.Equal(t, "near", list.Items[0].StoreSlug)
	assert.Equal(t, pg.StoreId, list.Items[0].StoreId)
	found := f.search("amul butter")
	require.NotEmpty(t, found.Results)
	assert.Equal(t, "near", found.Results[0].Store.StoreSlug)
	assert.Equal(t, pg.StoreId, found.Results[0].Store.StoreId)
	require.NotNil(t, pg.CategorySlug)
	require.NotNil(t, pg.CategoryName)
	assert.Equal(t, "grocery", *pg.CategorySlug)
	assert.NotEmpty(t, *pg.CategoryName)
	require.NotNil(t, pg.DistanceM)
	assert.InDelta(t, 300, *pg.DistanceM, 5)
	// Half a point is no point: the page without a distance, not an error.
	half, err := f.svc.GetStore(f.ctx, "near", discoverapi.GetDiscoverStoreParams{Lng: &lng})
	require.NoError(t, err)
	assert.Nil(t, half.DistanceM)

	items, err := f.svc.ListStoreItems(f.ctx, "instore", discoverapi.ListDiscoverStoreItemsParams{})
	require.NoError(t, err)
	require.Len(t, items.Items, 2)
	assert.Equal(t, "Ghee", items.Items[0].Name)
	assert.True(t, items.Items[0].InStock)
	assert.Equal(t, "Amul Butter", items.Items[1].Name)
	assert.False(t, items.Items[1].InStock, "out of stock listed last")

	items, err = f.svc.ListStoreItems(f.ctx, "near", discoverapi.ListDiscoverStoreItemsParams{Q: sp("dahi")})
	require.NoError(t, err)
	require.Len(t, items.Items, 1)
	assert.Equal(t, "Nandini Curd", items.Items[0].Name)

	lim := 2
	items, err = f.svc.ListStoreItems(f.ctx, "near", discoverapi.ListDiscoverStoreItemsParams{Limit: &lim})
	require.NoError(t, err)
	assert.Len(t, items.Items, 2)
	require.NotNil(t, items.NextCursor)
	more, err := f.svc.ListStoreItems(f.ctx, "near", discoverapi.ListDiscoverStoreItemsParams{Limit: &lim, Cursor: items.NextCursor})
	require.NoError(t, err)
	assert.Len(t, more.Items, 1)
	assert.Nil(t, more.NextCursor)
}

func TestDiscoverSummaryAndList(t *testing.T) {
	f := newQA(t)
	sum, err := f.svc.Summary(f.ctx, discoverapi.GetDiscoverSummaryParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	assert.Equal(t, 4, sum.StoresNearby)
	assert.Equal(t, 4, sum.OpenNow)
	assert.Equal(t, 8, sum.ProductsInRadius, "near 3, mid 2, far 2, instore 1 (the 0-stock row is not counted)")
	assert.Len(t, sum.Pins, 4)
	assert.False(t, sum.PinsTruncated)

	until := testNow.Add(time.Hour)
	f.exec(`UPDATE stores SET closed_until = $1 WHERE slug = 'far'`, until)
	sum, err = f.svc.Summary(f.ctx, discoverapi.GetDiscoverSummaryParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	assert.Equal(t, 3, sum.OpenNow)

	slugsOf := func(p discoverapi.ListDiscoverStoresParams) []string {
		t.Helper()
		p.Lat, p.Lng = qaLat, qaLng
		l, err := f.svc.ListStores(f.ctx, p)
		require.NoError(t, err)
		var out []string
		for _, c := range l.Items {
			out = append(out, c.StoreSlug)
		}
		return out
	}
	assert.Equal(t, []string{"near", "instore", "mid"}, slugsOf(discoverapi.ListDiscoverStoresParams{OpenNow: bp(true)}))
	assert.Equal(t, []string{"near", "mid", "far"}, slugsOf(discoverapi.ListDiscoverStoresParams{Pickup: bp(true)}))
	assert.Equal(t, []string{"mid"}, slugsOf(discoverapi.ListDiscoverStoresParams{Delivery: bp(true)}))
	assert.Equal(t, []string{"near", "far"}, slugsOf(discoverapi.ListDiscoverStoresParams{Category: sp("staples")}))
	assert.Equal(t, []string{"near", "instore", "mid", "far"}, slugsOf(discoverapi.ListDiscoverStoresParams{Category: sp("grocery")}))
	assert.Empty(t, slugsOf(discoverapi.ListDiscoverStoresParams{Category: sp("no-such-category")}))

	// Delivery off platform-wide: no store delivers.
	f.exec(`UPDATE platform_settings SET value = 'false' WHERE key = 'feature_delivery_enabled'`)
	f.cache.Invalidate()
	assert.Empty(t, slugsOf(discoverapi.ListDiscoverStoresParams{Delivery: bp(true)}))
	assert.Empty(t, f.search("curd", deliveryOnly).Results)
}

func TestPlaces(t *testing.T) {
	w := newWorld(t)
	pin, err := w.svc.SearchPlaces(w.ctx, discoverapi.SearchPlacesParams{Q: "5600"})
	require.NoError(t, err)
	require.NotEmpty(t, pin.Items)
	for _, p := range pin.Items {
		require.NotNil(t, p.Pincode)
		assert.True(t, strings.HasPrefix(*p.Pincode, "5600"), *p.Pincode)
	}
	byName, err := w.svc.SearchPlaces(w.ctx, discoverapi.SearchPlacesParams{Q: "indira"})
	require.NoError(t, err)
	require.NotEmpty(t, byName.Items)
	assert.Equal(t, "Indiranagar", byName.Items[0].Name)
	assert.Equal(t, "Indiranagar, Bengaluru 560038", byName.Items[0].Label)

	typo, err := w.svc.SearchPlaces(w.ctx, discoverapi.SearchPlacesParams{Q: "indranagar"})
	require.NoError(t, err)
	var names []string
	for _, p := range typo.Items {
		names = append(names, p.Name)
	}
	assert.Contains(t, names, "Indiranagar")

	for _, q := range []string{"%", "_", `\`, "'"} {
		_, err := w.svc.SearchPlaces(w.ctx, discoverapi.SearchPlacesParams{Q: q})
		require.NoError(t, err, q)
	}

	rev, err := w.svc.ReversePlace(w.ctx, discoverapi.ReversePlaceParams{Lat: qaLat, Lng: qaLng})
	require.NoError(t, err)
	assert.Equal(t, "Indiranagar", rev.Name)
	_, err = w.svc.ReversePlace(w.ctx, discoverapi.ReversePlaceParams{Lat: 0, Lng: 0})
	assert.Equal(t, 404, problem.Status(err))
}

func TestSuggest(t *testing.T) {
	f := newQA(t)
	out, err := f.svc.Suggest(f.ctx, discoverapi.SearchSuggestParams{Q: "amu"})
	require.NoError(t, err)
	require.NotEmpty(t, out.Items)
	assert.Equal(t, discoverapi.DiscoverSuggestionKind("brand"), out.Items[0].Kind)
	assert.Equal(t, "Amul", out.Items[0].Text)
	var products int
	for _, it := range out.Items {
		if it.Kind == "product" {
			products++
		}
	}
	assert.Equal(t, 2, products, "both Amul Butter packs")

	out, err = f.svc.Suggest(f.ctx, discoverapi.SearchSuggestParams{Q: "dairy"})
	require.NoError(t, err)
	var cats []string
	for _, it := range out.Items {
		if it.Kind == "category" && it.CategorySlug != nil {
			cats = append(cats, *it.CategorySlug)
		}
	}
	assert.Contains(t, cats, "dairy-eggs")
	assert.Empty(t, f.logRows(), "a suggestion is not a search")
}
