package search

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Basket search, the I/O part (W2-14; D-110, 01 §11.4,
// POST /v1/discover/basket-search). It is read-only apart from the search
// log, public, and never calls a model. Each line is searched under the
// same filter as search (in stock, a live store of a verified business,
// inside the radius): a product line by its listings, a text line through
// find (the normalised words, synonyms, typo tolerance, pack size). The
// covering item per line and store, the order, the badges and the split are
// basket_rank.go. Totals take item-level offers through the Pricer; credits
// never count. Every line is logged as one search with source "basket", so
// basket searches feed demand like any search (D-99).

// PriceLine is one covered line as the offers engine sees it.
type PriceLine struct {
	StoreItemID, ProductID ids.ID
	UnitPricePaise         int64
	Quantity               int
}

// Pricer takes item-level offers off basket lines (offers.Price, W2-07):
// one discount per line, in the order given. customerID is zero for a
// signed-out caller. A discount is clamped to its line.
type Pricer interface {
	LineDiscounts(ctx context.Context, customerID, storeID ids.ID, lines []PriceLine, t time.Time) ([]int64, error)
}

// NoLineOffers is the Pricer until offers land: nothing off.
type NoLineOffers struct{}

// LineDiscounts implements Pricer.
func (NoLineOffers) LineDiscounts(_ context.Context, _, _ ids.ID, lines []PriceLine, _ time.Time) ([]int64, error) {
	return make([]int64, len(lines)), nil
}

const sourceBasket = "basket"

// SearchBasket answers POST /v1/discover/basket-search.
func (s *Service) SearchBasket(ctx context.Context, in discoverapi.BasketSearchRequest) (discoverapi.BasketSearchResult, error) {
	start := s.now()
	out := discoverapi.BasketSearchResult{Results: []discoverapi.BasketSearchStore{}, ServerNow: start.UTC()}
	q, err := s.ready(ctx)
	if err != nil {
		return out, err
	}
	maxItems, err := s.settings.Int(ctx, settings.BasketMaxItems)
	if err != nil {
		return out, internal(err)
	}
	if int64(len(in.Lines)) > maxItems {
		return out, fieldProblem("body.lines", "too many")
	}
	lines := make([]bLine, len(in.Lines))
	for i, l := range in.Lines {
		path := "body.lines[" + strconv.Itoa(i) + "]"
		if (l.ProductId == nil) == (l.Text == nil) {
			return out, fieldProblem(path, "one of productId or text")
		}
		lines[i].qty = l.Quantity
		if l.ProductId != nil {
			id, err := ids.Parse(*l.ProductId)
			if err != nil {
				return out, fieldProblem(path+".productId", "invalid")
			}
			lines[i].productID = id
		} else if lines[i].norm = Normalize(*l.Text); lines[i].norm == "" {
			return out, fieldProblem(path+".text", "empty")
		}
	}
	req := searchReq{Lat: in.Lat, Lng: in.Lng, RadiusM: defaultRadiusM, Sort: sortRelevance, Source: sourceBasket}
	if in.Radius != nil {
		req.RadiusM = int(*in.Radius)
	}
	rs, err := s.rankSettings(ctx)
	if err != nil {
		return out, internal(err)
	}
	stores, logs, err := s.basketCandidates(ctx, q, req, lines, rs, start)
	if err != nil {
		return out, internal(err)
	}
	if err := s.priceBasket(ctx, stores, start); err != nil {
		return out, internal(err)
	}
	rankBasket(stores)

	n, err := q.CountStoresInRadius(ctx, searchdb.CountStoresInRadiusParams{Lng: req.Lng, Lat: req.Lat, RadiusM: float64(req.RadiusM)})
	if err != nil {
		return out, internal(err)
	}
	withAll := 0
	for _, b := range stores {
		if b.covered == len(lines) {
			withAll++
		}
	}
	out.Summary = discoverapi.BasketSearchSummary{StoresChecked: max(int(n), len(stores)), StoresWithAll: withAll}

	resultsMax, err := s.settings.Int(ctx, settings.BasketResultsMax)
	if err != nil {
		return out, internal(err)
	}
	page := stores[:min(len(stores), int(max(resultsMax, 0)))]
	marks := basketBadges(page, len(lines))
	for i, b := range page {
		out.Results = append(out.Results, basketResult(b, len(lines), marks[i], start, rs))
	}
	if withAll == 0 {
		splitMax, err := s.settings.Int(ctx, settings.BasketSplitMaxShops)
		if err != nil {
			return out, internal(err)
		}
		if splitMax >= 2 {
			if sp := bestSplit(stores, len(lines)); sp != nil {
				out.Split = splitResult(sp, start, rs)
			}
		}
	}
	for _, e := range logs {
		s.record(ctx, q, e)
	}
	return out, nil
}

// basketCandidates finds, per line, the stores that cover it, and the
// search log entry of each line.
func (s *Service) basketCandidates(ctx context.Context, q *searchdb.Queries, req searchReq, lines []bLine, rs rankSettings, now time.Time) ([]*bStore, []logEntry, error) {
	byID := map[ids.ID]*bStore{}
	var order []*bStore
	cover := func(k int, found []*candStore) int {
		hits := 0
		for _, st := range found {
			it, ok := choose(st.Items, lines[k])
			if !ok {
				continue
			}
			hits++
			b := byID[st.ID]
			if b == nil {
				b = &bStore{st: st, picks: make([]*bPick, len(lines))}
				byID[st.ID] = b
				order = append(order, b)
			}
			b.picks[k] = &bPick{item: it, qty: lines[k].qty}
		}
		return hits
	}
	logs := make([]logEntry, 0, len(lines))

	// Product lines: one query for all of them.
	var pids []pgtype.UUID
	seen := map[ids.ID]bool{}
	for _, l := range lines {
		if l.productID != ids.Zero && !seen[l.productID] {
			seen[l.productID] = true
			pids = append(pids, ids.UUID(l.productID))
		}
	}
	var productStores []*candStore
	names := map[ids.ID]string{}
	if len(pids) > 0 {
		rows, err := q.BasketProductItems(ctx, searchdb.BasketProductItemsParams{
			Lng: req.Lng, Lat: req.Lat, RadiusM: float64(req.RadiusM), ProductIds: pids, Lim: maxRows,
		})
		if err != nil {
			return nil, nil, err
		}
		same := make([]searchdb.SearchItemsRow, len(rows))
		for i, r := range rows {
			same[i] = searchdb.SearchItemsRow(r)
		}
		if productStores, err = s.group(ctx, same, req, parsed{}, "", nil, nil, rs, now); err != nil {
			return nil, nil, err
		}
		pn, err := q.BasketProductNames(ctx, pids)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range pn {
			if id, ok := ids.FromUUID(r.ID); ok {
				names[id] = r.Name
			}
		}
	}
	for k, l := range lines {
		var found []*candStore
		e := logEntry{req: req, start: now}
		if l.productID != ids.Zero {
			found = productStores
			e.norm, e.product = Normalize(names[l.productID]), ids.UUID(l.productID)
		} else {
			r := req
			r.Q = l.norm
			var err error
			if found, _, _, err = s.find(ctx, q, r, l.norm, rs, now); err != nil {
				return nil, nil, err
			}
			e.norm = l.norm
		}
		hits := cover(k, found)
		e.results, e.stores = hits, hits
		if e.norm != "" { // a product id that names no product is not logged
			logs = append(logs, e)
		}
	}
	return order, logs, nil
}

// priceBasket fills each pick's line total: price × quantity less the
// store's item-level offers.
func (s *Service) priceBasket(ctx context.Context, stores []*bStore, now time.Time) error {
	var customer ids.ID
	if uid, err := ids.Parse(s.userID(ctx)); err == nil {
		customer = uid
	}
	for _, b := range stores {
		var lines []PriceLine
		var picks []*bPick
		for _, p := range b.picks {
			if p == nil {
				continue
			}
			lines = append(lines, PriceLine{
				StoreItemID: p.item.ID, ProductID: p.item.ProductID, UnitPricePaise: p.item.Price, Quantity: p.qty,
			})
			picks = append(picks, p)
		}
		disc, err := s.pricer.LineDiscounts(ctx, customer, b.st.ID, lines, now)
		if err != nil {
			return fmt.Errorf("search: basket pricing: %w", err)
		}
		for i, p := range picks {
			gross := lines[i].UnitPricePaise * int64(lines[i].Quantity)
			var d int64
			if i < len(disc) {
				d = min(max(disc[i], 0), gross)
			}
			p.lineTotal = gross - d
		}
		b.tally()
	}
	return nil
}

// basketResult is one result card.
func basketResult(b *bStore, lines int, badge string, now time.Time, rs rankSettings) discoverapi.BasketSearchStore {
	out := discoverapi.BasketSearchStore{
		StoreId: b.st.ID.String(), StoreSlug: b.st.Slug, StoreName: b.st.Name, Locality: b.st.Locality,
		DistanceM: roundM(b.st.DistanceM), Open: b.st.Open, Bookable: b.st.bookable(),
		CoveredCount: b.covered, LineCount: lines, TotalPaise: int(b.total),
		Covered: coveredLines(b, now, rs), Missing: []discoverapi.BasketMissingLine{},
	}
	for k, p := range b.picks {
		if p == nil {
			out.Missing = append(out.Missing, discoverapi.BasketMissingLine{LineIndex: k})
		}
	}
	if badge != "" {
		v := discoverapi.BasketBadge(badge)
		out.Badge = &v
	}
	return out
}

func splitResult(sp *bSplit, now time.Time, rs rankSettings) *discoverapi.BasketSplitSuggestion {
	out := &discoverapi.BasketSplitSuggestion{Stores: make([]discoverapi.BasketSplitPart, 0, 2), Uncovered: []int{}}
	for _, p := range sp.parts {
		part := discoverapi.BasketSplitPart{
			StoreId: p.st.ID.String(), StoreSlug: p.st.Slug, StoreName: p.st.Name,
			DistanceM: roundM(p.st.DistanceM), Bookable: p.st.bookable(), TotalPaise: int(p.total),
			Covered: coveredLines(p, now, rs), LineIndexes: []int{},
		}
		for k, pk := range p.picks {
			if pk != nil {
				part.LineIndexes = append(part.LineIndexes, k)
			}
		}
		out.Stores = append(out.Stores, part)
	}
	out.Uncovered = append(out.Uncovered, sp.uncovered...)
	return out
}

func coveredLines(b *bStore, now time.Time, rs rankSettings) []discoverapi.BasketCoveredLine {
	out := []discoverapi.BasketCoveredLine{}
	for k, p := range b.picks {
		if p == nil {
			continue
		}
		it := p.item
		l := discoverapi.BasketCoveredLine{
			LineIndex: k, StoreItemId: it.ID.String(), ProductId: it.ProductID.String(), Name: it.Name,
			Variant: it.Variant, PackSize: it.PackSize, Unit: discoverapi.DiscoverUnit(it.Unit),
			PricePaise: int(it.Price), ImageKey: it.ImageKey, Quantity: p.qty, LineTotalPaise: int(p.lineTotal),
			ConfirmedAt: confirmedAt(it.StockUpdatedAt, now, rs.staleAfter),
		}
		if it.Mrp != nil {
			m := int(*it.Mrp)
			l.MrpPaise = &m
		}
		out = append(out, l)
	}
	return out
}

func fieldProblem(path, reason string) error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}
