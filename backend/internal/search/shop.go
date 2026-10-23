package search

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// The shop page (C-07) and its catalogue. Only a live store of a verified
// business has one; anything else is 404 not_found, the same answer as a
// slug that never existed (D-40: a paused or suspended shop is not
// disclosed).

// weekdays are the keys of the weekly hours, Monday first (01 §3).
var weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// storedHours is stores.hours as W1-04 writes it (business.Hours). It is
// decoded here, not imported, because domains do not import each other.
type storedHours struct {
	Weekly   map[string][]discoverapi.DiscoverHoursRange `json:"weekly"`
	Holidays []struct {
		Date   string                           `json:"date"`
		Ranges []discoverapi.DiscoverHoursRange `json:"ranges"`
	} `json:"holidays"`
}

// hoursOut is the store's hours for the page: every weekday key (an empty
// list is closed) and the holidays from today (IST) on, in date order.
func hoursOut(raw []byte, now time.Time) discoverapi.DiscoverHours {
	out := discoverapi.DiscoverHours{
		Weekly: make(map[string][]discoverapi.DiscoverHoursRange, len(weekdays)), Holidays: []discoverapi.DiscoverHoursHoliday{},
	}
	var h storedHours
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &h) // stored hours were validated on write; bad data shows as closed
	}
	for _, d := range weekdays {
		rs := h.Weekly[d]
		if rs == nil {
			rs = []discoverapi.DiscoverHoursRange{}
		}
		out.Weekly[d] = rs
	}
	today := now.In(clock.IST).Format(time.DateOnly)
	for _, hd := range h.Holidays {
		if hd.Date < today {
			continue
		}
		t, err := time.Parse(time.DateOnly, hd.Date)
		if err != nil {
			continue
		}
		rs := hd.Ranges
		if rs == nil {
			rs = []discoverapi.DiscoverHoursRange{}
		}
		out.Holidays = append(out.Holidays, discoverapi.DiscoverHoursHoliday{Date: openapi_types.Date{Time: t}, Ranges: rs})
	}
	sort.Slice(out.Holidays, func(i, j int) bool { return out.Holidays[i].Date.Before(out.Holidays[j].Date.Time) })
	return out
}

// storeBySlug loads the page's store, 404 when it is not a live store of a
// verified business.
func storeBySlug(ctx context.Context, q *searchdb.Queries, slug string, lat, lng *float64) (searchdb.StoreBySlugRow, error) {
	r, err := q.StoreBySlug(ctx, searchdb.StoreBySlugParams{Slug: slug, Lat: lat, Lng: lng})
	if db.IsNoRows(err) {
		return r, problem.New(genproblems.NotFound, "This shop isn't on TodayZ right now.")
	}
	if err != nil {
		return r, internal(err)
	}
	return r, nil
}

// GetStore answers GET /v1/discover/stores/{slug}.
func (s *Service) GetStore(ctx context.Context, slug string, p discoverapi.GetDiscoverStoreParams) (discoverapi.DiscoverStorePage, error) {
	now := s.now()
	q, err := s.ready(ctx)
	if err != nil {
		return discoverapi.DiscoverStorePage{}, err
	}
	// The point only adds the distance. Half a point is no point: the page is
	// the same without it, and the schema cannot say "both or neither", so the
	// contract smoke counts a 422 here as refusing a valid request.
	// TODO(lead): conservative reading; a 422 is the alternative.
	if p.Lat == nil || p.Lng == nil {
		p.Lat, p.Lng = nil, nil
	}
	r, err := storeBySlug(ctx, q, slug, p.Lat, p.Lng)
	if err != nil {
		return discoverapi.DiscoverStorePage{}, err
	}
	deliveryOn, err := s.settings.Bool(ctx, settings.FeatureDeliveryEnabled)
	if err != nil {
		return discoverapi.DiscoverStorePage{}, internal(err)
	}
	delivers := r.AcceptsDelivery && deliveryOn
	out := discoverapi.DiscoverStorePage{
		StoreSlug: r.Slug, Name: r.Name, Locality: r.Locality, AddressLine1: r.AddressLine1, AddressLine2: r.AddressLine2,
		Landmark: r.Landmark, City: r.City, Pincode: r.Pincode, Lat: r.Lat, Lng: r.Lng,
		Open: s.open.OpenAt(r.Hours, r.ClosedUntil, now), Hours: hoursOut(r.Hours, now),
		Pickup: r.AcceptsPickup, Delivery: delivers, Bookable: r.AcceptsPickup || delivers,
		Rating: ratingPtr(r.HasRating, r.RatingAvg), RatingCount: int(r.RatingCount),
		CashbackPct: cashbackPct(r.CashbackBp), CategoryTint: r.CategoryTint,
		DeliveryFeePaise: int(r.DeliveryFeePaise), DeliveryMinOrderPaise: int(r.DeliveryMinOrderPaise),
		DeliveryRadiusM: int(r.DeliveryRadiusM), ServerNow: now,
	}
	if r.DeliveryFreeAbovePaise != nil {
		v := int(*r.DeliveryFreeAbovePaise)
		out.DeliveryFreeAbovePaise = &v
	}
	if r.HasDistance {
		d := roundM(r.DistanceM)
		out.DistanceM = &d
	}
	if r.ClosedUntil != nil && r.ClosedUntil.After(now) {
		v := r.ClosedUntil.UTC()
		out.ClosedUntil = &v
	}
	return out, nil
}

// ListStoreItems answers GET /v1/discover/stores/{slug}/items: the shop's
// listed items, in stock first, then by name, optionally matching q (with
// synonyms; a pack size in q is ignored here) and a category.
func (s *Service) ListStoreItems(ctx context.Context, slug string, p discoverapi.ListDiscoverStoreItemsParams) (discoverapi.DiscoverStoreItemList, error) {
	now := s.now()
	out := discoverapi.DiscoverStoreItemList{Items: []discoverapi.DiscoverSearchItem{}, ServerNow: now}
	q, err := s.ready(ctx)
	if err != nil {
		return out, err
	}
	offset, err := decodeCursor(cursorItems, p.Cursor)
	if err != nil {
		return out, err
	}
	limit := pageLimit(p.Limit, defaultLimit)
	st, err := storeBySlug(ctx, q, slug, nil, nil)
	if err != nil {
		return out, err
	}
	params := searchdb.StoreItemsPageParams{StoreID: st.ID, Off: i32(offset), Lim: i32(limit + 1)}
	if p.Q != nil {
		if words := parse(Normalize(*p.Q)).words; len(words) > 0 {
			slots, err := expand(ctx, q, words)
			if err != nil {
				return out, internal(err)
			}
			tsq := tsquery(slots)
			params.Tsq = &tsq
		}
	}
	if p.Category != nil && *p.Category != "" {
		cats, err := loadCategories(ctx, q)
		if err != nil {
			return out, internal(err)
		}
		tree, ok := cats.slugTree(*p.Category)
		if !ok {
			return out, nil
		}
		params.CategoryIds = tree
	}
	rows, err := q.StoreItemsPage(ctx, params)
	if err != nil {
		return out, internal(err)
	}
	if len(rows) > limit {
		rows = rows[:limit]
		out.NextCursor = encodeCursor(cursorItems, offset+limit)
	}
	stale, err := s.settings.Duration(ctx, settings.StockStaleAfterHours)
	if err != nil {
		return out, internal(err)
	}
	itemIDs := make([]ids.ID, 0, len(rows))
	for _, r := range rows {
		if id, ok := ids.FromUUID(r.StoreItemID); ok {
			itemIDs = append(itemIDs, id)
		}
	}
	offers, err := s.offers.ItemOffers(ctx, itemIDs, now)
	if err != nil {
		return out, internal(err)
	}
	for _, r := range rows {
		iid, ok1 := ids.FromUUID(r.StoreItemID)
		pid, ok2 := ids.FromUUID(r.ProductID)
		if !ok1 || !ok2 {
			continue
		}
		name := r.ProductName
		if r.LocalName != nil && *r.LocalName != "" {
			name = *r.LocalName
		}
		it := searchItem(candItem{
			ID: iid, ProductID: pid, Name: name, Variant: r.Variant, Brand: r.BrandName, PackSize: r.PackSize,
			Unit: r.Unit, Price: r.PricePaise, Mrp: r.MrpPaise, StockUpdatedAt: r.StockUpdatedAt,
			ImageKey: r.ImageKey, Offer: offers[iid],
		}, now, stale)
		it.InStock = r.InStock
		out.Items = append(out.Items, it)
	}
	return out, nil
}
