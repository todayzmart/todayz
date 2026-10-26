package search

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// Limits of the search query (not product rules: bounds on one request).
const (
	// maxRows caps the in-stock listings one search reads before ranking;
	// past it the farthest-scanned rows are not seen. At launch density a
	// 20 km radius holds far fewer matches for one query.
	maxRows = 2000 //settings:ok a per-request row bound, not a product rule
	// typoLimit caps the products the trigram fallback adds.
	typoLimit = 50
	// typoMinRunes is the shortest query the trigram fallback runs for
	// (shorter strings have too few trigrams to mean anything).
	typoMinRunes = 3
	// defaultRadiusM is the contract's default (the 2 km chip, 01 §11); the
	// generated params carry it only when the client sends it.
	defaultRadiusM = 2000 //settings:ok the contract's radius enum default, not a platform setting
	defaultLimit   = 20
)

// searchReq is GET /v1/discover/search after defaults.
type searchReq struct {
	Q         string
	Lat, Lng  float64
	RadiusM   int
	Category  string
	Brand     string
	ProductID *string
	OpenNow   bool
	Pickup    bool
	Delivery  bool
	HasOffer  bool
	MaxPrice  *int64
	Sort      string
	Source    string
	Limit     int
	Cursor    *string
	// Parse is false when the customer turned the refine-chip parser off (parse=false).
	Parse bool
	// skipMiss is set by refine when the query was only chips, so it has no words to miss.
	skipMiss bool
}

func searchReqFrom(p discoverapi.SearchDiscoverParams) searchReq {
	r := searchReq{
		Q: p.Q, Lat: p.Lat, Lng: p.Lng, RadiusM: defaultRadiusM,
		Sort: sortRelevance, Source: "typed", Limit: pageLimit(p.Limit, defaultLimit), Cursor: p.Cursor,
		Parse: p.Parse == nil || *p.Parse,
	}
	if p.Radius != nil {
		r.RadiusM = int(*p.Radius)
	}
	if p.Category != nil {
		r.Category = *p.Category
	}
	if p.Brand != nil {
		r.Brand = *p.Brand
	}
	r.ProductID = p.ProductId
	r.OpenNow = p.OpenNow != nil && *p.OpenNow
	r.Pickup = p.Pickup != nil && *p.Pickup
	r.Delivery = p.Delivery != nil && *p.Delivery
	r.HasOffer = p.HasOffer != nil && *p.HasOffer
	if p.MaxPricePaise != nil {
		v := int64(*p.MaxPricePaise)
		r.MaxPrice = &v
	}
	if p.Sort != nil {
		r.Sort = string(*p.Sort)
	}
	if p.Source != nil {
		r.Source = string(*p.Source)
	}
	return r
}

// rankSettings reads the ranking weights, the stale-stock window and the
// delivery switch from platform_settings.
type rankSettings struct {
	w               weights
	staleAfter      time.Duration
	deliveryEnabled bool
}

func (s *Service) rankSettings(ctx context.Context) (rankSettings, error) {
	var rs rankSettings
	keys := []struct {
		k settings.Key
		v *float64
	}{
		{settings.SearchWText, &rs.w.text},
		{settings.SearchWDistance, &rs.w.distance},
		{settings.SearchWReliability, &rs.w.reliability},
		{settings.SearchWRating, &rs.w.rating},
		{settings.SearchWOffer, &rs.w.offer},
	}
	for _, k := range keys {
		bp, err := s.settings.BP(ctx, k.k)
		if err != nil {
			return rs, err
		}
		*k.v = float64(bp) / 10000 //settings:ok basis points per unit
	}
	var err error
	if rs.staleAfter, err = s.settings.Duration(ctx, settings.StockStaleAfterHours); err != nil {
		return rs, err
	}
	if rs.deliveryEnabled, err = s.settings.Bool(ctx, settings.FeatureDeliveryEnabled); err != nil {
		return rs, err
	}
	return rs, nil
}

// Search answers GET /v1/discover/search (01 §11, D-101).
func (s *Service) Search(ctx context.Context, p discoverapi.SearchDiscoverParams) (discoverapi.DiscoverSearchResult, error) {
	start := s.now()
	req := searchReqFrom(p)
	out := discoverapi.DiscoverSearchResult{
		Query: strings.TrimSpace(req.Q), Results: []discoverapi.DiscoverSearchStore{},
		Chips: []discoverapi.DiscoverChip{}, ServerNow: start,
	}
	q, err := s.ready(ctx)
	if err != nil {
		return out, err
	}
	if out.Chips, out.RefineHint, err = s.refine(ctx, &req); err != nil {
		return out, internal(err)
	}
	out.Query = strings.TrimSpace(req.Q)
	offset, err := decodeCursor(cursorSearch, req.Cursor)
	if err != nil {
		return out, err
	}
	norm := Normalize(req.Q)
	if norm == "" {
		return out, nil // nothing searchable (only punctuation): nothing to log
	}
	rs, err := s.rankSettings(ctx)
	if err != nil {
		return out, internal(err)
	}
	stores, found, miss, err := s.find(ctx, q, req, norm, rs, start)
	if err != nil {
		return out, internal(err)
	}
	if !out.RefineHint && miss.possible && req.Parse && !req.skipMiss && found {
		// The last test of a synonym miss, asked only when nothing else recognised the words:
		// is there an active master product with them, in stock nearby or not (D-238).
		known, err := q.CatalogueKnows(ctx, miss.tsq)
		if err != nil {
			return out, internal(err)
		}
		out.RefineHint = !known
	}
	m := modes{pickup: req.Pickup, delivery: req.Delivery}
	if found {
		rank(stores, rs.w, req.Sort)
		n, err := q.CountStoresInRadius(ctx, searchdb.CountStoresInRadiusParams{Lng: req.Lng, Lat: req.Lat, RadiusM: float64(req.RadiusM)})
		if err != nil {
			return out, internal(err)
		}
		if withIt, minPrice, ok := summary(stores); ok {
			out.Summary = &discoverapi.DiscoverSearchSummary{
				StoresChecked: max(int(n), withIt), StoresWithIt: withIt, MinPricePaise: int(minPrice),
			}
		}
	}
	end := min(offset+req.Limit, len(stores))
	var page []*candStore
	if offset < len(stores) {
		page = stores[offset:end]
	}
	var marks map[int]string
	if offset == 0 {
		marks = badges(page, m)
	}
	for i, st := range page {
		out.Results = append(out.Results, storeResult(st, m, marks[i], start, rs))
	}
	if end < len(stores) {
		out.NextCursor = encodeCursor(cursorSearch, end)
	}
	// A filter naming a brand or category that does not exist is a bad
	// request shape, not demand: it is not logged, so it never shows as a
	// zero-result search (TODO(lead): conservative reading of 01 §11).
	if offset == 0 && found {
		results := 0
		for _, st := range stores {
			results += len(st.Items)
		}
		s.record(ctx, q, logEntry{
			req: req, norm: norm, results: results, stores: len(stores),
			start: start,
		})
	}
	return out, nil
}

// synonymMiss is what find knows of refineHint's second half (01 §11.1, D-238). possible is
// true when the synonym table matched none of the query's phrases and nothing else in the
// search recognised the words: no listing, no trigram product, no category name, not a
// barcode and not one product's page. The caller makes the last test, whether the master
// catalogue holds the words (CatalogueKnows with tsq), so only a search that gets that
// far pays for it.
type synonymMiss struct {
	possible bool
	tsq      string
}

// find runs the filter-first query and the request's filters. found is false
// when a filter names a brand or category that does not exist (no results).
func (s *Service) find(ctx context.Context, q *searchdb.Queries, req searchReq, norm string, rs rankSettings, now time.Time) ([]*candStore, bool, synonymMiss, error) {
	var miss synonymMiss
	pq := parse(norm)
	core := pq.core()
	slots, hit, err := expand(ctx, q, pq.words)
	if err != nil {
		return nil, false, miss, err
	}
	cats, err := loadCategories(ctx, q)
	if err != nil {
		return nil, false, miss, err
	}
	params := searchdb.SearchItemsParams{
		Tsq: tsquery(slots), Q: core, Lng: req.Lng, Lat: req.Lat, RadiusM: float64(req.RadiusM),
		TypoIds: []pgtype.UUID{}, MatchCategoryIds: cats.named(core), MaxPrice: req.MaxPrice, Lim: maxRows,
	}
	if params.MatchCategoryIds == nil {
		params.MatchCategoryIds = []pgtype.UUID{}
	}
	if req.Category != "" {
		tree, ok := cats.slugTree(req.Category)
		if !ok {
			return nil, false, miss, nil
		}
		params.CategoryIds = tree
	}
	if req.Brand != "" {
		id, err := q.BrandBySlug(ctx, req.Brand)
		if db.IsNoRows(err) {
			return nil, false, miss, nil
		}
		if err != nil {
			return nil, false, miss, err
		}
		params.BrandID = id
	}
	if req.ProductID != nil {
		id, perr := ids.Parse(*req.ProductID)
		if perr != nil { // a product id that is no id names no product: no results
			return nil, false, miss, nil //nolint:nilerr // no results is the answer
		}
		params.ProductID = ids.UUID(id)
	}
	if utf8.RuneCountInString(core) >= typoMinRunes {
		typo, err := q.TypoProducts(ctx, searchdb.TypoProductsParams{Q: core, Lim: typoLimit})
		if err != nil {
			return nil, false, miss, err
		}
		params.TypoIds = typo
	}
	// A barcode finds its product (D-99 counts it as an exact match), which
	// then scores as an exact name.
	exact := map[[16]byte]bool{}
	if isBarcode(norm) {
		codes, err := q.ProductsByBarcode(ctx, norm)
		if err != nil {
			return nil, false, miss, err
		}
		for _, id := range codes {
			exact[id.Bytes] = true
		}
		params.TypoIds = append(params.TypoIds, codes...)
	}
	rows, err := q.SearchItems(ctx, params)
	if err != nil {
		return nil, false, miss, err
	}
	named := make(map[[16]byte]bool, len(params.TypoIds))
	for _, id := range params.TypoIds {
		named[id.Bytes] = true
	}
	miss.possible = !hit && len(rows) == 0 && len(params.TypoIds) == 0 && len(params.MatchCategoryIds) == 0 &&
		req.ProductID == nil && !isBarcode(norm)
	miss.tsq = params.Tsq
	stores, err := s.group(ctx, rows, req, pq, core, exact, named, rs, now)
	return stores, true, miss, err
}

// group turns the rows into stores with their items, applying the filters
// the query does not: the pack size, open now, pickup, delivery and has
// offer. Each item gets its text score and offer flag. named holds the
// products the trigram fallback or a barcode added; a row with no full-text
// match outside it came in only by its category.
func (s *Service) group(ctx context.Context, rows []searchdb.SearchItemsRow, req searchReq, pq parsed, core string, exact, named map[[16]byte]bool, rs rankSettings, now time.Time) ([]*candStore, error) {
	itemIDs := make([]ids.ID, 0, len(rows))
	for _, r := range rows {
		if id, ok := ids.FromUUID(r.StoreItemID); ok {
			itemIDs = append(itemIDs, id)
		}
	}
	offers, err := s.offers.ItemOffers(ctx, itemIDs, now)
	if err != nil {
		return nil, err
	}
	byStore := map[ids.ID]*candStore{}
	var order []*candStore
	for _, r := range rows {
		if pq.pack != nil && !pq.pack.matches(r.PackSize, r.Unit) {
			continue
		}
		sid, ok1 := ids.FromUUID(r.StoreID)
		iid, ok2 := ids.FromUUID(r.StoreItemID)
		pid, ok3 := ids.FromUUID(r.ProductID)
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		st, seen := byStore[sid]
		if !seen {
			isOpen := s.open.OpenAt(r.Hours, r.ClosedUntil, now)
			delivers := r.AcceptsDelivery && rs.deliveryEnabled
			if (req.OpenNow && !isOpen) || (req.Pickup && !r.AcceptsPickup) || (req.Delivery && !delivers) {
				byStore[sid] = nil
				continue
			}
			st = &candStore{
				ID: sid, Slug: r.StoreSlug, Name: r.StoreName, Locality: r.StoreLocality, Tint: r.CategoryTint,
				DistanceM: r.DistanceM, Open: isOpen, Pickup: r.AcceptsPickup, Delivery: delivers,
				FeePaise: r.DeliveryFeePaise, FreeAbove: r.DeliveryFreeAbovePaise, CashbackBP: r.CashbackBp,
				RatingCount: r.RatingCount,
			}
			if r.HasRating {
				v := r.RatingAvg
				st.Rating = &v
			}
			if r.HasReliability {
				v := r.Reliability
				st.Reliability = &v
			}
			byStore[sid] = st
			order = append(order, st)
		}
		if st == nil {
			continue // the store failed a filter
		}
		offer := offers[iid]
		if req.HasOffer && !offer {
			continue
		}
		name := r.ProductName
		if r.LocalName != nil && strings.TrimSpace(*r.LocalName) != "" {
			name = *r.LocalName
		}
		st.Items = append(st.Items, candItem{
			ID: iid, ProductID: pid, Name: name, Variant: r.Variant, Brand: r.BrandName,
			PackSize: r.PackSize, Unit: r.Unit, Price: r.PricePaise, Mrp: r.MrpPaise, Available: r.Available,
			StockUpdatedAt: r.StockUpdatedAt, ImageKey: r.ImageKey, Offer: offer,
			Text:     textScore(core, r, exact[r.ProductID.Bytes]),
			Category: !r.Fts && !named[r.ProductID.Bytes],
		})
	}
	out := order[:0]
	for _, st := range order {
		if len(st.Items) > 0 {
			out = append(out, st)
		}
	}
	return out, nil
}

// Text score parts (01 §11 "text", 0–1). An exact name is 1; a full-text
// match is 0.6 plus up to 0.4 for how close the name is; a match through the
// trigram fallback only (a typo) scores below any full-text match; a match
// only because the query names the item's category scores 0.3.
const (
	textFTSBase     = 0.6
	textFTSSim      = 0.4
	textTypoFactor  = 0.6
	textCategoryHit = 0.3
)

// textScore scores one row against the query words; a product the query
// names exactly (its barcode) scores 1.
func textScore(core string, r searchdb.SearchItemsRow, exact bool) float64 {
	if exact {
		return 1
	}
	names := []string{Normalize(r.ProductName)}
	if r.LocalName != nil {
		names = append(names, Normalize(*r.LocalName))
	}
	if r.Variant != nil {
		names = append(names, Normalize(r.ProductName+" "+*r.Variant))
	}
	if r.BrandName != nil {
		names = append(names, Normalize(*r.BrandName+" "+r.ProductName))
	}
	for _, n := range names {
		if n != "" && n == core {
			return 1
		}
	}
	sim := clamp01(r.Sim)
	switch {
	case r.Fts:
		return textFTSBase + textFTSSim*sim
	case sim > 0:
		return textTypoFactor * sim
	default:
		return textCategoryHit
	}
}

// storeResult is one result card.
func storeResult(st *candStore, m modes, badge string, now time.Time, rs rankSettings) discoverapi.DiscoverSearchStore {
	out := discoverapi.DiscoverSearchStore{Store: storeCard(st), Items: []discoverapi.DiscoverSearchItem{}}
	for i, it := range st.Items {
		if i == maxItemsPerStore {
			break
		}
		out.Items = append(out.Items, searchItem(it, now, rs.staleAfter))
	}
	if badge != "" {
		b := discoverapi.DiscoverBadge(badge)
		out.Badge = &b
	}
	if showFee(st, m) {
		fee := int(deliveryFee(st, st.lead().Price))
		out.DeliveryFeePaise = &fee
	}
	return out
}

// storeCard is the DiscoverStoreCard of a store.
func storeCard(st *candStore) discoverapi.DiscoverStoreCard {
	return discoverapi.DiscoverStoreCard{
		StoreId: st.ID.String(), StoreSlug: st.Slug, Name: st.Name, Locality: st.Locality, CategoryTint: st.Tint,
		DistanceM: roundM(st.DistanceM), Open: st.Open, Pickup: st.Pickup, Delivery: st.Delivery,
		Bookable: st.bookable(), Rating: st.Rating, RatingCount: int(st.RatingCount),
		CashbackPct: cashbackPct(st.CashbackBP),
	}
}

// searchItem is the DiscoverSearchItem of an item; confirmedAt only while the
// stock confirmation is fresh (D-101).
func searchItem(it candItem, now time.Time, staleAfter time.Duration) discoverapi.DiscoverSearchItem {
	out := discoverapi.DiscoverSearchItem{
		StoreItemId: it.ID.String(), ProductId: it.ProductID.String(), Name: it.Name,
		Variant: it.Variant, Brand: it.Brand, PackSize: it.PackSize, Unit: discoverapi.DiscoverUnit(it.Unit),
		PricePaise: int(it.Price), HasOffer: it.Offer, InStock: true, ImageKey: it.ImageKey,
		ConfirmedAt: confirmedAt(it.StockUpdatedAt, now, staleAfter),
	}
	if it.Mrp != nil {
		v := int(*it.Mrp)
		out.MrpPaise = &v
	}
	return out
}

// confirmedAt is t while it is within staleAfter of now, else nil.
func confirmedAt(t *time.Time, now time.Time, staleAfter time.Duration) *time.Time {
	if t == nil || now.Sub(*t) > staleAfter {
		return nil
	}
	v := t.UTC()
	return &v
}

func roundM(m float64) int {
	if m < 0 {
		return 0
	}
	return int(m + 0.5)
}

// cashbackPct turns basis points into a percentage (250 bp = 2.5 %).
func cashbackPct(bp int32) float64 { return float64(bp) / 100 }
