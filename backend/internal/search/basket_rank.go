package search

import (
	"slices"

	"github.com/todayz/backend/internal/platform/ids"
)

// Basket search, the pure part (W2-14; D-110, 01 §11.4): which item covers a
// line at a store, the order of the stores, the badges and the two-store
// split. basket.go gathers the candidates and prices them; nothing here
// touches the database, so basket_rank_test.go pins every rule.

// bLine is one basket line: a master product, or normalised free text.
type bLine struct {
	productID ids.ID // zero for free text
	norm      string
	qty       int
}

// bPick is the item that covers a line at a store, priced for the line.
type bPick struct {
	item      candItem
	qty       int
	lineTotal int64 // price × quantity less the item's line discount
}

// bStore is one store with its pick per line (nil: the line is missing).
type bStore struct {
	st      *candStore // the store; its Items are not used
	picks   []*bPick
	covered int
	total   int64 // the covered lines' total
}

// tally recounts covered and total from the picks.
func (b *bStore) tally() {
	b.covered, b.total = 0, 0
	for _, p := range b.picks {
		if p != nil {
			b.covered++
			b.total += p.lineTotal
		}
	}
}

// choose is the item that covers line l among a store's matching items:
// only items with enough available stock count; a product line takes the
// cheapest listing of that product, a text line the best text match (a
// category-only hit does not count), then the lower price, then the id.
func choose(items []candItem, l bLine) (candItem, bool) {
	var best candItem
	found := false
	for _, it := range items {
		if int(it.Available) < l.qty {
			continue
		}
		if l.productID != ids.Zero {
			if it.ProductID != l.productID {
				continue
			}
		} else if it.Category {
			continue
		}
		if !found || better(it, best, l.productID == ids.Zero) {
			best, found = it, true
		}
	}
	return best, found
}

func better(a, b candItem, byText bool) bool {
	if byText && a.Text != b.Text {
		return a.Text > b.Text
	}
	if a.Price != b.Price {
		return a.Price < b.Price
	}
	return a.ID.Compare(b.ID) < 0
}

// nearerStore orders by distance, then id (the stable tie-break).
func nearerStore(a, b *candStore) bool {
	if a.DistanceM != b.DistanceM {
		return a.DistanceM < b.DistanceM
	}
	return a.ID.Compare(b.ID) < 0
}

// rankBasket orders the stores (01 §11.4): more lines covered first (so
// every store that covers the whole basket comes first), then the lower
// total, then the nearer store, then the store id.
func rankBasket(stores []*bStore) {
	slices.SortStableFunc(stores, func(a, b *bStore) int {
		switch {
		case a.covered != b.covered:
			return b.covered - a.covered
		case a.total != b.total:
			if a.total < b.total {
				return -1
			}
			return 1
		case nearerStore(a.st, b.st):
			return -1
		case nearerStore(b.st, a.st):
			return 1
		}
		return 0
	})
}

const badgeHasEverything = "has_everything"

// basketBadges labels the shown cards (labels only, never a change in
// order; D-101's rules). Has everything goes to the first card when it
// covers every line. Lowest price (bookable stores only) and Closest are
// decided among the cards with the most lines covered, ties to the nearer
// store. A card shows at most one badge, in that order; a badge whose winner
// already shows one goes to no other card.
func basketBadges(page []*bStore, lines int) map[int]string {
	out := map[int]string{}
	if len(page) == 0 {
		return out
	}
	if page[0].covered == lines {
		out[0] = badgeHasEverything
	}
	top := page[0].covered
	cheapest, closest := -1, -1
	var cheap, near *bStore
	for i, b := range page {
		if b.covered != top {
			continue
		}
		if b.st.bookable() && (cheap == nil || b.total < cheap.total ||
			(b.total == cheap.total && nearerStore(b.st, cheap.st))) {
			cheapest, cheap = i, b
		}
		if near == nil || nearerStore(b.st, near.st) {
			closest, near = i, b
		}
	}
	if _, taken := out[cheapest]; cheapest >= 0 && !taken {
		out[cheapest] = badgeLowestPrice
	}
	if _, taken := out[closest]; closest >= 0 && !taken {
		out[closest] = badgeClosest
	}
	return out
}

// bSplit is a suggested two-store split: each part's picks hold only the
// lines that store gets.
type bSplit struct {
	parts     [2]*bStore
	uncovered []int
}

// bestSplit picks the pair of stores for a basket no store covers whole
// (01 §11.4): the pair that covers the most lines, then the lower combined
// total, then the lower combined distance, then the store ids. Each line
// goes to the cheaper of the two stores that cover it (ties to the nearer
// store). Both stores must get at least one line, or the pair is no split.
// The first part is the store with more lines, then the nearer one.
func bestSplit(stores []*bStore, lines int) *bSplit {
	type cand struct {
		a, b            *bStore
		covered         int
		total           int64
		distance        float64
		toA, toB, uncov []int
	}
	var best *cand
	betterCand := func(c, d *cand) bool {
		switch {
		case c.covered != d.covered:
			return c.covered > d.covered
		case c.total != d.total:
			return c.total < d.total
		case c.distance != d.distance:
			return c.distance < d.distance
		}
		ca, cb := orderedIDs(c.a, c.b)
		da, db := orderedIDs(d.a, d.b)
		if x := ca.Compare(da); x != 0 {
			return x < 0
		}
		return cb.Compare(db) < 0
	}
	for i, a := range stores {
		for _, b := range stores[i+1:] {
			c := cand{a: a, b: b, distance: a.st.DistanceM + b.st.DistanceM}
			for k := range lines {
				pa, pb := a.picks[k], b.picks[k]
				switch {
				case pa == nil && pb == nil:
					c.uncov = append(c.uncov, k)
					continue
				case pb == nil || (pa != nil && (pa.lineTotal < pb.lineTotal ||
					(pa.lineTotal == pb.lineTotal && nearerStore(a.st, b.st)))):
					c.toA = append(c.toA, k)
					c.total += pa.lineTotal
				default:
					c.toB = append(c.toB, k)
					c.total += pb.lineTotal
				}
				c.covered++
			}
			if len(c.toA) == 0 || len(c.toB) == 0 {
				continue
			}
			if best == nil || betterCand(&c, best) {
				cc := c
				best = &cc
			}
		}
	}
	if best == nil {
		return nil
	}
	pa, pb := part(best.a, best.toA, lines), part(best.b, best.toB, lines)
	if pb.covered > pa.covered || (pb.covered == pa.covered && nearerStore(pb.st, pa.st)) {
		pa, pb = pb, pa
	}
	return &bSplit{parts: [2]*bStore{pa, pb}, uncovered: best.uncov}
}

// part is store b with only the given lines' picks.
func part(b *bStore, lineIdx []int, lines int) *bStore {
	p := &bStore{st: b.st, picks: make([]*bPick, lines)}
	for _, k := range lineIdx {
		p.picks[k] = b.picks[k]
	}
	p.tally()
	return p
}

func orderedIDs(a, b *bStore) (ids.ID, ids.ID) {
	if a.st.ID.Compare(b.st.ID) <= 0 {
		return a.st.ID, b.st.ID
	}
	return b.st.ID, a.st.ID
}
