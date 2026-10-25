package facts

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/platform/ids"
)

// item is one store item with what the kinds read.
type item struct {
	id         ids.ID
	productID  ids.ID
	name, size string
	price, mrp int64
	onHand     int64
	reserved   int64
	listed     bool
	updatedAt  *time.Time
	createdAt  time.Time
	firstStock time.Time
}

func (i *item) available() int64 { return i.onHand - i.reserved }

// batch is a stock batch with remaining quantity above 0.
type batch struct {
	id       ids.ID
	item     ids.ID
	qty      int64
	expires  *Day
	received time.Time
	cost     *int64
}

// demand is nearby searches (plus unanswered asks) for a product, or for a
// normalised query when no product matched, on one IST day.
type demand struct {
	day     Day
	product ids.ID
	hasProd bool
	query   string
	n       int64
}

// input is everything the kinds share, loaded once per Compute.
type input struct {
	q       *insightsdb.Queries
	storeID ids.ID
	store   pgtype.UUID
	now     time.Time
	today   Day
	from    Day // first day of the period
	items   []*item
	byID    map[ids.ID]*item
	listed  map[ids.ID]bool // products the store has an item for
	sold    map[ids.ID]int64
	last    map[ids.ID]time.Time
	batches []batch // earliest expiry first, undated last
	demand  []demand
	deltas  map[ids.ID]map[Day]int64
}

func load(ctx context.Context, q *insightsdb.Queries, asks AskDemand, st Settings, storeID ids.ID, now time.Time) (*input, error) {
	today := DayOf(now)
	in := &input{
		q: q, storeID: storeID, store: ids.UUID(storeID), now: now, today: today,
		from:   today.Add(-int(st.PeriodDays)),
		byID:   map[ids.ID]*item{},
		listed: map[ids.ID]bool{},
		sold:   map[ids.ID]int64{},
		last:   map[ids.ID]time.Time{},
		deltas: map[ids.ID]map[Day]int64{},
	}
	rows, err := q.StoreItems(ctx, in.store)
	if err != nil {
		return nil, fmt.Errorf("facts: store items: %w", err)
	}
	for _, r := range rows {
		id, _ := ids.FromUUID(r.ID)
		pid, _ := ids.FromUUID(r.ProductID)
		it := &item{
			id: id, productID: pid, name: r.Name, size: r.Size, price: r.PricePaise, mrp: r.MrpPaise,
			onHand: int64(r.OnHand), reserved: int64(r.Reserved), listed: r.IsListed,
			updatedAt: r.StockUpdatedAt, createdAt: r.CreatedAt, firstStock: r.FirstStockAt,
		}
		in.items = append(in.items, it)
		in.byID[id] = it
		in.listed[pid] = true
	}

	sales, err := q.ItemSales(ctx, insightsdb.ItemSalesParams{StoreID: in.store, FromAt: in.from.Start(), ToAt: today.Start()})
	if err != nil {
		return nil, fmt.Errorf("facts: item sales: %w", err)
	}
	for _, r := range sales {
		id, _ := ids.FromUUID(r.StoreItemID)
		in.sold[id] = r.Units
	}
	last, err := q.LastSales(ctx, insightsdb.LastSalesParams{StoreID: in.store, ToAt: now})
	if err != nil {
		return nil, fmt.Errorf("facts: last sales: %w", err)
	}
	for _, r := range last {
		id, _ := ids.FromUUID(r.StoreItemID)
		in.last[id] = r.LastAt
	}

	bs, err := q.OpenBatches(ctx, in.store)
	if err != nil {
		return nil, fmt.Errorf("facts: batches: %w", err)
	}
	for _, r := range bs {
		id, _ := ids.FromUUID(r.ID)
		itemID, _ := ids.FromUUID(r.StoreItemID)
		b := batch{id: id, item: itemID, qty: int64(r.Quantity), received: r.ReceivedAt, cost: r.CostPaise}
		if r.ExpiresOn.Valid {
			d := DayFromPG(r.ExpiresOn)
			b.expires = &d
		}
		in.batches = append(in.batches, b)
	}

	mv, err := q.MovementDays(ctx, insightsdb.MovementDaysParams{StoreID: in.store, FromAt: in.from.Start()})
	if err != nil {
		return nil, fmt.Errorf("facts: movements: %w", err)
	}
	for _, r := range mv {
		id, _ := ids.FromUUID(r.StoreItemID)
		if in.deltas[id] == nil {
			in.deltas[id] = map[Day]int64{}
		}
		in.deltas[id][DayFromPG(r.Day)] += r.Delta
	}

	dm, err := q.NearbyDemand(ctx, insightsdb.NearbyDemandParams{
		StoreID: in.store, FromDay: in.from.PG(), ToDay: today.PG(), RadiusM: float64(st.RadiusM),
	})
	if err != nil {
		return nil, fmt.Errorf("facts: nearby demand: %w", err)
	}
	for _, r := range dm {
		d := demand{day: DayFromPG(r.Day), query: r.QueryNorm, n: r.Searches}
		d.product, d.hasProd = ids.FromUUID(r.ProductID)
		in.demand = append(in.demand, d)
	}
	ak, err := asks.Asks(ctx, storeID, in.from, today, st.RadiusM)
	if err != nil {
		return nil, fmt.Errorf("facts: asks: %w", err)
	}
	for _, a := range ak {
		in.demand = append(in.demand, demand{day: a.Day, product: a.ProductID, hasProd: a.HasProd, query: a.QueryNorm, n: a.Count})
	}
	return in, nil
}

// latestCosted is the item's latest batch (by received_at) with remaining
// above 0 that has a cost; a batch with no cost is skipped (01 §12).
func (in *input) latestCosted(itemID ids.ID) *batch {
	var out *batch
	for i := range in.batches {
		b := &in.batches[i]
		if b.item != itemID || b.cost == nil {
			continue
		}
		if out == nil || b.received.After(out.received) ||
			(b.received.Equal(out.received) && b.id.Compare(out.id) > 0) {
			out = b
		}
	}
	return out
}

// closingZeroDays counts the period's days whose closing stock was 0 or
// below and returns them. The closing stock of day D is on_hand minus every
// delta after D; a day before the item had any stock never counts, and today
// is never a day of the period.
func (in *input) closingZeroDays(it *item) map[Day]bool {
	out := map[Day]bool{}
	deltas := in.deltas[it.id]
	closing := it.onHand - deltas[in.today] // end of yesterday
	first := DayOf(it.firstStock)
	for d := in.today.Add(-1); !d.Before(in.from); d = d.Add(-1) {
		if d.Before(first) {
			break
		}
		if closing <= 0 {
			out[d] = true
		}
		closing -= deltas[d]
	}
	return out
}

func ceilDiv(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	if a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// restockQty is ceil(sold × cover ÷ period) − available, never below 0 (01 §12).
func restockQty(sold, cover, period, available int64) int64 {
	return max(ceilDiv(sold*cover, period)-available, 0)
}
