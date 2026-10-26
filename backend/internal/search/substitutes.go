package search

import (
	"context"
	"math"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// AI-C3 substitutes, rule-based in v1 (11 §AI-C3, D-233): what to buy instead of an
// out-of-stock item. No model and no flag; "AI ranking later" is 10-later.md.

// Fixed numbers of the rules. 01 §17 does not list them; they are the contract's maxItems
// and a first tolerance. TODO(lead): make substitutePackTolerance a platform setting if
// shops ask for a different one.
const (
	// substitutePackTolerance is how far a same-shop alternative's pack may be from the item's
	// (in base units), either way.
	substitutePackTolerance = 0.25
	// maxSameShop and maxNearby bound the two lists (DiscoverSubstitutes maxItems).
	maxSameShop = 4
	maxNearby   = 3
	// substituteRows caps the rows read for the shop's alternatives.
	substituteRows = 200 //settings:ok a per-request row bound, not a product rule
)

// unitFamilies are the units a pack can be compared in, with the factor to the base unit.
var unitFamilies = map[string]struct {
	units  []string
	factor float64
}{
	"g": {[]string{"g", "kg"}, 1}, "kg": {[]string{"g", "kg"}, 1000},
	"ml": {[]string{"ml", "l"}, 1}, "l": {[]string{"ml", "l"}, 1000},
	"pc": {[]string{"pc"}, 1}, "pack": {[]string{"pack"}, 1},
}

// baseSize is a pack in its family's base unit (g, ml, pc, pack).
func baseSize(size float64, unit string) float64 {
	if f, ok := unitFamilies[unit]; ok {
		return size * f.factor
	}
	return size
}

// Substitutes answers GET /v1/discover/stores/{slug}/substitutes.
func (s *Service) Substitutes(ctx context.Context, slug string, p discoverapi.ListDiscoverSubstitutesParams) (discoverapi.DiscoverSubstitutes, error) {
	now := s.now()
	out := discoverapi.DiscoverSubstitutes{
		SameShop: []discoverapi.DiscoverSearchItem{}, Nearby: []discoverapi.DiscoverSearchStore{}, ServerNow: now,
	}
	q, err := s.ready(ctx)
	if err != nil {
		return out, err
	}
	st, err := storeBySlug(ctx, q, slug, nil, nil)
	if err != nil {
		return out, err
	}
	notListed := problem.New(genproblems.NotFound, "This shop doesn't list that item.")
	itemID, perr := ids.Parse(p.ItemId)
	if perr != nil {
		return out, notListed
	}
	src, err := q.SubstituteSource(ctx, searchdb.SubstituteSourceParams{StoreItemID: ids.UUID(itemID), StoreID: st.ID})
	if db.IsNoRows(err) {
		return out, notListed
	}
	if err != nil {
		return out, internal(err)
	}
	rs, err := s.rankSettings(ctx)
	if err != nil {
		return out, internal(err)
	}
	req := searchReq{Lat: p.Lat, Lng: p.Lng, RadiusM: defaultRadiusM}
	if p.Radius != nil {
		req.RadiusM = int(*p.Radius)
	}

	if fam, ok := unitFamilies[src.Unit]; ok {
		base := baseSize(src.PackSize, src.Unit)
		rows, err := q.SubstituteShopItems(ctx, searchdb.SubstituteShopItemsParams{
			Lng: req.Lng, Lat: req.Lat, StoreID: st.ID, CategoryID: src.CategoryID, ProductID: src.ProductID,
			Units: fam.units, MinBase: base * (1 - substitutePackTolerance), MaxBase: base * (1 + substitutePackTolerance),
			Lim: substituteRows,
		})
		if err != nil {
			return out, internal(err)
		}
		same := make([]searchdb.SearchItemsRow, len(rows))
		for i, r := range rows {
			same[i] = searchdb.SearchItemsRow(r)
		}
		sortSameShop(same, src.BrandID, base)
		stores, err := s.group(ctx, same, req, parsed{}, "", nil, nil, rs, now)
		if err != nil {
			return out, internal(err)
		}
		for _, cs := range stores {
			for _, it := range cs.Items {
				if len(out.SameShop) == maxSameShop {
					break
				}
				out.SameShop = append(out.SameShop, searchItem(it, now, rs.staleAfter))
			}
		}
	}

	rows, err := q.BasketProductItems(ctx, searchdb.BasketProductItemsParams{
		Lng: req.Lng, Lat: req.Lat, RadiusM: float64(req.RadiusM), ProductIds: []pgtype.UUID{src.ProductID}, Lim: maxRows,
	})
	if err != nil {
		return out, internal(err)
	}
	other := make([]searchdb.SearchItemsRow, 0, len(rows))
	for _, r := range rows {
		if r.StoreID != st.ID {
			other = append(other, searchdb.SearchItemsRow(r))
		}
	}
	stores, err := s.group(ctx, other, req, parsed{}, "", nil, nil, rs, now)
	if err != nil {
		return out, internal(err)
	}
	sort.SliceStable(stores, func(i, j int) bool {
		if stores[i].DistanceM != stores[j].DistanceM {
			return stores[i].DistanceM < stores[j].DistanceM
		}
		return stores[i].ID.String() < stores[j].ID.String()
	})
	for _, cs := range stores {
		if len(out.Nearby) == maxNearby {
			break
		}
		sort.SliceStable(cs.Items, func(i, j int) bool { return cs.Items[i].Price < cs.Items[j].Price })
		cs.Items = cs.Items[:1]
		out.Nearby = append(out.Nearby, storeResult(cs, modes{}, "", now, rs))
	}
	return out, nil
}

// sortSameShop orders the shop's alternatives: the item's brand first, then the pack
// closest to the item's, then the cheaper, then the item id.
func sortSameShop(rows []searchdb.SearchItemsRow, brand pgtype.UUID, base float64) {
	sameBrand := func(r searchdb.SearchItemsRow) bool {
		return brand.Valid && r.BrandID.Valid && r.BrandID.Bytes == brand.Bytes
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if sa, sb := sameBrand(a), sameBrand(b); sa != sb {
			return sa
		}
		dA, dB := math.Abs(baseSize(a.PackSize, a.Unit)-base), math.Abs(baseSize(b.PackSize, b.Unit)-base)
		if dA != dB {
			return dA < dB
		}
		if a.PricePaise != b.PricePaise {
			return a.PricePaise < b.PricePaise
		}
		return string(a.StoreItemID.Bytes[:]) < string(b.StoreItemID.Bytes[:])
	})
}
