package search

import (
	"math"
	"sort"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// Ranking (01 §11): filter first, then
//
//	score = w_text·text + w_distance·exp(−d / 1.5 km) + w_reliability·reliability
//	      + w_rating·rating_norm + w_offer·has_offer
//
// with the weights from platform_settings (basis points / 10 000). Results
// are grouped by store: a store's text and offer terms are those of its best
// item ("the lead item"), so a store ranks by the best thing it has for the
// query. Badges are labels only and never change this order (D-101).

// distanceScaleM is the 1.5 km of exp(−d / 1.5 km) in the 01 §11 formula; it
// is part of the formula, not a setting.
const distanceScaleM = 1500.0

// ratingPrior is the store-rating prior of 01 §10 / §17 (a fixed constant,
// "4.0 with weight 5"): a store with no rating yet ranks as if rated 4.0, the
// Bayesian average of zero reviews. TODO(lead): 01 §11 does not say what
// rating_norm is for an unrated store; the prior is the reading that neither
// rewards nor punishes a new shop.
const ratingPrior = 4.0

// maxItemsPerStore is how many matching items a result card lists.
// TODO(lead): no number is given (the design shows one to three rows); the
// summary and the badges still use every matching item.
const maxItemsPerStore = 5

// Sorts the search accepts (04 §2.3).
const (
	sortRelevance = "relevance"
	sortNearest   = "nearest"
	sortCheapest  = "cheapest"
)

// weights are the five ranking weights as fractions.
type weights struct {
	text, distance, reliability, rating, offer float64
}

// candItem is one matching in-stock item of a store.
type candItem struct {
	ID             ids.ID
	ProductID      ids.ID
	Name           string // the store's local name, else the product name
	Variant        *string
	Brand          *string
	PackSize       float64
	Unit           string
	Price          int64
	Mrp            *int64
	StockUpdatedAt *time.Time
	ImageKey       *string
	Text           float64 // text match, 0–1
	Offer          bool
}

// itemScore orders a store's items (text and offer terms only: the store
// terms are the same for all of them).
func (it candItem) itemScore(w weights) float64 {
	s := w.text * it.Text
	if it.Offer {
		s += w.offer
	}
	return s
}

// candStore is a store with at least one matching item.
type candStore struct {
	ID          ids.ID
	Slug        string
	Name        string
	Locality    *string
	Tint        *string
	DistanceM   float64
	Open        bool
	Pickup      bool
	Delivery    bool // effective: the store delivers and delivery is on
	FeePaise    int64
	FreeAbove   *int64
	CashbackBP  int32
	Rating      *float64
	RatingCount int32
	Reliability *float64
	Items       []candItem // best first after rank
	Score       float64
}

// bookable: pickup or delivery (D-41); an In-store only store is not.
func (s *candStore) bookable() bool { return s.Pickup || s.Delivery }

// lead is the store's best item (after rank).
func (s *candStore) lead() candItem { return s.Items[0] }

// storeScore is the 01 §11 score of the store with its best item.
func storeScore(s *candStore, lead candItem, w weights) float64 {
	rel := 1.0 // a store with no reliability yet counts as 1.0 (01 §11)
	if s.Reliability != nil {
		rel = clamp01(*s.Reliability)
	}
	rating := ratingPrior
	if s.Rating != nil {
		rating = *s.Rating
	}
	return lead.itemScore(w) +
		w.distance*math.Exp(-s.DistanceM/distanceScaleM) +
		w.reliability*rel +
		w.rating*clamp01(rating/5)
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

// rank orders each store's items (score, then cheaper, then id), scores the
// stores and orders them by the sort: relevance (score, ties to the nearer
// store), nearest (distance, then score) or cheapest (the lead item's price,
// then distance). The store id breaks every remaining tie, so the order is
// stable across pages.
func rank(stores []*candStore, w weights, sortBy string) {
	for _, s := range stores {
		sort.SliceStable(s.Items, func(i, j int) bool {
			a, b := s.Items[i], s.Items[j]
			if sa, sb := a.itemScore(w), b.itemScore(w); sa != sb {
				return sa > sb
			}
			if a.Price != b.Price {
				return a.Price < b.Price
			}
			return a.ID.Compare(b.ID) < 0
		})
		s.Score = storeScore(s, s.lead(), w)
	}
	byDistance := func(a, b *candStore) (bool, bool) {
		if a.DistanceM != b.DistanceM {
			return a.DistanceM < b.DistanceM, true
		}
		return false, false
	}
	sort.SliceStable(stores, func(i, j int) bool {
		a, b := stores[i], stores[j]
		switch sortBy {
		case sortNearest:
			if less, ok := byDistance(a, b); ok {
				return less
			}
			if a.Score != b.Score {
				return a.Score > b.Score
			}
		case sortCheapest:
			if pa, pb := a.lead().Price, b.lead().Price; pa != pb {
				return pa < pb
			}
			if less, ok := byDistance(a, b); ok {
				return less
			}
		default:
			if a.Score != b.Score {
				return a.Score > b.Score
			}
			if less, ok := byDistance(a, b); ok {
				return less
			}
		}
		return a.ID.Compare(b.ID) < 0
	})
}

// modes are the Pickup and Delivery chips of the request.
type modes struct{ pickup, delivery bool }

// deliveryFee is the store's fee for one unit at price: 0 at or above its
// free-delivery threshold (01 §5.5, D-101).
func deliveryFee(s *candStore, price int64) int64 {
	if s.FreeAbove != nil && price >= *s.FreeAbove {
		return 0
	}
	return s.FeePaise
}

// total is what the lead item costs the customer in the chosen mode (D-101
// Lowest price): the price with the Pickup chip, price plus one unit's fee
// with the Delivery chip, and with no mode (or both, TODO(lead): D-101 does
// not say) the store's cheaper mode: the price when it accepts pickup, else
// price plus fee. ok is false for a store that cannot serve that mode, which
// then never gets Lowest price. Credits never count.
func total(s *candStore, m modes) (int64, bool) {
	price := s.lead().Price
	switch {
	case m.pickup && !m.delivery:
		return price, s.Pickup
	case m.delivery && !m.pickup:
		return price + deliveryFee(s, price), s.Delivery
	}
	switch {
	case s.Pickup:
		return price, true
	case s.Delivery:
		return price + deliveryFee(s, price), true
	}
	return 0, false
}

// showFee reports whether the card shows the delivery fee: the Delivery chip
// is on (and the store delivers) or the store only delivers (D-101).
func showFee(s *candStore, m modes) bool {
	return s.Delivery && (m.delivery || !s.Pickup)
}

// Badge names (DiscoverBadge).
// i32 narrows a count, limit or offset for a query parameter, clamping
// instead of wrapping (the contract already bounds every one of them).
func i32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

const (
	badgeLowestPrice = "lowest_price"
	badgeClosest     = "closest"
)

// badges labels the cards of the first page (D-101): Lowest price to the
// lowest total (ties to the nearer store; In-store only stores never), and
// Closest to the nearest card. A card shows at most one badge, Lowest price
// first; when one card wins both, Closest goes to no other card. Ready
// soonest is not shown on search results in v1. It returns the badge per
// index of page.
func badges(page []*candStore, m modes) map[int]string {
	out := map[int]string{}
	if len(page) == 0 {
		return out
	}
	nearer := func(a, b *candStore) bool {
		if a.DistanceM != b.DistanceM {
			return a.DistanceM < b.DistanceM
		}
		return a.ID.Compare(b.ID) < 0
	}
	cheapest, best := -1, int64(0)
	var cheapStore *candStore
	for i, s := range page {
		t, ok := total(s, m)
		if !ok || !s.bookable() {
			continue
		}
		if cheapStore == nil || t < best || (t == best && nearer(s, cheapStore)) {
			cheapest, best, cheapStore = i, t, s
		}
	}
	closest, nearest := 0, page[0]
	for i, s := range page {
		if nearer(s, nearest) {
			closest, nearest = i, s
		}
	}
	if cheapest >= 0 {
		out[cheapest] = badgeLowestPrice
	}
	if closest != cheapest {
		out[closest] = badgeClosest
	}
	return out
}

// summary is the D-101 line: M stores have it, from the lowest item price
// among all of them (every matching item, not only the listed ones). ok is
// false when no store has it (the line is hidden).
func summary(stores []*candStore) (withIt int, minPrice int64, ok bool) {
	for _, s := range stores {
		for _, it := range s.Items {
			if !ok || it.Price < minPrice {
				minPrice, ok = it.Price, true
			}
		}
	}
	return len(stores), minPrice, ok
}
