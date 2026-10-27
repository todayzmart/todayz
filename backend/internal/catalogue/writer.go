package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

// The stock writer: the only code that inserts stock_movements and their
// batch allocation (D-99). Other domains (orders, POS sync, returns) call it
// with their own transaction's querier, so the movement, its allocation and
// their own rows commit together.
//
// Lock order, the same for every write: the store's catalogue sequence (the
// stores row, through RecordCatalogueChange), then the item (FOR UPDATE),
// then the item's batches by id.

// Movement sources (ck_stock_movements_source).
const (
	SourceOpening         = "opening"
	SourceRestock         = "restock"
	SourceSupplierBill    = "supplier_bill"
	SourceImport          = "import"
	SourceCountCorrection = "count_correction"
	SourceDamage          = "damage"
	SourceExpired         = "expired"
	SourceSalePOS         = "sale_pos"
	SourceSaleOnline      = "sale_online"
	SourcePOSReversal     = "pos_reversal"
)

// Ref types the writer itself sets.
const (
	RefStockMovement = "stock_movement"
	RefStockBatch    = "stock_batch"
)

// maxQuantity bounds one write (the contract's maximum) so sums stay in int32.
const maxQuantity = 1_000_000

var (
	// ErrNotInTx: the writer was given the pool, not a transaction.
	ErrNotInTx = errors.New("catalogue: the stock writer needs a transaction")
	// ErrItemNotFound: no such item in the store.
	ErrItemNotFound = errors.New("catalogue: no such item in the store")
	// ErrMovementNotFound: the movement to reverse is not in the store.
	ErrMovementNotFound = errors.New("catalogue: no such movement in the store")
	// ErrNotReversible: only sale_pos and sale_online movements are reversed.
	ErrNotReversible = errors.New("catalogue: only a sale can be reversed")
	// ErrBatchNotFound: a named batch is not one of the item's.
	ErrBatchNotFound = errors.New("catalogue: no such batch for the item")
	// ErrNothingToWriteOff: the named batches are empty or nothing is available.
	ErrNothingToWriteOff = errors.New("catalogue: nothing left to write off")
	// ErrClientIDReused: a client id already names a different movement.
	ErrClientIDReused = errors.New("catalogue: client id already used for another movement")
	// ErrQuantity: a quantity outside 1..1,000,000 (0..1,000,000 for a count).
	ErrQuantity = errors.New("catalogue: quantity out of range")
	// ErrSource: a source this method does not write.
	ErrSource = errors.New("catalogue: wrong source for this write")
)

// OverError is a quantity above what the write may take: available stock
// for damage, what is left to put back for a reversal.
type OverError struct{ Max int32 }

func (e *OverError) Error() string { return fmt.Sprintf("catalogue: quantity above %d", e.Max) }

// capAt is *OverError when qty is above limit (never below 0 in Max).
func capAt(qty int32, limit int64) error {
	if int64(qty) > limit {
		return &OverError{Max: i32(max(limit, 0))} // limit < qty ≤ 1e6
	}
	return nil
}

// Allocation is one batch's share of a movement.
type Allocation struct {
	BatchID ids.ID
	Delta   int32
}

// Movement is one ledger line as written (or found again on a replay).
type Movement struct {
	ID        ids.ID
	StoreID   ids.ID
	ItemID    ids.ID
	Delta     int32
	Source    string
	RefType   *string
	RefID     *ids.ID
	Note      *string
	CreatedAt time.Time
	Batches   []Allocation
	// Replayed is true when ClientID matched a movement already written.
	Replayed bool
}

// Who is who wrote it: the signed-in user and the device, both optional.
type Who struct {
	UserID   ids.ID
	DeviceID ids.ID
}

// Writer writes stock movements.
type Writer struct {
	clock clock.Clock
	newID func(time.Time) ids.ID
}

// NewWriter returns a writer on c.
func NewWriter(c clock.Clock) *Writer { return &Writer{clock: c, newID: ids.NewGenerator().New} }

// NewWriterWithIDs returns a writer on c that takes movement and batch ids
// from next: the seed passes a deterministic source so seeded rows are the
// same on every run. Services use NewWriter.
func NewWriterWithIDs(c clock.Clock, next func(time.Time) ids.ID) *Writer {
	return &Writer{clock: c, newID: next}
}

// Sale is a sale of Qty units (sale_pos or sale_online). A sale is never
// refused for stock (03 stock rule 1): on_hand may go below 0.
type Sale struct {
	StoreID, ItemID ids.ID
	Qty             int32
	Source          string
	RefType         string // e.g. "order", "pos_sale"; empty for none
	RefID           ids.ID
	ClientID        ids.ID // the offline line id; zero for none
	Note            *string
	Who             Who
	// CapAvailable refuses more than available (on_hand − reserved) with
	// *OverError; the online counter sale sets it (D-270). A synced offline
	// sale does not: it already happened.
	CapAvailable bool
}

// Sell writes a sale and takes it from the batches in rule 8's sale order.
func (w *Writer) Sell(ctx context.Context, q db.Querier, in Sale) (Movement, error) {
	if in.Source != SourceSalePOS && in.Source != SourceSaleOnline {
		return Movement{}, ErrSource
	}
	if in.Qty <= 0 || in.Qty > maxQuantity {
		return Movement{}, ErrQuantity
	}
	return w.one(ctx, q, in.StoreID, in.ItemID, in.Source, in.ClientID, func(t *txn) (Movement, error) {
		if in.CapAvailable {
			if err := capAt(in.Qty, int64(t.item.OnHand)-int64(t.item.Reserved)); err != nil {
				return Movement{}, err
			}
		}
		bs, err := t.batches()
		if err != nil {
			return Movement{}, err
		}
		takes := saleTake(bs, in.Qty, t.item.OnHand, Today(t.now))
		return t.write(-in.Qty, in.Source, ref(in.RefType, in.RefID), in.ClientID, in.Note, in.Who, negate(takes))
	})
}

// Reversal puts back Qty units of a sale (pos_reversal, rule 5).
type Reversal struct {
	StoreID    ids.ID
	MovementID ids.ID // the sale being reversed
	Qty        int32
	ClientID   ids.ID
	Note       *string
	Who        Who
}

// Reverse writes a pos_reversal against a sale: at most what the sale took
// less what earlier reversals put back, into the batches the sale took from.
func (w *Writer) Reverse(ctx context.Context, q db.Querier, in Reversal) (Movement, error) {
	if in.Qty <= 0 || in.Qty > maxQuantity {
		return Movement{}, ErrQuantity
	}
	if _, ok := q.(pgx.Tx); !ok {
		return Movement{}, ErrNotInTx
	}
	src, err := cataloguedb.New(q).GetMovement(ctx, cataloguedb.GetMovementParams{ID: ids.UUID(in.MovementID), StoreID: ids.UUID(in.StoreID)})
	if db.IsNoRows(err) {
		return Movement{}, ErrMovementNotFound
	}
	if err != nil {
		return Movement{}, fmt.Errorf("catalogue: reverse: source: %w", err)
	}
	if src.Source != SourceSalePOS && src.Source != SourceSaleOnline {
		return Movement{}, ErrNotReversible
	}
	itemID, _ := ids.FromUUID(src.StoreItemID)
	m, err := w.one(ctx, q, in.StoreID, itemID, SourcePOSReversal, in.ClientID, func(t *txn) (Movement, error) {
		done, err := t.q.ReversedFrom(ctx, src.ID)
		if err != nil {
			return Movement{}, fmt.Errorf("catalogue: reverse: reversed: %w", err)
		}
		left := int64(-src.Delta) - done.Total
		if int64(in.Qty) > left {
			return Movement{}, &OverError{Max: i32(max(left, 0))} // left ≤ |delta|
		}
		restored, err := perBatch(done.PerBatch)
		if err != nil {
			return Movement{}, err
		}
		alloc, err := t.q.MovementAllocations(ctx, src.ID)
		if err != nil {
			return Movement{}, fmt.Errorf("catalogue: reverse: allocation: %w", err)
		}
		taken := map[ids.ID]int32{}
		var srcIDs []pgtype.UUID
		for _, a := range alloc {
			id, _ := ids.FromUUID(a.BatchID)
			taken[id] = -a.Delta
			srcIDs = append(srcIDs, a.BatchID)
		}
		bs, err := t.batches()
		if err != nil {
			return Movement{}, err
		}
		var order []ids.ID
		if len(srcIDs) > 0 {
			rows, err := t.q.LockBatchesByID(ctx, cataloguedb.LockBatchesByIDParams{StoreItemID: ids.UUID(itemID), Ids: srcIDs})
			if err != nil {
				return Movement{}, fmt.Errorf("catalogue: reverse: lock batches: %w", err)
			}
			src := make([]batch, 0, len(rows))
			for _, r := range rows {
				src = append(src, toBatch(r.ID, r.Quantity, r.ReceivedAt, r.ExpiresOn))
			}
			order = restoreOrder(src, Today(t.now))
		}
		room := max(int64(t.item.OnHand)+int64(in.Qty), 0) - sumQty(bs)
		puts := reverseRestore(order, taken, restored, in.Qty, room)
		return t.write(in.Qty, SourcePOSReversal, ref(RefStockMovement, in.MovementID), in.ClientID, in.Note, in.Who, puts)
	})
	if err == nil && m.Replayed && (m.RefID == nil || *m.RefID != in.MovementID) {
		return Movement{}, ErrClientIDReused
	}
	return m, err
}

// StockIn is stock received: opening, restock, supplier_bill or import. With
// ExpiresOn or CostPaise it starts a batch (rule 8).
type StockIn struct {
	StoreID, ItemID ids.ID
	Qty             int32
	Source          string
	ExpiresOn       *time.Time // IST date as midnight UTC
	CostPaise       *int64
	RefType         string
	RefID           ids.ID
	ClientID        ids.ID
	Note            *string
	Who             Who
}

// AddStock writes a stock in. The new batch receives Qty and holds what the
// batches may still take (all of it unless on_hand was below 0); it is
// created even when that is 0, so its cost is kept.
func (w *Writer) AddStock(ctx context.Context, q db.Querier, in StockIn) (Movement, error) {
	switch in.Source {
	case SourceOpening, SourceRestock, SourceSupplierBill, SourceImport:
	default:
		return Movement{}, ErrSource
	}
	if in.Qty <= 0 || in.Qty > maxQuantity {
		return Movement{}, ErrQuantity
	}
	return w.one(ctx, q, in.StoreID, in.ItemID, in.Source, in.ClientID, func(t *txn) (Movement, error) {
		var nb *newBatch
		if in.ExpiresOn != nil || in.CostPaise != nil {
			bs, err := t.batches()
			if err != nil {
				return Movement{}, err
			}
			nb = &newBatch{
				received: in.Qty,
				alloc:    stockInRoom(in.Qty, int64(t.item.OnHand)+int64(in.Qty), sumQty(bs)),
				expires:  in.ExpiresOn,
				cost:     in.CostPaise,
			}
		}
		return t.writeIn(in.Qty, in.Source, ref(in.RefType, in.RefID), in.ClientID, in.Note, in.Who, nb)
	})
}

// Removal is stock lost: damage, or expired without naming batches.
type Removal struct {
	StoreID, ItemID ids.ID
	Qty             int32
	Source          string
	// CapAvailable refuses more than available (on_hand − reserved) with
	// *OverError; the business app's damage and expired forms set it.
	CapAvailable bool
	// CapOnHand refuses more than on_hand with *OverError; a synced offline
	// damage or expired entry sets it (D-270).
	CapOnHand bool
	ClientID  ids.ID
	Note      *string
	Who       Who
}

// Remove writes damage or expired stock, taken in rule 8's plain order.
func (w *Writer) Remove(ctx context.Context, q db.Querier, in Removal) (Movement, error) {
	if in.Source != SourceDamage && in.Source != SourceExpired {
		return Movement{}, ErrSource
	}
	if in.Qty <= 0 || in.Qty > maxQuantity {
		return Movement{}, ErrQuantity
	}
	return w.one(ctx, q, in.StoreID, in.ItemID, in.Source, in.ClientID, func(t *txn) (Movement, error) {
		switch {
		case in.CapAvailable:
			if err := capAt(in.Qty, int64(t.item.OnHand)-int64(t.item.Reserved)); err != nil {
				return Movement{}, err
			}
		case in.CapOnHand:
			if err := capAt(in.Qty, int64(t.item.OnHand)); err != nil {
				return Movement{}, err
			}
		}
		bs, err := t.batches()
		if err != nil {
			return Movement{}, err
		}
		return t.write(-in.Qty, in.Source, nil, in.ClientID, in.Note, in.Who, negate(plainTake(bs, in.Qty)))
	})
}

// Count is a shelf count: Counted units are there now.
type Count struct {
	StoreID, ItemID ids.ID
	Counted         int32
	ClientID        ids.ID
	Note            *string
	Who             Who
}

// Count writes the difference from on hand as a count_correction. A count
// equal to on hand writes nothing and confirms the stock (01 §4); a count up
// starts a found-in-count batch with what the batches may take; a count down
// takes in the plain order. ok is false when nothing was written.
func (w *Writer) Count(ctx context.Context, q db.Querier, in Count) (m Movement, ok bool, err error) {
	if in.Counted < 0 || in.Counted > maxQuantity {
		return Movement{}, false, ErrQuantity
	}
	wrote := true
	m, err = w.one(ctx, q, in.StoreID, in.ItemID, SourceCountCorrection, in.ClientID, func(t *txn) (Movement, error) {
		delta := int64(in.Counted) - int64(t.item.OnHand)
		if delta == 0 {
			wrote = false
			if err := t.q.ConfirmStock(ctx, cataloguedb.ConfirmStockParams{Now: t.now, ID: t.item.ID}); err != nil {
				return Movement{}, fmt.Errorf("catalogue: confirm stock: %w", err)
			}
			return Movement{}, nil
		}
		if delta < -maxQuantity*2 || delta > maxQuantity*2 {
			// on_hand is far outside anything a count could mean.
			return Movement{}, ErrQuantity
		}
		d := int32(delta)
		bs, err := t.batches()
		if err != nil {
			return Movement{}, err
		}
		if d < 0 {
			return t.write(d, SourceCountCorrection, nil, in.ClientID, in.Note, in.Who, negate(plainTake(bs, -d)))
		}
		var nb *newBatch
		if room := stockInRoom(d, int64(in.Counted), sumQty(bs)); room > 0 {
			nb = &newBatch{received: room, alloc: room, foundInCount: true}
		}
		return t.writeIn(d, SourceCountCorrection, nil, in.ClientID, in.Note, in.Who, nb)
	})
	return m, wrote && err == nil, err
}

// WriteOff is "mark expired" (03 stock rule 6): the named batches of one item.
type WriteOff struct {
	StoreID, ItemID ids.ID
	BatchIDs        []ids.ID
	Note            *string
	Who             Who
}

// WriteOffBatches writes one expired movement per named batch, earliest
// expiry first, at min(the batch's remaining, available left), with
// ref_type stock_batch. Batches with nothing to take are skipped; when
// nothing is written it is ErrNothingToWriteOff.
func (w *Writer) WriteOffBatches(ctx context.Context, q db.Querier, in WriteOff) ([]Movement, error) {
	if len(in.BatchIDs) == 0 {
		return nil, ErrBatchNotFound
	}
	t, err := w.begin(ctx, q, in.StoreID, in.ItemID)
	if err != nil {
		return nil, err
	}
	want := slices.Clone(in.BatchIDs)
	slices.SortFunc(want, func(a, b ids.ID) int { return a.Compare(b) })
	want = slices.Compact(want)
	u := make([]pgtype.UUID, len(want))
	for i, id := range want {
		u[i] = ids.UUID(id)
	}
	rows, err := t.q.LockBatchesByID(ctx, cataloguedb.LockBatchesByIDParams{StoreItemID: ids.UUID(in.ItemID), Ids: u})
	if err != nil {
		return nil, fmt.Errorf("catalogue: write off: lock batches: %w", err)
	}
	if len(rows) != len(want) {
		return nil, ErrBatchNotFound
	}
	bs := make([]batch, 0, len(rows))
	for _, r := range rows {
		bs = append(bs, toBatch(r.ID, r.Quantity, r.ReceivedAt, r.ExpiresOn))
	}
	bs = plainOrder(bs)
	left := int64(t.item.OnHand) - int64(t.item.Reserved)
	var out []Movement
	for _, b := range bs {
		n := min(int64(b.Qty), left)
		if n <= 0 {
			continue
		}
		k := i32(n) // n ≤ b.Qty
		m, err := t.write(-k, SourceExpired, ref(RefStockBatch, b.ID), ids.Zero, in.Note, in.Who, []take{{ID: b.ID, Qty: -k}})
		if err != nil {
			return nil, err
		}
		out = append(out, m)
		left -= n
	}
	if len(out) == 0 {
		return nil, ErrNothingToWriteOff
	}
	return out, nil
}

// --- the transaction --------------------------------------------------------------

// txn is one write in progress: the locks are held.
type txn struct {
	ctx   context.Context
	w     *Writer
	q     *cataloguedb.Queries
	now   time.Time
	store ids.ID
	item  cataloguedb.LockStoreItemRow
}

// one runs a single-movement write with the client-id replay check: a
// client id already written returns that movement (Replayed) without writing.
func (w *Writer) one(ctx context.Context, q db.Querier, storeID, itemID ids.ID, source string, clientID ids.ID, fn func(*txn) (Movement, error)) (Movement, error) {
	if _, ok := q.(pgx.Tx); !ok {
		return Movement{}, ErrNotInTx
	}
	if clientID != ids.Zero {
		// Fast path without locks; the check after the store lock decides.
		if m, ok, err := replay(ctx, cataloguedb.New(q), storeID, itemID, source, clientID); ok || err != nil {
			return m, err
		}
	}
	t, err := w.begin(ctx, q, storeID, itemID)
	if err != nil {
		return Movement{}, err
	}
	if clientID != ids.Zero {
		if m, ok, err := replay(ctx, t.q, storeID, itemID, source, clientID); ok || err != nil {
			return m, err
		}
	}
	return fn(t)
}

// begin takes the store's catalogue sequence (recording the item change)
// and locks the item.
func (w *Writer) begin(ctx context.Context, q db.Querier, storeID, itemID ids.ID) (*txn, error) {
	if _, ok := q.(pgx.Tx); !ok {
		return nil, ErrNotInTx
	}
	now := w.clock.Now()
	qq := cataloguedb.New(q)
	if _, err := qq.RecordCatalogueChange(ctx, cataloguedb.RecordCatalogueChangeParams{
		StoreID: ids.UUID(storeID), Entity: "store_item", EntityID: ids.UUID(itemID), Op: "upsert", Now: now,
	}); err != nil {
		if db.IsNoRows(err) {
			return nil, ErrItemNotFound
		}
		return nil, fmt.Errorf("catalogue: catalogue change: %w", err)
	}
	item, err := qq.LockStoreItem(ctx, cataloguedb.LockStoreItemParams{ID: ids.UUID(itemID), StoreID: ids.UUID(storeID)})
	if db.IsNoRows(err) {
		return nil, ErrItemNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("catalogue: lock item: %w", err)
	}
	return &txn{ctx: ctx, w: w, q: qq, now: now, store: storeID, item: item}, nil
}

func replay(ctx context.Context, q *cataloguedb.Queries, storeID, itemID ids.ID, source string, clientID ids.ID) (Movement, bool, error) {
	row, err := q.MovementByClientID(ctx, cataloguedb.MovementByClientIDParams{StoreID: ids.UUID(storeID), ClientID: ids.UUID(clientID)})
	if db.IsNoRows(err) {
		return Movement{}, false, nil
	}
	if err != nil {
		return Movement{}, false, fmt.Errorf("catalogue: replay: %w", err)
	}
	if row.StoreItemID != ids.UUID(itemID) || row.Source != source {
		return Movement{}, false, ErrClientIDReused
	}
	alloc, err := q.MovementAllocations(ctx, row.ID)
	if err != nil {
		return Movement{}, false, fmt.Errorf("catalogue: replay: allocation: %w", err)
	}
	m := fromRow(row)
	for _, a := range alloc {
		id, _ := ids.FromUUID(a.BatchID)
		m.Batches = append(m.Batches, Allocation{BatchID: id, Delta: a.Delta})
	}
	m.Replayed = true
	return m, true, nil
}

// batches are the item's batches with remaining stock, locked.
func (t *txn) batches() ([]batch, error) {
	rows, err := t.q.LockItemBatches(t.ctx, t.item.ID)
	if err != nil {
		return nil, fmt.Errorf("catalogue: lock batches: %w", err)
	}
	out := make([]batch, 0, len(rows))
	for _, r := range rows {
		out = append(out, toBatch(r.ID, r.Quantity, r.ReceivedAt, r.ExpiresOn))
	}
	return out, nil
}

type reference struct {
	typ string
	id  ids.ID
}

func ref(typ string, id ids.ID) *reference {
	if typ == "" || id == ids.Zero {
		return nil
	}
	return &reference{typ: typ, id: id}
}

// write inserts the movement, then one allocation row per take (signed as
// given: negative for stock out, positive for stock back in).
func (t *txn) write(delta int32, source string, r *reference, clientID ids.ID, note *string, who Who, takes []take) (Movement, error) {
	row, err := t.insert(delta, source, r, clientID, note, who)
	if err != nil {
		return Movement{}, err
	}
	m := fromRow(row)
	for _, k := range takes {
		if k.Qty == 0 {
			continue
		}
		if err := t.q.InsertMovementBatch(t.ctx, cataloguedb.InsertMovementBatchParams{MovementID: row.ID, BatchID: ids.UUID(k.ID), Delta: k.Qty}); err != nil {
			return Movement{}, fmt.Errorf("catalogue: allocation: %w", err)
		}
		m.Batches = append(m.Batches, Allocation{BatchID: k.ID, Delta: k.Qty})
	}
	return m, nil
}

type newBatch struct {
	received, alloc int32
	expires         *time.Time
	cost            *int64
	foundInCount    bool
}

// writeIn inserts a stock-in movement and, when nb is set, its new batch
// with an allocation row for nb.alloc (none when that is 0).
func (t *txn) writeIn(delta int32, source string, r *reference, clientID ids.ID, note *string, who Who, nb *newBatch) (Movement, error) {
	row, err := t.insert(delta, source, r, clientID, note, who)
	if err != nil {
		return Movement{}, err
	}
	m := fromRow(row)
	if nb == nil {
		return m, nil
	}
	id := t.w.newID(t.now)
	exp := pgtype.Date{}
	if nb.expires != nil {
		exp = pgtype.Date{Time: *nb.expires, Valid: true}
	}
	if err := t.q.InsertBatch(t.ctx, cataloguedb.InsertBatchParams{
		ID: ids.UUID(id), StoreItemID: t.item.ID, ReceivedQuantity: nb.received, ExpiresOn: exp,
		ReceivedAt: t.now, CostPaise: nb.cost, FoundInCount: nb.foundInCount,
	}); err != nil {
		return Movement{}, fmt.Errorf("catalogue: new batch: %w", err)
	}
	if nb.alloc > 0 {
		if err := t.q.InsertMovementBatch(t.ctx, cataloguedb.InsertMovementBatchParams{MovementID: row.ID, BatchID: ids.UUID(id), Delta: nb.alloc}); err != nil {
			return Movement{}, fmt.Errorf("catalogue: allocation: %w", err)
		}
		m.Batches = append(m.Batches, Allocation{BatchID: id, Delta: nb.alloc})
	}
	return m, nil
}

func (t *txn) insert(delta int32, source string, r *reference, clientID ids.ID, note *string, who Who) (cataloguedb.StockMovement, error) {
	p := cataloguedb.InsertMovementParams{
		ID: ids.UUID(t.w.newID(t.now)), StoreID: ids.UUID(t.store), StoreItemID: t.item.ID,
		Delta: delta, Source: source, Note: note, CreatedAt: t.now,
		ActorUserID: optUUID(who.UserID), DeviceID: optUUID(who.DeviceID), ClientID: optUUID(clientID),
	}
	if r != nil {
		p.RefType, p.RefID = &r.typ, ids.UUID(r.id)
	}
	row, err := t.q.InsertMovement(t.ctx, p)
	if err != nil {
		if db.IsUniqueViolation(err, "ux_stock_movements_store_client") {
			// A concurrent write with the same client id won the race.
			return cataloguedb.StockMovement{}, ErrClientIDReused
		}
		return cataloguedb.StockMovement{}, fmt.Errorf("catalogue: movement: %w", err)
	}
	return row, nil
}

// --- conversions --------------------------------------------------------------

func optUUID(id ids.ID) pgtype.UUID {
	if id == ids.Zero {
		return pgtype.UUID{}
	}
	return ids.UUID(id)
}

func toBatch(id pgtype.UUID, qty int32, received time.Time, exp pgtype.Date) batch {
	b := batch{Qty: qty, Received: received}
	b.ID, _ = ids.FromUUID(id)
	if exp.Valid {
		t := exp.Time
		b.Expires = &t
	}
	return b
}

func negate(ts []take) []take {
	out := make([]take, len(ts))
	for i, t := range ts {
		out[i] = take{ID: t.ID, Qty: -t.Qty}
	}
	return out
}

func fromRow(r cataloguedb.StockMovement) Movement {
	m := Movement{Delta: r.Delta, Source: r.Source, RefType: r.RefType, Note: r.Note, CreatedAt: r.CreatedAt}
	m.ID, _ = ids.FromUUID(r.ID)
	m.StoreID, _ = ids.FromUUID(r.StoreID)
	m.ItemID, _ = ids.FromUUID(r.StoreItemID)
	if id, ok := ids.FromUUID(r.RefID); ok {
		m.RefID = &id
	}
	return m
}

// perBatch reads ReversedFrom's {batchId: qty} map.
func perBatch(raw json.RawMessage) (map[ids.ID]int32, error) {
	var m map[string]int32
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("catalogue: reversed per batch: %w", err)
	}
	out := make(map[ids.ID]int32, len(m))
	for k, v := range m {
		var u pgtype.UUID
		if err := u.Scan(k); err != nil {
			return nil, fmt.Errorf("catalogue: reversed per batch: %w", err)
		}
		id, _ := ids.FromUUID(u)
		out[id] = v
	}
	return out, nil
}

// --- the read interface --------------------------------------------------------

// Item is what orders and POS read of one store item.
type Item struct {
	ID, StoreID, ProductID ids.ID
	Name                   string
	PricePaise             int64
	MrpPaise               *int64
	OnHand, Reserved       int32
	// Available is on_hand − reserved (may be below 0).
	Available      int32
	IsListed       bool
	StockUpdatedAt *time.Time
}

// LookupItem reads one item of one store with q (the pool or the caller's
// transaction); an item of another store is ErrItemNotFound.
func LookupItem(ctx context.Context, q db.Querier, storeID, itemID ids.ID) (Item, error) {
	r, err := cataloguedb.New(q).LookupStoreItem(ctx, cataloguedb.LookupStoreItemParams{ID: ids.UUID(itemID), StoreID: ids.UUID(storeID)})
	if db.IsNoRows(err) {
		return Item{}, ErrItemNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("catalogue: lookup item: %w", err)
	}
	it := Item{
		Name: r.Name, PricePaise: r.PricePaise, MrpPaise: r.MrpPaise, OnHand: r.OnHand, Reserved: r.Reserved,
		Available: r.OnHand - r.Reserved, IsListed: r.IsListed, StockUpdatedAt: r.StockUpdatedAt,
	}
	it.ID, _ = ids.FromUUID(r.ID)
	it.StoreID, _ = ids.FromUUID(r.StoreID)
	it.ProductID, _ = ids.FromUUID(r.ProductID)
	return it, nil
}
