package search

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"  Amul   Butter 100gm ":   "amul butter 100 g",
		"AMUL BUTTER, 100 Grams":   "amul butter 100 g",
		"Ａｍｕｌ ｂｕｔｔｅｒ":              "amul butter",    // NFKC folds full-width letters
		"atta 1,5kg":               "atta 1.5 kg",    // a comma between digits is a decimal point
		"atta 1.5 KILO":            "atta 1.5 kg",    // unit aliases after a number
		"milk 1 ltr":               "milk 1 l",       // ltr -> l
		"g":                        "g",              // a unit alone is a word
		"दही १०० ग्राम":            "दही 100 ग्राम",  // Devanagari digits become ASCII
		"ಬೆಣ್ಣೆ ೫೦೦":               "ಬೆಣ್ಣೆ 500",     // Kannada marks stay, Kannada digits become ASCII
		"tea-bags (25 pcs)!!":      "tea bags 25 pc", // punctuation is a space
		"!!! ???":                  "",               // nothing searchable
		"'; DROP TABLE x; --":      "drop table x",   // quotes and operators never survive
		"coca-cola 2l & pepsi|7up": "coca cola 2 l pepsi 7up",
	}
	for in, want := range cases {
		require.Equal(t, want, Normalize(in), in)
	}
	long := ""
	for range 120 {
		long += "abc "
	}
	got := Normalize(long)
	require.LessOrEqual(t, len([]rune(got)), maxQueryRunes)
	require.NotContains(t, got[len(got)-1:], " ")
	require.Equal(t, "abc", got[len(got)-3:], "the cut falls on a word boundary")
}

func TestParsePack(t *testing.T) {
	p := parse("amul butter 100 g")
	require.Equal(t, []string{"amul", "butter"}, p.words)
	require.NotNil(t, p.pack)
	require.True(t, p.pack.matches(100, "g"))
	require.True(t, p.pack.matches(0.1, "kg"), "100 g is 0.1 kg")
	require.False(t, p.pack.matches(500, "g"))
	require.False(t, p.pack.matches(100, "ml"), "grams never match millilitres")

	p = parse("1 kg aashirvaad atta")
	require.Equal(t, []string{"aashirvaad", "atta"}, p.words)
	require.True(t, p.pack.matches(1000, "g"))

	p = parse("500 g") // only a pack size: keep it as words
	require.Nil(t, p.pack)
	require.Equal(t, []string{"500", "g"}, p.words)

	p = parse("7up 2 l")
	require.Equal(t, "7up", p.core())
	require.True(t, p.pack.matches(2000, "ml"))

	require.Nil(t, parse("butter 0 g").pack, "a zero pack is not a pack")
	require.Equal(t, "100 g", packText(100, "g"))
	require.Equal(t, "0.5 kg", packText(0.5, "kg"))
}

func TestTsquery(t *testing.T) {
	require.Equal(t, "'butter':*", tsToken("butter"))
	require.Equal(t, "('eggs':* | 'egg':*)", tsToken("eggs"), "a Latin plural also matches its singular")
	require.Equal(t, "'glass':*", tsToken("glass"), "ss is not a plural")
	require.Equal(t, "'bus':*", tsToken("bus"), "3 letters: never stripped")

	alts := map[string][]string{"wheat flour": {"atta", "gehun ka atta"}, "amul": {}}
	slots := buildSlots([]string{"amul", "wheat", "flour", "fresh"}, alts)
	require.Len(t, slots, 3)
	require.False(t, slots[0].expanded, "a phrase with no alternatives is not an expansion")
	require.True(t, slots[1].expanded)
	require.Equal(t,
		"'amul':* & (('wheat':* & 'flour':*) | 'atta':* | ('gehun':* & 'ka':* & 'atta':*)) & 'fresh':*",
		tsquery(slots))

	require.Equal(t, []string{"a", "a b", "a b c", "b", "b c", "c"}, ngrams([]string{"a", "b", "c"}))
	require.Equal(t, "'amul' & 'butter'", tsExact([]string{"amul", "butter"}))
	require.Equal(t, `50\% off\_x`, escapeLike("50% off_x"))
}

func TestCursor(t *testing.T) {
	c := encodeCursor(cursorSearch, 40)
	off, err := decodeCursor(cursorSearch, c)
	require.NoError(t, err)
	require.Equal(t, 40, off)

	off, err = decodeCursor(cursorSearch, nil)
	require.NoError(t, err)
	require.Zero(t, off)

	_, err = decodeCursor(cursorStores, c)
	require.Error(t, err, "a search cursor is not a store-list cursor")
	for _, bad := range []string{"!!", encB64("s:0"), encB64("s:-1"), encB64("s:01"), encB64("s:x"), encB64("s:99999999")} {
		_, err = decodeCursor(cursorSearch, &bad)
		require.Error(t, err, bad)
	}
	n := 0
	require.Equal(t, 20, pageLimit(nil, 20))
	require.Equal(t, 20, pageLimit(&n, 20))
}

func encB64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestConfirmedAt(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	stale := 72 * time.Hour
	fresh := now.Add(-12 * time.Minute)
	require.Equal(t, fresh, *confirmedAt(&fresh, now, stale), "ASK-12: 12 minutes ago is confirmed")
	edge := now.Add(-stale)
	require.NotNil(t, confirmedAt(&edge, now, stale), "exactly at the window is still confirmed")
	old := now.Add(-stale - time.Second)
	require.Nil(t, confirmedAt(&old, now, stale), "ASK-12: older than stock_stale_after_hours has no confirmedAt")
	require.Nil(t, confirmedAt(nil, now, stale), "never confirmed")
}

func TestHoursOut(t *testing.T) {
	now := time.Date(2026, 10, 23, 20, 0, 0, 0, time.UTC) // 24 Oct 01:30 IST
	raw := []byte(`{"weekly":{"mon":[{"open":"09:00","close":"21:00"}]},
	  "holidays":[{"date":"2026-12-25","ranges":[]},{"date":"2026-10-23","ranges":[]},{"date":"2026-10-24","ranges":[{"open":"10:00","close":"12:00"}]}]}`)
	h := hoursOut(raw, now)
	require.Len(t, h.Weekly, 7)
	require.Len(t, h.Weekly["mon"], 1)
	require.Empty(t, h.Weekly["sun"])
	require.NotNil(t, h.Weekly["sun"], "a closed day is an empty list, not null")
	require.Len(t, h.Holidays, 2, "a holiday before today (IST) is not shown")
	require.Equal(t, "2026-10-24", h.Holidays[0].Date.Format(time.DateOnly))
	require.Equal(t, "2026-12-25", h.Holidays[1].Date.Format(time.DateOnly))
	require.Len(t, hoursOut([]byte(`not json`), now).Weekly, 7)
}

// Ranking golden test (01 §11) with the seeded weights 4500/3000/1000/1000/500.
var defaultWeights = weights{text: 0.45, distance: 0.30, reliability: 0.10, rating: 0.10, offer: 0.05}

func fid(n byte) ids.ID {
	var id ids.ID
	id[0], id[15] = 0x01, n
	return id
}

func fp(v float64) *float64 { return &v }

func goldenStores() []*candStore {
	item := func(n byte, text float64, price int64, offer bool) candItem {
		return candItem{ID: fid(100 + n), ProductID: fid(200 + n), Name: "item", Unit: "g", PackSize: 100, Price: price, Text: text, Offer: offer}
	}
	return []*candStore{
		// B: near, partial text, top rating, half reliability.
		{ID: fid(2), Slug: "b", DistanceM: 200, Pickup: true, Rating: fp(5), Reliability: fp(0.5),
			Items: []candItem{item(2, 0.8, 5800, false)}},
		// C: far, exact text with an offer.
		{ID: fid(3), Slug: "c", DistanceM: 1500, Pickup: true, Rating: fp(4.5), Reliability: fp(0.9),
			Items: []candItem{item(3, 1, 5900, true)}},
		// D: the same as A in every term: the id breaks the tie.
		{ID: fid(4), Slug: "d", DistanceM: 500, Pickup: true,
			Items: []candItem{item(4, 1, 5800, false)}},
		// A: exact text, no rating (prior 4.0) and no reliability (1.0); its
		// second item is the better-priced but weaker match.
		{ID: fid(1), Slug: "a", DistanceM: 500, Pickup: true,
			Items: []candItem{item(5, 0.7, 5000, false), item(1, 1, 6000, false)}},
	}
}

func slugs(ss []*candStore) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Slug
	}
	return out
}

func TestRankGolden(t *testing.T) {
	ss := goldenStores()
	rank(ss, defaultWeights, sortRelevance)
	require.Equal(t, []string{"a", "d", "c", "b"}, slugs(ss))
	scores := map[string]float64{
		"a": 0.45 + 0.30*0.7165313106 + 0.10 + 0.08, // 0.8449594
		"d": 0.45 + 0.30*0.7165313106 + 0.10 + 0.08,
		"c": 0.45 + 0.05 + 0.30*0.3678794412 + 0.09 + 0.09, // 0.7903638
		"b": 0.36 + 0.30*0.8751733190 + 0.05 + 0.10,        // 0.7725520
	}
	for _, s := range ss {
		require.InDelta(t, scores[s.Slug], s.Score, 1e-6, s.Slug)
	}
	require.Equal(t, int64(6000), ss[0].lead().Price, "a store's lead item is its best match, not its cheapest")

	ss = goldenStores()
	rank(ss, defaultWeights, sortNearest)
	require.Equal(t, []string{"b", "a", "d", "c"}, slugs(ss), "SR-04: strictly by distance; equal distance by score, then id")

	ss = goldenStores()
	rank(ss, defaultWeights, sortCheapest)
	require.Equal(t, []string{"b", "d", "c", "a"}, slugs(ss), "the lead item's price, then the nearer store")

	// SR-02: a weight change changes the order. With distance dominant the
	// nearest store wins.
	ss = goldenStores()
	rank(ss, weights{text: 0.05, distance: 0.90, reliability: 0.0, rating: 0.0, offer: 0.05}, sortRelevance)
	require.Equal(t, "b", ss[0].Slug)

	// A missing reliability ranks as 1.0, the same as a perfect one.
	x := &candStore{ID: fid(9), DistanceM: 100, Items: []candItem{{Text: 1}}}
	y := &candStore{ID: fid(8), DistanceM: 100, Reliability: fp(1), Items: []candItem{{Text: 1}}}
	require.InDelta(t, storeScore(x, x.lead(), defaultWeights), storeScore(y, y.lead(), defaultWeights), 1e-12)
}

func TestBadges(t *testing.T) {
	free := int64(10000)
	st := func(n byte, dist float64, price int64, pickup, delivery bool, fee int64) *candStore {
		return &candStore{ID: fid(n), Slug: string('a' + rune(n)), DistanceM: dist, Pickup: pickup, Delivery: delivery,
			FeePaise: fee, FreeAbove: &free, Items: []candItem{{ID: fid(100 + n), Price: price, Text: 1}}}
	}
	none := modes{}

	// A card that wins both shows Lowest price; Closest goes to no other card.
	page := []*candStore{st(1, 300, 5000, true, false, 0), st(2, 100, 4000, true, false, 0), st(3, 50, 6000, true, false, 0)}
	page[2].DistanceM = 900
	got := badges(page, none)
	require.Equal(t, map[int]string{1: badgeLowestPrice}, got)

	// Equal totals: the nearer store.
	page = []*candStore{st(1, 300, 5000, true, false, 0), st(2, 200, 5000, true, false, 0), st(3, 100, 9000, true, false, 0)}
	require.Equal(t, map[int]string{1: badgeLowestPrice, 2: badgeClosest}, badges(page, none))

	// An in-store-only store never gets Lowest price, even when cheapest.
	page = []*candStore{st(1, 100, 1000, false, false, 0), st(2, 500, 5000, true, false, 0)}
	require.Equal(t, map[int]string{0: badgeClosest, 1: badgeLowestPrice}, badges(page, none))

	// Delivery chip: the total includes one unit's fee (0 at or above the
	// free-delivery threshold).
	page = []*candStore{
		st(1, 100, 9000, true, true, 2000),  // 9000 + 2000 = 11000
		st(2, 200, 10000, true, true, 2000), // free at 10000: 10000
		st(3, 50, 1000, true, false, 0),     // pickup only: not a delivery result
	}
	got = badges(page, modes{delivery: true})
	require.Equal(t, badgeLowestPrice, got[1])
	require.Equal(t, badgeClosest, got[2])
	require.True(t, showFee(page[0], modes{delivery: true}))
	require.False(t, showFee(page[0], none), "a store with pickup shows no fee without the Delivery chip")
	require.Equal(t, int64(0), deliveryFee(page[1], 10000))
	require.Equal(t, int64(2000), deliveryFee(page[1], 9999))

	// No mode: a delivery-only store counts price plus fee.
	page = []*candStore{st(1, 100, 4000, false, true, 1500), st(2, 900, 5000, true, false, 0)}
	got = badges(page, none)
	require.Equal(t, badgeLowestPrice, got[1], "4000+1500 > 5000")
	require.True(t, showFee(page[0], none), "a delivery-only store always shows its fee")

	// Pickup chip: the price alone, and a delivery-only store cannot win.
	page = []*candStore{st(1, 100, 1000, false, true, 0), st(2, 900, 5000, true, false, 0)}
	require.Equal(t, badgeLowestPrice, badges(page, modes{pickup: true})[1])

	// At most one badge per card, each badge once; and badges never reorder.
	ss := goldenStores()
	rank(ss, defaultWeights, sortRelevance)
	before := slugs(ss)
	marks := badges(ss, none)
	require.Equal(t, before, slugs(ss))
	seen := map[string]int{}
	for _, b := range marks {
		seen[b]++
	}
	for b, n := range seen {
		require.Equal(t, 1, n, b)
	}
	require.Empty(t, badges(nil, none))
}

func TestSummary(t *testing.T) {
	_, _, ok := summary(nil)
	require.False(t, ok, "no store has it: no summary line")
	n, minPrice, ok := summary(goldenStores())
	require.True(t, ok)
	require.Equal(t, 4, n)
	require.Equal(t, int64(5000), minPrice, "every matching item counts, not only the lead")
}
