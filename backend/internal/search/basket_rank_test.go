package search

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// Basket ranking, badges and split (08 BSK-05…08). Store ids rise with n,
// so the id tie-break is predictable.

const missing = -1

// bstore builds a store at distance m whose line k costs totals[k] paise
// (missing: not covered).
func bstore(n int, m float64, totals ...int64) *bStore {
	st := &candStore{ID: ids.New(time.Unix(int64(n), 0)), Name: string(rune('A' + n)), DistanceM: m, Pickup: true}
	b := &bStore{st: st, picks: make([]*bPick, len(totals))}
	for k, t := range totals {
		if t != missing {
			b.picks[k] = &bPick{qty: 1, lineTotal: t}
		}
	}
	b.tally()
	return b
}

func names(stores []*bStore) []string {
	out := make([]string, len(stores))
	for i, b := range stores {
		out[i] = b.st.Name
	}
	return out
}

func TestBasketRankOrder(t *testing.T) {
	// BSK-05: F 4/4 ₹400 3 km; G 3/4 ₹100 0.5 km; H 4/4 ₹350 6 km; J 3/4 ₹90 4 km.
	f := bstore(5, 3000, 10000, 10000, 10000, 10000)
	g := bstore(6, 500, 4000, 3000, 3000, missing)
	h := bstore(7, 6000, 10000, 10000, 10000, 5000)
	j := bstore(9, 4000, 3000, 3000, missing, 3000)
	f.st.Name, g.st.Name, h.st.Name, j.st.Name = "F", "G", "H", "J"
	stores := []*bStore{f, g, h, j}
	rankBasket(stores)
	require.Equal(t, []string{"H", "F", "J", "G"}, names(stores), "08 BSK-05 read G, J; 01 §11.4 orders by total (D-111)")
}

func TestBasketRankTies(t *testing.T) {
	// BSK-06: same lines and total → the nearer first; then the id.
	far := bstore(1, 900, 100, 100)
	near := bstore(2, 300, 100, 100)
	twin := bstore(3, 300, 100, 100)
	stores := []*bStore{far, twin, near}
	rankBasket(stores)
	require.Equal(t, []*bStore{near, twin, far}, stores)

	// A full match with a higher total still ranks above a cheaper partial.
	full := bstore(4, 5000, 900, 900)
	part := bstore(5, 100, 10, missing)
	stores = []*bStore{part, full}
	rankBasket(stores)
	require.Equal(t, []*bStore{full, part}, stores)
}

func TestBasketBadges(t *testing.T) {
	t.Run("has everything, lowest price, closest on three cards", func(t *testing.T) {
		a := bstore(1, 3000, 300, 300) // full, ₹6
		b := bstore(2, 2000, 200, 200) // full, ₹4: first card
		c := bstore(3, 500, 250, 250)  // full, ₹5, closest
		page := []*bStore{a, b, c}
		rankBasket(page)
		require.Equal(t, []*bStore{b, c, a}, page)
		got := basketBadges(page, 2)
		// b is first and covers everything: Has everything; it also wins
		// Lowest price, which then goes to no card. c is closest.
		require.Equal(t, map[int]string{0: badgeHasEverything, 1: badgeClosest}, got)
	})
	t.Run("no store has everything", func(t *testing.T) {
		a := bstore(1, 3000, 100, missing, 100) // 2 of 3, ₹2
		b := bstore(2, 400, 150, 150, missing)  // 2 of 3, ₹3, closest
		c := bstore(3, 100, 10, missing, missing)
		page := []*bStore{a, b, c}
		rankBasket(page)
		require.Equal(t, []*bStore{a, b, c}, page)
		got := basketBadges(page, 3)
		require.Equal(t, map[int]string{0: badgeLowestPrice, 1: badgeClosest}, got,
			"Closest is decided among the stores with the most lines, so c gets none")
	})
	t.Run("one card wins both: it shows Lowest price, Closest goes nowhere", func(t *testing.T) {
		a := bstore(1, 100, 100, missing)
		b := bstore(2, 900, 200, missing)
		page := []*bStore{a, b}
		rankBasket(page)
		require.Equal(t, map[int]string{0: badgeLowestPrice}, basketBadges(page, 2))
	})
	t.Run("Lowest price only for a bookable store", func(t *testing.T) {
		a := bstore(1, 900, 100, missing)
		a.st.Pickup = false
		b := bstore(2, 100, 200, missing)
		page := []*bStore{a, b}
		rankBasket(page)
		require.Equal(t, map[int]string{1: badgeLowestPrice}, basketBadges(page, 2),
			"b wins both Lowest price (a is not bookable) and Closest; it shows Lowest price")
	})
	t.Run("badges never change the order", func(t *testing.T) {
		a := bstore(1, 3000, 300, 300)
		b := bstore(2, 2000, 200, 200)
		c := bstore(3, 500, 250, missing)
		page := []*bStore{c, a, b}
		rankBasket(page)
		before := names(page)
		_ = basketBadges(page, 2)
		require.Equal(t, before, names(page))
	})
	t.Run("an empty page has no badges", func(t *testing.T) {
		require.Empty(t, basketBadges(nil, 2))
	})
}

func lineIdx(p *bStore) []int {
	var out []int
	for k, pk := range p.picks {
		if pk != nil {
			out = append(out, k)
		}
	}
	return out
}

func TestBasketSplit(t *testing.T) {
	t.Run("the pair covering the most lines wins; each line to the cheaper store", func(t *testing.T) {
		a := bstore(1, 1000, 100, 100, missing, missing) // lines 0,1
		b := bstore(2, 1000, 90, missing, 100, missing)  // lines 0,2
		c := bstore(3, 1000, missing, missing, missing, 50)
		sp := bestSplit([]*bStore{a, b, c}, 4)
		require.NotNil(t, sp)
		// Every pair covers 3 lines: (a,b) = 0,1,2 at 90+100+100 = 290;
		// (a,c) = 0,1,3 at 250; (b,c) = 0,2,3 at 240 → b and c.
		require.ElementsMatch(t, []*candStore{b.st, c.st}, []*candStore{sp.parts[0].st, sp.parts[1].st})
		require.Equal(t, b.st, sp.parts[0].st, "the store with more lines first")
		require.Equal(t, []int{0, 2}, lineIdx(sp.parts[0]))
		require.Equal(t, []int{3}, lineIdx(sp.parts[1]))
		require.Equal(t, []int{1}, sp.uncovered)
		require.EqualValues(t, 190, sp.parts[0].total)
	})
	t.Run("more lines beat a lower total", func(t *testing.T) {
		a := bstore(1, 100, 10, missing, missing)
		b := bstore(2, 100, missing, 10, missing)
		c := bstore(3, 100, missing, 500, 500)
		sp := bestSplit([]*bStore{a, b, c}, 3)
		require.Equal(t, []*candStore{c.st, a.st}, []*candStore{sp.parts[0].st, sp.parts[1].st})
		require.Empty(t, sp.uncovered)
	})
	t.Run("a shared line goes to the cheaper store; a tie to the nearer", func(t *testing.T) {
		a := bstore(1, 2000, 100, 50, missing)
		b := bstore(2, 500, 100, 60, 70)
		sp := bestSplit([]*bStore{a, b}, 3)
		require.Equal(t, b.st, sp.parts[0].st)
		require.Equal(t, []int{0, 2}, lineIdx(sp.parts[0]), "line 0 ties at ₹1: the nearer b")
		require.Equal(t, []int{1}, lineIdx(sp.parts[1]), "line 1 is cheaper at a")
	})
	t.Run("ties: lower combined total, then lower combined distance", func(t *testing.T) {
		a := bstore(1, 100, 100, missing)
		b := bstore(2, 100, missing, 100)
		c := bstore(3, 900, missing, 100)
		sp := bestSplit([]*bStore{a, c, b}, 2)
		require.ElementsMatch(t, []*candStore{a.st, b.st}, []*candStore{sp.parts[0].st, sp.parts[1].st})

		d := bstore(4, 100, missing, 90)
		sp = bestSplit([]*bStore{a, b, c, d}, 2)
		require.ElementsMatch(t, []*candStore{a.st, d.st}, []*candStore{sp.parts[0].st, sp.parts[1].st})
	})
	t.Run("a pair where one store gets nothing is no split", func(t *testing.T) {
		a := bstore(1, 100, 10, 10)
		b := bstore(2, 100, 20, 20)
		require.Nil(t, bestSplit([]*bStore{a, b}, 2))
		require.Nil(t, bestSplit([]*bStore{a}, 2))
		require.Nil(t, bestSplit(nil, 2))
	})
}

func TestChoose(t *testing.T) {
	p1 := ids.New(time.Unix(1, 0))
	p2 := ids.New(time.Unix(2, 0))
	i1, i2, i3 := ids.New(time.Unix(11, 0)), ids.New(time.Unix(12, 0)), ids.New(time.Unix(13, 0))
	items := []candItem{
		{ID: i1, ProductID: p1, Price: 500, Available: 5, Text: 1},
		{ID: i2, ProductID: p1, Price: 400, Available: 4, Text: 0.8},
		{ID: i3, ProductID: p2, Price: 100, Available: 9, Text: textCategoryHit, Category: true},
	}
	// BSK-04: available 5 for 5 covers; 4 for 5 does not.
	it, ok := choose(items, bLine{productID: p1, qty: 5})
	require.True(t, ok)
	require.Equal(t, i1, it.ID)
	it, ok = choose(items, bLine{productID: p1, qty: 4})
	require.True(t, ok)
	require.Equal(t, i2, it.ID, "a product line takes the cheapest listing with enough stock")
	_, ok = choose(items, bLine{productID: p1, qty: 6})
	require.False(t, ok)

	// A text line: the best text match; a category-only hit never covers.
	it, ok = choose(items, bLine{norm: "amul butter", qty: 1})
	require.True(t, ok)
	require.Equal(t, i1, it.ID)
	_, ok = choose(items[2:], bLine{norm: "dairy", qty: 1})
	require.False(t, ok)
}
