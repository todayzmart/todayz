package search

import (
	"context"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

const (
	// maxPins is the StoreMap's pin count (the contract says "the nearest
	// 200"); past it pinsTruncated is true. TODO(lead): clusters by zoom
	// are deferred; the app clusters these pins itself.
	maxPins = 200
	// maxStoresScan caps the stores one summary or list reads (open-now and
	// the offer filter run in Go after the query).
	maxStoresScan = 5000
)

// Summary answers GET /v1/discover/summary (C-03 StoreMap).
func (s *Service) Summary(ctx context.Context, p discoverapi.GetDiscoverSummaryParams) (discoverapi.DiscoverSummary, error) {
	now := s.now()
	out := discoverapi.DiscoverSummary{Pins: []discoverapi.DiscoverPin{}, ServerNow: now}
	q, err := s.ready(ctx)
	if err != nil {
		return out, err
	}
	radius := float64(defaultRadiusM)
	if p.Radius != nil {
		radius = float64(*p.Radius)
	}
	n, err := q.CountStoresInRadius(ctx, searchdb.CountStoresInRadiusParams{Lng: p.Lng, Lat: p.Lat, RadiusM: radius})
	if err != nil {
		return out, internal(err)
	}
	listings, err := q.CountListingsInRadius(ctx, searchdb.CountListingsInRadiusParams{Lng: p.Lng, Lat: p.Lat, RadiusM: radius})
	if err != nil {
		return out, internal(err)
	}
	rows, err := q.StoresInRadius(ctx, searchdb.StoresInRadiusParams{Lng: p.Lng, Lat: p.Lat, RadiusM: radius, Lim: maxStoresScan})
	if err != nil {
		return out, internal(err)
	}
	out.StoresNearby, out.ProductsInRadius = int(n), int(listings)
	for _, r := range rows {
		open := s.open.OpenAt(r.Hours, r.ClosedUntil, now)
		if open {
			out.OpenNow++
		}
		if len(out.Pins) < maxPins {
			out.Pins = append(out.Pins, discoverapi.DiscoverPin{
				StoreSlug: r.Slug, Lat: r.Lat, Lng: r.Lng, Open: open, CategoryTint: r.CategoryTint,
			})
		}
	}
	out.PinsTruncated = int(n) > len(out.Pins)
	return out, nil
}

// ListStores answers GET /v1/discover/stores (C-04): nearest first, with the
// open-now, pickup, delivery, has-offer and category filters. A category
// keeps a store whose business is in it or that has an in-stock item in it.
// An unknown category slug lists nothing.
func (s *Service) ListStores(ctx context.Context, p discoverapi.ListDiscoverStoresParams) (discoverapi.DiscoverStoreList, error) {
	now := s.now()
	out := discoverapi.DiscoverStoreList{Items: []discoverapi.DiscoverStoreCard{}, ServerNow: now}
	q, err := s.ready(ctx)
	if err != nil {
		return out, err
	}
	offset, err := decodeCursor(cursorStores, p.Cursor)
	if err != nil {
		return out, err
	}
	limit := pageLimit(p.Limit, defaultLimit)
	radius := float64(defaultRadiusM)
	if p.Radius != nil {
		radius = float64(*p.Radius)
	}
	params := searchdb.StoresInRadiusParams{Lng: p.Lng, Lat: p.Lat, RadiusM: radius, Lim: maxStoresScan}
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
	rows, err := q.StoresInRadius(ctx, params)
	if err != nil {
		return out, internal(err)
	}
	deliveryOn, err := s.settings.Bool(ctx, settings.FeatureDeliveryEnabled)
	if err != nil {
		return out, internal(err)
	}
	want := func(b *bool) bool { return b != nil && *b }
	var offers map[ids.ID]bool
	if want(p.HasOffer) {
		storeIDs := make([]ids.ID, 0, len(rows))
		for _, r := range rows {
			if id, ok := ids.FromUUID(r.ID); ok {
				storeIDs = append(storeIDs, id)
			}
		}
		if offers, err = s.offers.StoreOffers(ctx, storeIDs, now); err != nil {
			return out, internal(err)
		}
	}
	var cards []discoverapi.DiscoverStoreCard
	for _, r := range rows {
		sid, ok := ids.FromUUID(r.ID)
		if !ok {
			continue
		}
		open := s.open.OpenAt(r.Hours, r.ClosedUntil, now)
		delivers := r.AcceptsDelivery && deliveryOn
		if (want(p.OpenNow) && !open) || (want(p.Pickup) && !r.AcceptsPickup) ||
			(want(p.Delivery) && !delivers) || (want(p.HasOffer) && !offers[sid]) {
			continue
		}
		cards = append(cards, storeCard(&candStore{
			ID: sid, Slug: r.Slug, Name: r.Name, Locality: r.Locality, Tint: r.CategoryTint,
			DistanceM: r.DistanceM, Open: open, Pickup: r.AcceptsPickup, Delivery: delivers,
			CashbackBP: r.CashbackBp, Rating: ratingPtr(r.HasRating, r.RatingAvg), RatingCount: r.RatingCount,
		}))
	}
	end := min(offset+limit, len(cards))
	if offset < len(cards) {
		out.Items = append(out.Items, cards[offset:end]...)
	}
	if end < len(cards) {
		out.NextCursor = encodeCursor(cursorStores, end)
	}
	return out, nil
}

func ratingPtr(has bool, v float64) *float64 {
	if !has {
		return nil
	}
	return &v
}
