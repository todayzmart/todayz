package share

// A-23 Gaps (01 §11.1): what customers of a city asked for that no shop had,
// as counts only, after the ask_gap_min_customers k-floor.

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	admindemandapi "github.com/todayz/backend/gen/api/admin-demand"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

func (s *Service) gaps(ctx context.Context, params admindemandapi.ListDemandGapsParams) (admindemandapi.DemandGapList, error) {
	if err := s.ready(ctx); err != nil {
		return admindemandapi.DemandGapList{}, err
	}
	var city pgtype.UUID
	if params.City != nil {
		id, err := ids.Parse(*params.City)
		if err != nil {
			return admindemandapi.DemandGapList{}, fieldErr("query.city", "invalid")
		}
		city = ids.UUID(id)
	}
	var days int
	if params.Days != nil {
		days = *params.Days
	} else {
		n, err := s.settings.Int(ctx, settings.AskGapWindowDays)
		if err != nil {
			return admindemandapi.DemandGapList{}, err
		}
		days = int(n)
	}
	minCustomers, err := s.floorSetting(ctx, settings.AskGapMinCustomers)
	if err != nil {
		return admindemandapi.DemandGapList{}, err
	}
	now := s.now()
	rows, err := s.q(ctx).DemandGaps(ctx, asksdb.DemandGapsParams{
		Since: now.Add(-time.Duration(days) * day), CityPlaceID: city, MinCustomers: int32(minCustomers), // #nosec G115 -- small setting
	})
	if err != nil {
		return admindemandapi.DemandGapList{}, fmt.Errorf("share: demand gaps: %w", err)
	}
	items := make([]admindemandapi.DemandGap, 0, len(rows))
	for _, r := range rows {
		name := r.QueryKey
		if r.ProductName != nil {
			name = *r.ProductName
			if r.ProductVariant != nil && *r.ProductVariant != "" {
				name += " " + *r.ProductVariant
			}
		}
		items = append(items, admindemandapi.DemandGap{
			CityId: idOf(r.CityPlaceID).String(), CityName: r.CityName,
			ProductId: ulidPtr(r.ProductID), ItemName: name, Category: strPtr(r.CategoryName),
			Customers: int(r.Customers), Asks: int(r.Asks), ShopsAsked: int(r.ShopsAsked),
			LastAskedAt: r.LastAskedAt.UTC(),
		})
	}
	return admindemandapi.DemandGapList{Days: days, Items: items, MinCustomers: minCustomers, ServerNow: now}, nil
}
