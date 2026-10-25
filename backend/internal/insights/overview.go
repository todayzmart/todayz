package insights

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	insightsapi "github.com/todayz/backend/gen/api/insights"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

const (
	// newOrdersShown is the B-03 new orders card.
	newOrdersShown = 5 //settings:ok 06 B-03 shows at most 5 waiting orders
	// topItemsShown is the B-03 top items table.
	topItemsShown = 10 //settings:ok 06 B-03 top items table size
	// setupProducts is the "50 products" step of the setup checklist (06 B-03).
	setupProducts = 50 //settings:ok 06 B-03 fixes the setup step at 50 products
	hoursPerDay   = 24
	lastDays      = 7 // the top items' sparkline
)

// span is an overview range: [from, to) against [prevFrom, prevTo).
type span struct {
	from, to, prevFrom, prevTo time.Time
}

func overviewSpan(r insightsapi.OverviewRange, now time.Time) span {
	today := facts.DayOf(now)
	days, back := 1, lastDays // today against the same weekday last week
	switch r {
	case insightsapi.OverviewRangeN7d:
		days, back = 7, 7
	case insightsapi.OverviewRangeN30d:
		days, back = 30, 30
	}
	from := today.Add(-(days - 1)).Start()
	return span{from: from, to: now, prevFrom: from.AddDate(0, 0, -back), prevTo: now.AddDate(0, 0, -back)}
}

// Overview is B-03 for the store at now.
func (s *Service) Overview(ctx context.Context, userID, storeID ids.ID, r insightsapi.OverviewRange) (insightsapi.StoreOverview, error) {
	if err := s.ready(ctx); err != nil {
		return insightsapi.StoreOverview{}, err
	}
	now := s.now()
	q := s.q(ctx)
	store := ids.UUID(storeID)
	head, err := q.StoreHeader(ctx, store)
	if errors.Is(err, pgx.ErrNoRows) {
		return insightsapi.StoreOverview{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return insightsapi.StoreOverview{}, fmt.Errorf("insights: store: %w", err)
	}
	owner, err := q.UserFirstName(ctx, ids.UUID(userID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return insightsapi.StoreOverview{}, fmt.Errorf("insights: user: %w", err)
	}
	sp := overviewSpan(r, now)
	out := insightsapi.StoreOverview{
		Range: r, StoreName: head.StoreName, OwnerName: firstName(owner), ServerNow: now,
		NewOrders: []insightsapi.OverviewOrder{}, SalesByHour: []insightsapi.HourSales{}, TopItems: []insightsapi.OverviewTopItem{},
	}
	if out.Kpis, err = s.kpis(ctx, q, storeID, sp); err != nil {
		return insightsapi.StoreOverview{}, err
	}
	if out.NewOrders, err = newOrders(ctx, q, store); err != nil {
		return insightsapi.StoreOverview{}, err
	}
	if out.SalesByHour, err = salesByHour(ctx, q, store, now); err != nil {
		return insightsapi.StoreOverview{}, err
	}

	items, err := q.StoreItems(ctx, store)
	if err != nil {
		return insightsapi.StoreOverview{}, fmt.Errorf("insights: items: %w", err)
	}
	lowDefault, err := s.settings.Int(ctx, settings.LowStockDefault)
	if err != nil {
		return insightsapi.StoreOverview{}, fmt.Errorf("insights: %s: %w", settings.LowStockDefault, err)
	}
	byID := make(map[ids.ID]insightsdb.StoreItemsRow, len(items))
	for _, it := range items {
		id, _ := ids.FromUUID(it.ID)
		byID[id] = it
		if it.IsListed {
			out.ListedItems++
			if isLow(it, lowDefault) {
				out.Kpis.LowItems++
			}
		}
	}
	// Sell out soon is the high selling-fast rows (stock for 2 days or less, 01 §12).
	res, err := facts.Compute(ctx, s.source(ctx), storeID, now, nil)
	if err != nil {
		return insightsapi.StoreOverview{}, err
	}
	for _, row := range res.Rows {
		if row.Kind == facts.SellingFast && row.Severity == facts.High {
			out.Kpis.SellOutSoon++
		}
	}
	if out.TopItems, out.DistinctItemsSold, err = topItems(ctx, q, store, sp, now, byID, lowDefault); err != nil {
		return insightsapi.StoreOverview{}, err
	}
	if out.Setup, err = setup(ctx, q, store); err != nil {
		return insightsapi.StoreOverview{}, err
	}
	return out, nil
}

func isLow(it insightsdb.StoreItemsRow, lowDefault int64) bool {
	at := lowDefault
	if it.LowStockAt != nil {
		at = int64(*it.LowStockAt)
	}
	return int64(it.OnHand-it.Reserved) <= at
}

func (s *Service) kpis(ctx context.Context, q *insightsdb.Queries, storeID ids.ID, sp span) (insightsapi.OverviewKpis, error) {
	store := ids.UUID(storeID)
	cur, err := q.SalesSummary(ctx, insightsdb.SalesSummaryParams{StoreID: store, FromAt: sp.from, ToAt: sp.to})
	if err != nil {
		return insightsapi.OverviewKpis{}, fmt.Errorf("insights: sales: %w", err)
	}
	prev, err := q.SalesSummary(ctx, insightsdb.SalesSummaryParams{StoreID: store, FromAt: sp.prevFrom, ToAt: sp.prevTo})
	if err != nil {
		return insightsapi.OverviewKpis{}, fmt.Errorf("insights: sales before: %w", err)
	}
	waiting, err := q.WaitingOrders(ctx, store)
	if err != nil {
		return insightsapi.OverviewKpis{}, fmt.Errorf("insights: waiting orders: %w", err)
	}
	return insightsapi.OverviewKpis{
		SalesPaise: int(cur.SalesPaise), SalesPrevPaise: int(prev.SalesPaise),
		Bills: int(cur.Bills), OnlineOrders: int(cur.OnlineBills),
		Orders: int(cur.Orders), OrdersPrev: int(prev.Orders), WaitingOrders: int(waiting),
		AvgBillPaise: avg(cur.SalesPaise, cur.Bills), AvgBillPrevPaise: avg(prev.SalesPaise, prev.Bills),
	}, nil
}

func avg(paise int64, bills int32) int {
	if bills <= 0 {
		return 0
	}
	return int(paise / int64(bills))
}

func newOrders(ctx context.Context, q *insightsdb.Queries, store pgtype.UUID) ([]insightsapi.OverviewOrder, error) {
	rows, err := q.NewOrders(ctx, insightsdb.NewOrdersParams{StoreID: store, Lim: newOrdersShown})
	if err != nil {
		return nil, fmt.Errorf("insights: new orders: %w", err)
	}
	out := make([]insightsapi.OverviewOrder, 0, len(rows))
	for _, o := range rows {
		id, _ := ids.FromUUID(o.ID)
		out = append(out, insightsapi.OverviewOrder{
			Id: id.String(), Code: o.DisplayNumber, Customer: shortName(o.CustomerName), Summary: o.Summary,
			TotalPaise: int(o.TotalPaise), Fulfilment: insightsapi.OverviewOrderFulfilment(o.Fulfilment), AcceptBy: o.AcceptBy,
		})
	}
	return out, nil
}

// salesByHour is today's sales per IST hour against the same weekday last
// week; hours after now have no today value.
func salesByHour(ctx context.Context, q *insightsdb.Queries, store pgtype.UUID, now time.Time) ([]insightsapi.HourSales, error) {
	today := facts.DayOf(now)
	cur, err := q.SalesByHour(ctx, insightsdb.SalesByHourParams{StoreID: store, FromAt: today.Start(), ToAt: now})
	if err != nil {
		return nil, fmt.Errorf("insights: sales by hour: %w", err)
	}
	last := today.Add(-lastDays)
	prev, err := q.SalesByHour(ctx, insightsdb.SalesByHourParams{StoreID: store, FromAt: last.Start(), ToAt: last.End()})
	if err != nil {
		return nil, fmt.Errorf("insights: sales by hour before: %w", err)
	}
	var c, p [hoursPerDay]int
	for _, r := range cur {
		c[r.Hour] = int(r.Paise)
	}
	for _, r := range prev {
		p[r.Hour] = int(r.Paise)
	}
	nowHour := int(now.Sub(today.Start()) / time.Hour)
	out := make([]insightsapi.HourSales, hoursPerDay)
	for h := range hoursPerDay {
		out[h] = insightsapi.HourSales{Hour: h, PrevPaise: p[h]}
		if h <= nowHour {
			v := c[h]
			out[h].TodayPaise = &v
		}
	}
	return out, nil
}

func topItems(ctx context.Context, q *insightsdb.Queries, store pgtype.UUID, sp span, now time.Time,
	byID map[ids.ID]insightsdb.StoreItemsRow, lowDefault int64) ([]insightsapi.OverviewTopItem, int, error) {
	sales, err := q.ItemSales(ctx, insightsdb.ItemSalesParams{StoreID: store, FromAt: sp.from, ToAt: sp.to})
	if err != nil {
		return nil, 0, fmt.Errorf("insights: item sales: %w", err)
	}
	distinct := 0
	for _, r := range sales {
		if r.Units > 0 {
			distinct++
		}
	}
	sort.SliceStable(sales, func(i, j int) bool {
		if sales[i].Units != sales[j].Units {
			return sales[i].Units > sales[j].Units
		}
		return sales[i].RevenuePaise > sales[j].RevenuePaise
	})
	type top struct {
		id  ids.ID
		row insightsdb.ItemSalesRow
		it  insightsdb.StoreItemsRow
	}
	var tops []top
	var uuids []pgtype.UUID
	for _, r := range sales {
		if len(tops) == topItemsShown {
			break
		}
		id, _ := ids.FromUUID(r.StoreItemID)
		it, ok := byID[id]
		if !ok || r.Units <= 0 {
			continue
		}
		tops = append(tops, top{id: id, row: r, it: it})
		uuids = append(uuids, r.StoreItemID)
	}
	out := make([]insightsapi.OverviewTopItem, 0, len(tops))
	if len(tops) == 0 {
		return out, distinct, nil
	}
	today := facts.DayOf(now)
	first := today.Add(-(lastDays - 1))
	daily, err := q.ItemDailyUnits(ctx, insightsdb.ItemDailyUnitsParams{StoreID: store, FromAt: first.Start(), ToAt: now})
	if err != nil {
		return nil, 0, fmt.Errorf("insights: daily units: %w", err)
	}
	spark := map[ids.ID][]int{}
	for _, d := range daily {
		id, _ := ids.FromUUID(d.StoreItemID)
		i := facts.DayFromPG(d.Day).Sub(first)
		if i < 0 || i >= lastDays {
			continue
		}
		if spark[id] == nil {
			spark[id] = make([]int, lastDays)
		}
		spark[id][i] = int(d.Units)
	}
	changes, err := q.LastStockChanges(ctx, uuids)
	if err != nil {
		return nil, 0, fmt.Errorf("insights: stock changes: %w", err)
	}
	last := map[ids.ID]*insightsapi.StockChange{}
	for _, c := range changes {
		id, _ := ids.FromUUID(c.StoreItemID)
		last[id] = &insightsapi.StockChange{Source: c.Source, At: c.CreatedAt}
	}
	for _, t := range tops {
		l7 := spark[t.id]
		if l7 == nil {
			l7 = make([]int, lastDays)
		}
		out = append(out, insightsapi.OverviewTopItem{
			StoreItemId: t.id.String(), Name: t.it.Name, Size: t.it.Size, Sold: int(t.row.Units),
			Last7: l7, RevenuePaise: int(t.row.RevenuePaise), InStock: int(t.it.OnHand),
			Low: isLow(t.it, lowDefault), LastChange: last[t.id],
		})
	}
	return out, distinct, nil
}

// setup is the B-03 setup checklist: KYC verified, 50 products listed, a
// staff member besides the owner, a QR standee and the opening hours.
func setup(ctx context.Context, q *insightsdb.Queries, store pgtype.UUID) (insightsapi.SetupProgress, error) {
	f, err := q.SetupFacts(ctx, store)
	if err != nil {
		return insightsapi.SetupProgress{}, fmt.Errorf("insights: setup: %w", err)
	}
	st := insightsapi.SetupSteps{
		Kyc: f.BusinessStatus == "verified", Products: f.Listed >= setupProducts, Staff: f.Staff > 0,
		Standee: f.HasStandee, OpeningHours: f.HasHours,
	}
	steps := []struct {
		done bool
		name insightsapi.SetupProgressNext
	}{
		{st.Kyc, insightsapi.Kyc}, {st.Products, insightsapi.Products}, {st.Staff, insightsapi.Staff},
		{st.Standee, insightsapi.Standee}, {st.OpeningHours, insightsapi.OpeningHours},
	}
	out := insightsapi.SetupProgress{Steps: st}
	done := 0
	for _, x := range steps {
		if x.done {
			done++
		} else if out.Next == nil {
			n := x.name
			out.Next = &n
		}
	}
	out.Percent = done * 100 / len(steps)
	return out, nil
}
