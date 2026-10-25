package insights

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	insightsapi "github.com/todayz/backend/gen/api/insights"
	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// benchmarkMinBusinesses is the B-12 seconds-to-accept benchmark's floor:
// the nearby median is shown only with at least 5 other businesses behind
// it (D-71; a coach statement built on it needs k ≥ 10).
const benchmarkMinBusinesses = 5 //settings:ok D-71 fixes the B-12 benchmark at k ≥ 5

func rangeDays(r insightsapi.AnalyticsRange) int {
	switch r {
	case insightsapi.AnalyticsRangeN7d:
		return 7
	case insightsapi.AnalyticsRangeN90d:
		return 90
	}
	return 30
}

func istDay(d facts.Day) openapi_types.Date { return openapi_types.Date{Time: time.Time(d)} }

// Analytics is the B-12 Sales tab over the last r whole IST days before
// today; with a metric, only that section.
func (s *Service) Analytics(ctx context.Context, storeID ids.ID, r insightsapi.AnalyticsRange, metric *insightsapi.AnalyticsMetric) (insightsapi.StoreAnalytics, error) {
	if err := s.ready(ctx); err != nil {
		return insightsapi.StoreAnalytics{}, err
	}
	now := s.now()
	today := facts.DayOf(now)
	days := rangeDays(r)
	first := today.Add(-days)
	from, to := first.Start(), today.Start()
	q := s.q(ctx)
	store := ids.UUID(storeID)
	out := insightsapi.StoreAnalytics{Range: r, From: istDay(first), To: istDay(today.Add(-1)), ServerNow: now}
	want := func(m insightsapi.AnalyticsMetric) bool { return metric == nil || *metric == m }

	if want(insightsapi.Sales) {
		rows, err := q.DailySales(ctx, insightsdb.DailySalesParams{StoreID: store, FromAt: from, ToAt: to})
		if err != nil {
			return out, fmt.Errorf("insights: daily sales: %w", err)
		}
		byDay := map[facts.Day]insightsdb.DailySalesRow{}
		for _, d := range rows {
			byDay[facts.DayFromPG(d.Day)] = d
		}
		daily := make([]insightsapi.DaySales, 0, days)
		for d := first; d.Before(today); d = d.Add(1) {
			x := byDay[d]
			daily = append(daily, insightsapi.DaySales{
				Day: istDay(d), OnlinePaise: int(x.OnlinePaise), PosPaise: int(x.PosPaise),
				OnlineBills: int(x.OnlineBills), PosBills: int(x.PosBills),
			})
		}
		out.Daily = &daily
		hours, err := q.SalesByHour(ctx, insightsdb.SalesByHourParams{StoreID: store, FromAt: from, ToAt: to})
		if err != nil {
			return out, fmt.Errorf("insights: sales by hour: %w", err)
		}
		var h [hoursPerDay]int
		for _, x := range hours {
			h[x.Hour] = int(x.Paise)
		}
		byHour := make([]insightsapi.HourTotal, hoursPerDay)
		for i := range hoursPerDay {
			byHour[i] = insightsapi.HourTotal{Hour: i, Paise: h[i]}
		}
		out.ByHour = &byHour
	}
	if want(insightsapi.Funnel) {
		f, err := s.funnel(ctx, q, storeID, first, today)
		if err != nil {
			return out, err
		}
		out.Funnel = &f
	}
	if want(insightsapi.Items) {
		top, err := analyticsTop(ctx, q, storeID, from, to)
		if err != nil {
			return out, err
		}
		out.TopItems = &top
	}
	if want(insightsapi.Categories) {
		rows, err := q.CategoryMix(ctx, insightsdb.CategoryMixParams{StoreID: store, FromAt: from, ToAt: to})
		if err != nil {
			return out, fmt.Errorf("insights: category mix: %w", err)
		}
		mix := make([]insightsapi.CategoryShare, 0, len(rows))
		for _, c := range rows {
			mix = append(mix, insightsapi.CategoryShare{Category: c.Category, Paise: int(c.Paise)})
		}
		out.CategoryMix = &mix
	}
	if want(insightsapi.Repeat) {
		rr, err := s.repeat(ctx, q, storeID, from, to)
		if err != nil {
			return out, err
		}
		out.Repeat = &rr
	}
	if want(insightsapi.Ratings) {
		rows, err := q.RatingsByWeek(ctx, insightsdb.RatingsByWeekParams{StoreID: store, FromAt: from, ToAt: to})
		if err != nil {
			return out, fmt.Errorf("insights: ratings: %w", err)
		}
		weeks := make([]insightsapi.WeekRating, 0, len(rows))
		for _, w := range rows {
			weeks = append(weeks, insightsapi.WeekRating{
				Week: istDay(facts.DayFromPG(w.Week)), Reviews: int(w.Reviews), AvgStars: float32(math.Round(w.AvgStars*10) / 10),
			})
		}
		out.Ratings = &weeks
	}
	return out, nil
}

func (s *Service) funnel(ctx context.Context, q *insightsdb.Queries, storeID ids.ID, first, today facts.Day) (insightsapi.OrderFunnel, error) {
	store := ids.UUID(storeID)
	f, err := q.OrderFunnel(ctx, insightsdb.OrderFunnelParams{StoreID: store, FromAt: first.Start(), ToAt: today.Start()})
	if err != nil {
		return insightsapi.OrderFunnel{}, fmt.Errorf("insights: funnel: %w", err)
	}
	out := insightsapi.OrderFunnel{
		Received: int(f.Received), Accepted: int(f.Accepted), Completed: int(f.Completed), Rejected: int(f.Rejected),
		NotAccepted: int(f.NotAccepted), NoShow: int(f.NoShow), Cancelled: int(f.Cancelled),
	}
	if f.Accepted > 0 {
		p := int(f.P50AcceptSeconds)
		out.P50AcceptSeconds = &p
	}
	// "Nearby" is the insight radius (insight_wanted_nearby_radius_m).
	radius, err := s.settings.Int(ctx, settings.InsightWantedNearbyRadiusM)
	if err != nil {
		return out, fmt.Errorf("insights: %s: %w", settings.InsightWantedNearbyRadiusM, err)
	}
	m, err := q.NearbyAcceptMedian(ctx, insightsdb.NearbyAcceptMedianParams{
		StoreID: store, RadiusM: float64(radius), FromDay: first.PG(), ToDay: today.PG(),
	})
	if err != nil {
		return out, fmt.Errorf("insights: nearby accept median: %w", err)
	}
	if m.Businesses >= benchmarkMinBusinesses {
		v := int(m.MedianSeconds)
		out.NearbyMedianAcceptSeconds = &v
	}
	return out, nil
}

func analyticsTop(ctx context.Context, q *insightsdb.Queries, storeID ids.ID, from, to time.Time) ([]insightsapi.AnalyticsTopItem, error) {
	store := ids.UUID(storeID)
	sales, err := q.ItemSales(ctx, insightsdb.ItemSalesParams{StoreID: store, FromAt: from, ToAt: to})
	if err != nil {
		return nil, fmt.Errorf("insights: item sales: %w", err)
	}
	items, err := q.StoreItems(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("insights: items: %w", err)
	}
	byID := make(map[ids.ID]insightsdb.StoreItemsRow, len(items))
	for _, it := range items {
		id, _ := ids.FromUUID(it.ID)
		byID[id] = it
	}
	sort.SliceStable(sales, func(i, j int) bool {
		if sales[i].Units != sales[j].Units {
			return sales[i].Units > sales[j].Units
		}
		return sales[i].RevenuePaise > sales[j].RevenuePaise
	})
	out := []insightsapi.AnalyticsTopItem{}
	for _, r := range sales {
		if len(out) == topItemsShown {
			break
		}
		id, _ := ids.FromUUID(r.StoreItemID)
		it, ok := byID[id]
		if !ok || r.Units <= 0 {
			continue
		}
		out = append(out, insightsapi.AnalyticsTopItem{
			StoreItemId: id.String(), Name: it.Name, Size: it.Size, Sold: int(r.Units), RevenuePaise: int(r.RevenuePaise),
		})
	}
	return out, nil
}

// repeat is the customers of the range (linked accounts and phone-only POS
// links) and how many came on two or more days; the percentage needs
// insight_min_events_for_percent customers (01 §12).
func (s *Service) repeat(ctx context.Context, q *insightsdb.Queries, storeID ids.ID, from, to time.Time) (insightsapi.RepeatRate, error) {
	rows, err := q.CustomerVisits(ctx, insightsdb.CustomerVisitsParams{StoreID: ids.UUID(storeID), FromAt: from, ToAt: to})
	if err != nil {
		return insightsapi.RepeatRate{}, fmt.Errorf("insights: customer visits: %w", err)
	}
	minEvents, err := s.settings.Int(ctx, settings.InsightMinEventsForPercent)
	if err != nil {
		return insightsapi.RepeatRate{}, fmt.Errorf("insights: %s: %w", settings.InsightMinEventsForPercent, err)
	}
	out := insightsapi.RepeatRate{Customers: len(rows)}
	for _, r := range rows {
		if r.Visits >= 2 {
			out.RepeatCustomers++
		}
	}
	if out.Customers > 0 && int64(out.Customers) >= minEvents {
		p := out.RepeatCustomers * 100 / out.Customers
		out.RepeatPct = &p
	}
	return out, nil
}
