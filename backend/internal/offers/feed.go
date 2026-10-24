package offers

// The "Offers near you" feed (GET /v1/discover/offers; 01 §12, 04 §2.6).
//
// One card per shop. A shop with a live banner shows the banner; otherwise its
// best live online offer. The cards are ordered nearest first, then rotated by
// the time bucket so every shop in range gets the top spot in turn (fair
// rotation). The caller's coordinates are used for this one query and never
// stored or logged.

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	offersapi "github.com/todayz/backend/gen/api/offers"
	"github.com/todayz/backend/gen/sqlc/offersdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// TODO(lead): the feed size and the rotation step are not in 01 §17; these are
// the conservative readings (a screenful, a quarter of an hour).
const (
	feedCards       = 10
	feedRotation    = 15 * time.Minute
	feedCandidates  = 200
	defaultRadiusM  = 2000
	maxFeedCoordErr = "outside India"
)

// kindRank orders offers of one shop for the card: the most visible first.
var kindRank = map[Kind]int{KindPercent: 0, KindBXGY: 1, KindFlat: 2, KindBillMin: 3}

// bestOffer picks the offer a shop's card shows: by kind, then the larger
// value, then id (stable).
func bestOffer(list []Offer) Offer {
	return slices.MinFunc(list, func(a, b Offer) int {
		if c := cmp.Compare(kindRank[a.Kind], kindRank[b.Kind]); c != 0 {
			return c
		}
		if c := cmp.Compare(b.Value, a.Value); c != 0 {
			return c
		}
		return slices.Compare(a.ID[:], b.ID[:])
	})
}

func (s *Service) listNearby(ctx context.Context, p offersapi.ListNearbyOffersParams) (offersapi.NearbyOffers, error) {
	if err := s.ready(ctx); err != nil {
		return offersapi.NearbyOffers{}, err
	}
	now := s.clock.Now()
	out := offersapi.NearbyOffers{Items: []offersapi.NearbyOffer{}, ServerNow: now}
	offersOn, err := s.enabled(ctx)
	if err != nil {
		return out, err
	}
	bannersOn, err := s.settings.Bool(ctx, settings.FeatureBannersEnabled)
	if err != nil {
		return out, fmt.Errorf("offers: feature_banners_enabled: %w", err)
	}
	bannersOn = bannersOn && s.banners != nil
	if !offersOn && !bannersOn {
		return out, nil
	}
	radius := float64(defaultRadiusM)
	if p.Radius != nil {
		radius = float64(*p.Radius)
	}
	q := s.q(ctx)
	stores, err := q.FeedStores(ctx, offersdb.FeedStoresParams{Lng: p.Lng, Lat: p.Lat, RadiusM: radius, Lim: feedCandidates})
	if err != nil {
		return out, fmt.Errorf("offers: feed stores: %w", err)
	}
	if len(stores) == 0 {
		return out, nil
	}
	storeIDs := make([]pgtype.UUID, len(stores))
	plain := make([]ids.ID, len(stores))
	for i, st := range stores {
		storeIDs[i] = st.ID
		plain[i] = idOf(st.ID)
	}
	var live map[ids.ID][]Offer
	if offersOn {
		if live, err = s.liveByStore(ctx, q, storeIDs, now); err != nil {
			return out, err
		}
	}
	banners := map[ids.ID][]BannerCard{}
	if bannersOn {
		cards, err := s.banners.LiveBanners(ctx, plain, now)
		if err != nil {
			return out, fmt.Errorf("offers: live banners: %w", err)
		}
		for _, c := range cards {
			banners[c.StoreID] = append(banners[c.StoreID], c)
		}
	}

	bucket := int(now.Unix() / int64(feedRotation.Seconds()))
	var cards []offersapi.NearbyOffer
	var shown []ids.ID
	for _, st := range stores {
		sid := idOf(st.ID)
		card := offersapi.NearbyOffer{
			StoreId: ulidOf(st.ID), StoreSlug: st.Slug, StoreName: st.Name, Locality: st.Locality,
			Tint: st.CategoryTint, DistanceM: int(st.DistanceM),
		}
		switch {
		case len(banners[sid]) > 0:
			// Max one banner per shop on a screen; the shop's banners take turns.
			list := banners[sid]
			slices.SortFunc(list, func(a, b BannerCard) int { return slices.Compare(a.ID[:], b.ID[:]) })
			b := list[bucket%len(list)]
			card.Kind = offersapi.NearbyOfferKindBanner
			card.Banner = nearbyBanner(b)
			shown = append(shown, b.ID)
		case len(live[sid]) > 0:
			o := bestOffer(live[sid])
			card.Kind = offersapi.NearbyOfferKindOffer
			card.Offer = nearbySummary(o)
		default:
			continue
		}
		cards = append(cards, card)
	}
	if len(cards) == 0 {
		return out, nil
	}
	// Fair rotation: the same nearest-first order, started at a different shop
	// each bucket, so the top slots move around the shops in range.
	start := bucket % len(cards)
	rotated := append(slices.Clone(cards[start:]), cards[:start]...)
	if len(rotated) > feedCards {
		rotated = rotated[:feedCards]
	}
	out.Items = rotated

	// Impressions of the banners that made the screen.
	var user pgtype.UUID
	if pr, ok := authn.FromContext(ctx); ok {
		user = ids.UUID(pr.UserID)
	}
	for _, c := range rotated {
		if c.Banner != nil {
			s.event(ctx, q, user, "banner_impression", map[string]any{"banner_id": c.Banner.Id})
		}
	}
	return out, nil
}

func nearbySummary(o Offer) *offersapi.NearbyOfferSummary {
	sum := &offersapi.NearbyOfferSummary{
		Id: o.ID.String(), Title: TitleOf(o), Kind: offersapi.OfferKind(o.Kind), Value: int(o.Value),
	}
	if o.MinBillPaise > 0 {
		sum.MinBillPaise = ptr(int(o.MinBillPaise))
	}
	if !o.EndsAt.IsZero() {
		sum.EndsAt = ptr(o.EndsAt)
	}
	return sum
}

func nearbyBanner(b BannerCard) *offersapi.NearbyBanner {
	nb := &offersapi.NearbyBanner{
		Id: b.ID.String(), Headline: b.Headline, ImageKey: b.ImageKey,
		TargetType: offersapi.NearbyBannerTargetType(b.TargetType), TargetItemIds: []offersapi.Ulid{},
	}
	if b.Template != nil {
		t := b.Template
		nb.Template = &t
	}
	if b.TargetID != nil {
		nb.TargetId = ptr(b.TargetID.String())
	}
	for _, id := range b.TargetItemIDs {
		nb.TargetItemIds = append(nb.TargetItemIds, id.String())
	}
	return nb
}
