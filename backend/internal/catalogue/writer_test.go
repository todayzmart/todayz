package catalogue

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/testkit"
)

// ledger is one test database with a business, two stores and a product,
// and helpers to make items and read the ledger back.
type ledger struct {
	t      *testing.T
	ctx    context.Context
	tdb    *testkit.DB
	d      *db.DB
	clk    *clock.Fake
	w      *Writer
	gen    *ids.Generator
	store  ids.ID
	store2 ids.ID
	prods  int
}

func newLedger(t *testing.T) *ledger {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	l := &ledger{t: t, ctx: context.Background(), tdb: tdb, d: db.New(tdb.Pool), clk: clock.NewFake(t0), gen: ids.NewGenerator()}
	l.w = NewWriter(l.clk)
	biz := l.gen.New(t0)
	l.store, l.store2 = l.gen.New(t0), l.gen.New(t0)
	l.exec(`INSERT INTO businesses (id, brand_name, referral_code) VALUES ($1, 'B', 'ABCDEF')`, ids.UUID(biz))
	l.exec(`INSERT INTO stores (id, business_id, slug, short_code, name) VALUES ($1, $3, 's1', 'ABCDE2', 'S1'), ($2, $3, 's2', 'ABCDE3', 'S2')`,
		ids.UUID(l.store), ids.UUID(l.store2), ids.UUID(biz))
	return l
}

func (l *ledger) exec(sql string, args ...any) {
	l.t.Helper()
	_, err := l.tdb.Pool.Exec(l.ctx, sql, args...)
	require.NoError(l.t, err, sql)
}

func (l *ledger) count(sql string, args ...any) int64 {
	l.t.Helper()
	var n int64
	require.NoError(l.t, l.tdb.Pool.QueryRow(l.ctx, sql, args...).Scan(&n), sql)
	return n
}

// item makes a new item in store with its own product.
func (l *ledger) item(store ids.ID) ids.ID {
	l.t.Helper()
	l.prods++
	p, it := l.gen.New(t0), l.gen.New(t0)
	l.exec(`INSERT INTO products (id, name, category_id, pack_size, unit) VALUES ($1, $2, '0190c47e-0000-7000-8000-000000000101', 1, 'kg')`,
		ids.UUID(p), fmt.Sprintf("Product %d", l.prods))
	l.exec(`INSERT INTO store_items (id, store_id, product_id, price_paise) VALUES ($1, $2, $3, 1000)`, ids.UUID(it), ids.UUID(store), ids.UUID(p))
	return it
}

// tx runs fn in one transaction, as a service does.
func (l *ledger) tx(fn func(ctx context.Context, q db.Querier) error) error {
	return l.d.InTx(l.ctx, func(ctx context.Context) error { return fn(ctx, l.d.Q(ctx)) })
}

type dbBatch struct {
	ID      ids.ID
	Qty     int32
	Recv    int32
	Expires *time.Time
	At      time.Time
}

func (l *ledger) batches(item ids.ID) []dbBatch {
	l.t.Helper()
	rows, err := l.tdb.Pool.Query(l.ctx, `SELECT id, quantity, received_quantity, expires_on, received_at FROM stock_batches WHERE store_item_id = $1 ORDER BY id`, ids.UUID(item))
	require.NoError(l.t, err)
	defer rows.Close()
	var out []dbBatch
	for rows.Next() {
		var id pgtype.UUID
		var b dbBatch
		var e pgtype.Date
		require.NoError(l.t, rows.Scan(&id, &b.Qty, &b.Recv, &e, &b.At))
		b.ID, _ = ids.FromUUID(id)
		if e.Valid {
			x := e.Time
			b.Expires = &x
		}
		out = append(out, b)
	}
	require.NoError(l.t, rows.Err())
	return out
}

func (l *ledger) onHand(item ids.ID) int32 {
	l.t.Helper()
	var n int32
	require.NoError(l.t, l.tdb.Pool.QueryRow(l.ctx, `SELECT on_hand FROM store_items WHERE id = $1`, ids.UUID(item)).Scan(&n))
	return n
}

func asBatches(bs []dbBatch) []batch {
	out := make([]batch, 0, len(bs))
	for _, b := range bs {
		if b.Qty > 0 {
			out = append(out, batch{ID: b.ID, Qty: b.Qty, Received: b.At, Expires: b.Expires})
		}
	}
	return out
}

func allocOf(m Movement) map[ids.ID]int32 {
	out := map[ids.ID]int32{}
	for _, a := range m.Batches {
		out[a.BatchID] += a.Delta
	}
	return out
}

func wantAlloc(ts []take, sign int32) map[ids.ID]int32 {
	out := map[ids.ID]int32{}
	for _, k := range ts {
		if k.Qty != 0 {
			out[k.ID] += sign * k.Qty
		}
	}
	return out
}

// checkLedger asserts the invariants the database can see after every write:
// on_hand = Σ movements (1), every batch = Σ its allocation rows and ≥ 0 (2),
// Σ batch ≤ max(on_hand, 0) (3), and the two expiry lists never show a
// sold-out batch and show exactly the batches with stock in their window (7).
func (l *ledger) checkLedger(where string, item ids.ID) {
	l.t.Helper()
	require.Zero(l.t, l.count(`SELECT count(*) FROM store_items i
		WHERE i.on_hand <> coalesce((SELECT sum(delta) FROM stock_movements m WHERE m.store_item_id = i.id), 0)`), "%s: on_hand ≠ Σ movements", where)
	require.Zero(l.t, l.count(`SELECT count(*) FROM stock_batches b
		WHERE b.quantity < 0 OR b.quantity <> coalesce((SELECT sum(delta) FROM stock_movement_batches a WHERE a.batch_id = b.id), 0)`),
		"%s: a batch ≠ Σ its allocations", where)
	require.Zero(l.t, l.count(`SELECT count(*) FROM store_items i
		WHERE (SELECT coalesce(sum(quantity), 0) FROM stock_batches b WHERE b.store_item_id = i.id) > greatest(i.on_hand, 0)`),
		"%s: Σ batch > max(on_hand, 0)", where)

	today := Today(l.clk.Now())
	until := today.AddDate(0, 0, 7)
	q := cataloguedb.New(l.tdb.Pool)
	exp, err := q.BatchesExpiredOnShelf(l.ctx, cataloguedb.BatchesExpiredOnShelfParams{StoreID: ids.UUID(l.store), Today: pgtype.Date{Time: today, Valid: true}})
	require.NoError(l.t, err)
	waste, err := q.BatchesGoingToWaste(l.ctx, cataloguedb.BatchesGoingToWasteParams{
		StoreID: ids.UUID(l.store), Today: pgtype.Date{Time: today, Valid: true}, Until: pgtype.Date{Time: until, Valid: true},
	})
	require.NoError(l.t, err)
	listedExp, listedWaste := map[ids.ID]bool{}, map[ids.ID]bool{}
	for _, r := range exp {
		id, _ := ids.FromUUID(r.ID)
		require.Positive(l.t, r.Quantity, "%s: expired on shelf lists a sold-out batch", where)
		listedExp[id] = true
	}
	for _, r := range waste {
		id, _ := ids.FromUUID(r.ID)
		require.Positive(l.t, r.Quantity, "%s: going to waste lists a sold-out batch", where)
		listedWaste[id] = true
	}
	for _, b := range l.batches(item) {
		inExp := b.Qty > 0 && b.Expires != nil && b.Expires.Before(today)
		inWaste := b.Qty > 0 && b.Expires != nil && !b.Expires.Before(today) && !b.Expires.After(until)
		require.Equal(l.t, inExp, listedExp[b.ID], "%s: expired on shelf for %s (qty %d)", where, b.ID, b.Qty)
		require.Equal(l.t, inWaste, listedWaste[b.ID], "%s: going to waste for %s (qty %d)", where, b.ID, b.Qty)
	}
}

// TestLedgerProperties drives random sequences of every stock write through
// Writer on a real database and checks the W1-05 row's seven properties
// after each: 1–3 and 7 on the stored rows (checkLedger); 4–6 by checking
// each movement's allocation rows against the D-99 order on the batches as
// they were before it (the pure property test proves that order keeps 4 and
// 5), and a reversal's rows against its sale's.
func TestLedgerProperties(t *testing.T) {
	l := newLedger(t)
	seeds := 30
	for seed := uint64(1); seed <= uint64(seeds); seed++ { //nolint:gosec // small
		r := rand.New(rand.NewPCG(seed, 0xd99))
		item := l.item(l.store)
		var sales []Movement
		for step := 0; step < 40; step++ {
			where := fmt.Sprintf("seed %d step %d", seed, step)
			before := l.batches(item)
			onHand := l.onHand(item)
			now := l.clk.Now()
			today := Today(now)
			var m Movement
			var err error
			op := r.IntN(100)
			switch {
			case op < 25:
				in := StockIn{StoreID: l.store, ItemID: item, Qty: int32(1 + r.IntN(20)), Source: SourceRestock} //nolint:gosec // small
				switch r.IntN(4) {
				case 0, 1:
					e := today.AddDate(0, 0, r.IntN(14)-3)
					in.ExpiresOn = &e
				case 2:
					c := int64(100 + r.IntN(1000))
					in.CostPaise = &c
				}
				err = l.tx(func(ctx context.Context, q db.Querier) error { m, err = l.w.AddStock(ctx, q, in); return err })
				require.NoError(t, err, where)
				if in.ExpiresOn != nil || in.CostPaise != nil {
					room := stockInRoom(in.Qty, int64(onHand)+int64(in.Qty), sumQty(asBatches(before)))
					require.Equal(t, len(before)+1, len(l.batches(item)), "%s: stock in with expiry or cost starts a batch", where)
					if room > 0 {
						require.Len(t, m.Batches, 1, where)
						require.Equal(t, room, m.Batches[0].Delta, where)
					}
				}
			case op < 55:
				in := Sale{StoreID: l.store, ItemID: item, Qty: int32(1 + r.IntN(25)), Source: SourceSalePOS} //nolint:gosec // small
				err = l.tx(func(ctx context.Context, q db.Querier) error { m, err = l.w.Sell(ctx, q, in); return err })
				require.NoError(t, err, where)
				require.Equal(t, wantAlloc(saleTake(asBatches(before), in.Qty, onHand, today), -1), allocOf(m), "%s: sale order", where)
				sales = append(sales, m)
			case op < 65:
				if len(sales) == 0 {
					continue
				}
				src := sales[r.IntN(len(sales))]
				var done int64
				require.NoError(t, l.tdb.Pool.QueryRow(l.ctx, `SELECT coalesce(sum(delta), 0) FROM stock_movements WHERE ref_type = 'stock_movement' AND ref_id = $1`,
					ids.UUID(src.ID)).Scan(&done))
				left := int64(-src.Delta) - done
				qty := int32(1 + r.IntN(int(-src.Delta))) //nolint:gosec // small
				err = l.tx(func(ctx context.Context, q db.Querier) error {
					m, err = l.w.Reverse(ctx, q, Reversal{StoreID: l.store, MovementID: src.ID, Qty: qty})
					return err
				})
				if int64(qty) > left {
					var over *OverError
					require.ErrorAs(t, err, &over, where)
					require.EqualValues(t, max(left, 0), over.Max, where)
					continue
				}
				require.NoError(t, err, where)
				// 6: only the sale's batches, never more than each lost.
				taken := allocOf(src)
				for id, n := range allocOf(m) {
					require.Negative(t, taken[id], "%s: reversal restored %s, which the sale did not take from", where, id)
					var back int64
					require.NoError(t, l.tdb.Pool.QueryRow(l.ctx, `SELECT coalesce(sum(a.delta), 0) FROM stock_movement_batches a
						JOIN stock_movements m ON m.id = a.movement_id WHERE m.ref_type = 'stock_movement' AND m.ref_id = $1 AND a.batch_id = $2`,
						ids.UUID(src.ID), ids.UUID(id)).Scan(&back))
					require.LessOrEqual(t, back, int64(-taken[id]), "%s: reversals put back more than the sale took from %s", where, id)
					require.Positive(t, n, where)
				}
			case op < 72:
				qty := int32(1 + r.IntN(15)) //nolint:gosec // small
				err = l.tx(func(ctx context.Context, q db.Querier) error {
					m, err = l.w.Remove(ctx, q, Removal{StoreID: l.store, ItemID: item, Qty: qty, Source: SourceDamage})
					return err
				})
				require.NoError(t, err, where)
				require.Equal(t, wantAlloc(plainTake(asBatches(before), qty), -1), allocOf(m), "%s: damage order", where)
			case op < 82:
				counted := int32(r.IntN(40)) //nolint:gosec // small
				var ok bool
				err = l.tx(func(ctx context.Context, q db.Querier) error {
					m, ok, err = l.w.Count(ctx, q, Count{StoreID: l.store, ItemID: item, Counted: counted})
					return err
				})
				require.NoError(t, err, where)
				require.Equal(t, counted != onHand, ok, where)
				require.Equal(t, counted, l.onHand(item), where)
				if counted < onHand {
					require.Equal(t, wantAlloc(plainTake(asBatches(before), onHand-counted), -1), allocOf(m), "%s: count down order", where)
				}
			case op < 90:
				var pick []ids.ID
				for _, b := range before {
					if r.IntN(2) == 0 {
						pick = append(pick, b.ID)
					}
				}
				if len(pick) == 0 {
					continue
				}
				var ms []Movement
				err = l.tx(func(ctx context.Context, q db.Querier) error {
					ms, err = l.w.WriteOffBatches(ctx, q, WriteOff{StoreID: l.store, ItemID: item, BatchIDs: pick})
					return err
				})
				if errors.Is(err, ErrNothingToWriteOff) {
					continue
				}
				require.NoError(t, err, where)
				var total int64
				for _, x := range ms {
					require.Equal(t, SourceExpired, x.Source, where)
					require.Len(t, x.Batches, 1, where)
					require.True(t, slices.Contains(pick, x.Batches[0].BatchID), where)
					total -= int64(x.Delta)
				}
				require.LessOrEqual(t, total, max(int64(onHand), 0), "%s: write-off beyond available", where)
			default:
				l.clk.Advance(time.Duration(r.IntN(3))*24*time.Hour + time.Minute)
			}
			l.checkLedger(where, item)
		}
	}
}

func TestWriterRefusals(t *testing.T) {
	l := newLedger(t)
	item, other := l.item(l.store), l.item(l.store2)
	ctx := l.ctx

	_, err := l.w.AddStock(ctx, l.tdb.Pool, StockIn{StoreID: l.store, ItemID: item, Qty: 1, Source: SourceRestock})
	require.ErrorIs(t, err, ErrNotInTx, "the pool is not a transaction")

	run := func(fn func(ctx context.Context, q db.Querier) error) error { return l.tx(fn) }
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.AddStock(ctx, q, StockIn{StoreID: l.store, ItemID: other, Qty: 1, Source: SourceRestock})
		return err
	}), ErrItemNotFound, "another store's item")
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.AddStock(ctx, q, StockIn{StoreID: l.store, ItemID: item, Qty: 1, Source: SourceDamage})
		return err
	}), ErrSource)
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Sell(ctx, q, Sale{StoreID: l.store, ItemID: item, Qty: 0, Source: SourceSalePOS})
		return err
	}), ErrQuantity)
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Sell(ctx, q, Sale{StoreID: l.store, ItemID: item, Qty: maxQuantity + 1, Source: SourceSalePOS})
		return err
	}), ErrQuantity)

	// Replay: the same client id returns the first movement and writes nothing.
	cid := l.gen.New(t0)
	var first, again Movement
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		first, err = l.w.AddStock(ctx, q, StockIn{StoreID: l.store, ItemID: item, Qty: 5, Source: SourceRestock, ClientID: cid})
		return err
	}))
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		again, err = l.w.AddStock(ctx, q, StockIn{StoreID: l.store, ItemID: item, Qty: 5, Source: SourceRestock, ClientID: cid})
		return err
	}))
	require.True(t, again.Replayed)
	require.Equal(t, first.ID, again.ID)
	require.EqualValues(t, 5, l.onHand(item))
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Sell(ctx, q, Sale{StoreID: l.store, ItemID: item, Qty: 1, Source: SourceSalePOS, ClientID: cid})
		return err
	}), ErrClientIDReused, "one client id, another kind of write")

	// Reversal: only a sale, only in its store, never beyond what it took.
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Reverse(ctx, q, Reversal{StoreID: l.store, MovementID: first.ID, Qty: 1})
		return err
	}), ErrNotReversible)
	var sale Movement
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		sale, err = l.w.Sell(ctx, q, Sale{StoreID: l.store, ItemID: item, Qty: 3, Source: saleSource(true)})
		return err
	}))
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Reverse(ctx, q, Reversal{StoreID: l.store2, MovementID: sale.ID, Qty: 1})
		return err
	}), ErrMovementNotFound, "another store's sale")
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Reverse(ctx, q, Reversal{StoreID: l.store, MovementID: sale.ID, Qty: 2})
		return err
	}))
	var over *OverError
	require.ErrorAs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Reverse(ctx, q, Reversal{StoreID: l.store, MovementID: sale.ID, Qty: 2})
		return err
	}), &over)
	require.EqualValues(t, 1, over.Max)

	// Damage capped at available (on_hand − reserved) when the form asks.
	l.exec(`UPDATE store_items SET reserved = 3 WHERE id = $1`, ids.UUID(item)) // on hand 4
	require.ErrorAs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Remove(ctx, q, Removal{StoreID: l.store, ItemID: item, Qty: 2, Source: SourceDamage, CapAvailable: true})
		return err
	}), &over)
	require.EqualValues(t, 1, over.Max)
	l.exec(`UPDATE store_items SET reserved = 0 WHERE id = $1`, ids.UUID(item))

	// Write-off: a batch of another item is refused; an empty one has nothing.
	e := Today(t0).AddDate(0, 0, -1)
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.AddStock(ctx, q, StockIn{StoreID: l.store2, ItemID: other, Qty: 2, Source: SourceRestock, ExpiresOn: &e})
		return err
	}))
	otherBatch := l.batches(other)[0].ID
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.WriteOffBatches(ctx, q, WriteOff{StoreID: l.store, ItemID: item, BatchIDs: []ids.ID{otherBatch}})
		return err
	}), ErrBatchNotFound)
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.WriteOffBatches(ctx, q, WriteOff{StoreID: l.store2, ItemID: other, BatchIDs: []ids.ID{otherBatch}})
		return err
	}))
	require.ErrorIs(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.WriteOffBatches(ctx, q, WriteOff{StoreID: l.store2, ItemID: other, BatchIDs: []ids.ID{otherBatch}})
		return err
	}), ErrNothingToWriteOff)
	require.Zero(t, l.onHand(other))

	// A failed write leaves nothing behind (the caller's transaction rolls back).
	l.checkLedger("refusals", item)
}

// saleSource picks a sale source, so both sale sources get written.
func saleSource(online bool) string {
	if online {
		return SourceSaleOnline
	}
	return SourceSalePOS
}

func TestWriterFreshnessAndNegativeStock(t *testing.T) {
	l := newLedger(t)
	item := l.item(l.store)
	run := l.tx
	at := func() time.Time {
		var ts time.Time
		require.NoError(t, l.tdb.Pool.QueryRow(l.ctx, `SELECT stock_updated_at FROM store_items WHERE id = $1`, ids.UUID(item)).Scan(&ts))
		return ts
	}

	// An offline sale drives on hand below 0; a later stock in with a cost
	// starts a batch that holds only what the batches may take.
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.Sell(ctx, q, Sale{StoreID: l.store, ItemID: item, Qty: 6, Source: SourceSalePOS})
		return err
	}))
	require.EqualValues(t, -6, l.onHand(item))
	cost := int64(1200)
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.AddStock(ctx, q, StockIn{StoreID: l.store, ItemID: item, Qty: 10, Source: SourceSupplierBill, CostPaise: &cost})
		return err
	}))
	bs := l.batches(item)
	require.Len(t, bs, 1)
	require.EqualValues(t, 10, bs[0].Recv)
	require.EqualValues(t, 4, bs[0].Qty, "max(4, 0) − 0")

	// A count equal to on hand writes nothing and confirms the stock.
	l.clk.Advance(time.Hour)
	var ok bool
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		var err error
		_, ok, err = l.w.Count(ctx, q, Count{StoreID: l.store, ItemID: item, Counted: 4})
		return err
	}))
	require.False(t, ok)
	require.True(t, at().Equal(t0.Add(time.Hour)))
	// A count up starts a found-in-count batch, undated.
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		var err error
		_, ok, err = l.w.Count(ctx, q, Count{StoreID: l.store, ItemID: item, Counted: 7})
		return err
	}))
	require.True(t, ok)
	require.EqualValues(t, 1, l.count(`SELECT count(*) FROM stock_batches WHERE store_item_id = $1 AND found_in_count AND quantity = 3`, ids.UUID(item)))

	// A write stamped earlier (a backdated writer) never moves freshness back.
	back := NewWriter(clock.NewFake(t0.Add(-48 * time.Hour)))
	require.NoError(t, run(func(ctx context.Context, q db.Querier) error {
		_, err := back.Remove(ctx, q, Removal{StoreID: l.store, ItemID: item, Qty: 1, Source: SourceDamage})
		return err
	}))
	require.True(t, at().Equal(t0.Add(time.Hour)))
	l.checkLedger("freshness", item)

	// Every write took a catalogue seq for the item (the delta feed's rows).
	require.EqualValues(t, l.count(`SELECT count(*) FROM stock_movements WHERE store_item_id = $1`, ids.UUID(item))+1,
		l.count(`SELECT count(*) FROM catalogue_changes WHERE entity_id = $1`, ids.UUID(item)), "one change per movement plus the confirming count")
}

func TestReconcile(t *testing.T) {
	l := newLedger(t)
	item := l.item(l.store)
	e := Today(t0).AddDate(0, 0, 5)
	require.NoError(t, l.tx(func(ctx context.Context, q db.Querier) error {
		_, err := l.w.AddStock(ctx, q, StockIn{StoreID: l.store, ItemID: item, Qty: 8, Source: SourceRestock, ExpiresOn: &e})
		return err
	}))

	res, err := Reconcile(l.ctx, l.d, l.clk)
	require.NoError(t, err)
	require.Equal(t, ReconcileResult{}, res, "nothing to repair")

	// Drift only a person with the owner role could cause.
	_, err = l.tdb.OwnerPool.Exec(l.ctx, `UPDATE store_items SET on_hand = 99 WHERE id = $1`, ids.UUID(item))
	require.NoError(t, err)
	_, err = l.tdb.OwnerPool.Exec(l.ctx, `UPDATE stock_batches SET quantity = 6 WHERE store_item_id = $1`, ids.UUID(item))
	require.NoError(t, err)
	res, err = Reconcile(l.ctx, l.d, l.clk)
	require.NoError(t, err)
	require.Equal(t, ReconcileResult{OnHandRepaired: 1, BatchesRepaired: 1}, res)
	require.EqualValues(t, 8, l.onHand(item))
	require.EqualValues(t, 8, l.batches(item)[0].Qty)
	require.EqualValues(t, 2, l.count(`SELECT count(*) FROM audit_log WHERE action = 'catalogue.reconcile_repair' AND actor_kind = 'system'`))

	// Batches holding more than the stock are reported, not trimmed.
	_, err = l.tdb.OwnerPool.Exec(l.ctx, `INSERT INTO stock_movements (id, store_id, store_item_id, delta, source, created_at)
		VALUES ($1, $2, $3, -5, 'damage', $4)`, ids.UUID(l.gen.New(t0)), ids.UUID(l.store), ids.UUID(item), t0)
	require.NoError(t, err)
	res, err = Reconcile(l.ctx, l.d, l.clk)
	require.NoError(t, err)
	require.Equal(t, ReconcileResult{BatchOver: 1}, res)
	require.EqualValues(t, 8, l.batches(item)[0].Qty)
}
