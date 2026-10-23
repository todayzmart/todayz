package catalogue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	catalogueapi "github.com/todayz/backend/gen/api/catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// kindAction is the action each movement kind needs beyond the route's
// stock.count (D-06, 02 §3.3).
var kindAction = map[catalogueapi.MovementKind]string{
	catalogueapi.MovementKindRestock:         policy.StockRestock,
	catalogueapi.MovementKindCountCorrection: policy.StockCount,
	catalogueapi.MovementKindDamage:          policy.StockDamage,
	catalogueapi.MovementKindExpired:         policy.StockDamage,
}

func (s *Service) createMovement(ctx context.Context, bid, sid, itemID string, b *catalogueapi.MovementCreate) (catalogueapi.MovementResult, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.MovementResult{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return catalogueapi.MovementResult{}, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.MovementResult{}, err
	}
	id, err := pathID(itemID)
	if err != nil {
		return catalogueapi.MovementResult{}, err
	}
	if b == nil {
		return catalogueapi.MovementResult{}, problem.New(genproblems.BadRequest, "")
	}
	action, ok := kindAction[b.Kind]
	if !ok {
		return catalogueapi.MovementResult{}, fieldErr("body.kind", "unknown kind")
	}
	if action != policy.StockCount {
		if err := s.also(ctx, action, sc); err != nil {
			return catalogueapi.MovementResult{}, err
		}
	}
	now := s.clock.Now()
	var v invalid
	v.text("body.note", b.Note)
	byBatch := b.BatchIds != nil
	if byBatch && b.Kind != catalogueapi.MovementKindExpired {
		v.add("body.batchIds", "only for kind expired")
	}
	if b.Kind != catalogueapi.MovementKindRestock {
		if b.ExpiresOn != nil {
			v.add("body.expiresOn", "only for kind restock")
		}
		if b.CostPaise != nil {
			v.add("body.costPaise", "only for kind restock")
		}
	}
	exp := istDate(b.ExpiresOn)
	if exp != nil && exp.Before(Today(now)) {
		v.add("body.expiresOn", "must not be in the past")
	}
	qty := int32(-1)
	if b.Quantity != nil {
		qty = i32i(*b.Quantity) // contract: 0..1,000,000
	}
	switch {
	case byBatch:
		// quantity is ignored: each batch goes at what it has left.
	case qty < 0:
		v.add("body.quantity", "required")
	case qty == 0 && b.Kind != catalogueapi.MovementKindCountCorrection:
		v.add("body.quantity", "must be above 0")
	}
	var batchIDs []ids.ID
	if byBatch {
		for i, s := range *b.BatchIds {
			bid, err := ids.Parse(s)
			if err != nil {
				v.add("body.batchIds["+strconv.Itoa(i)+"]", "not a batch id")
				continue
			}
			batchIDs = append(batchIDs, bid)
		}
	}
	if err := v.err(); err != nil {
		return catalogueapi.MovementResult{}, err
	}
	who := Who{UserID: p.UserID}
	var written []Movement
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var (
			m   Movement
			err error
		)
		switch {
		case b.Kind == catalogueapi.MovementKindRestock:
			m, err = s.writer.AddStock(ctx, q, StockIn{
				StoreID: sc.StoreID, ItemID: id, Qty: qty, Source: SourceRestock,
				ExpiresOn: exp, CostPaise: int64p(b.CostPaise), Note: b.Note, Who: who,
			})
		case b.Kind == catalogueapi.MovementKindCountCorrection:
			var wrote bool
			m, wrote, err = s.writer.Count(ctx, q, Count{StoreID: sc.StoreID, ItemID: id, Counted: qty, Note: b.Note, Who: who})
			if err == nil && !wrote {
				return nil
			}
		case byBatch:
			written, err = s.writer.WriteOffBatches(ctx, q, WriteOff{StoreID: sc.StoreID, ItemID: id, BatchIDs: batchIDs, Note: b.Note, Who: who})
			return writerProblem(err)
		default:
			// damage, or expired without batches: never more than is
			// available (TODO(lead): the cap is this slice's reading of D-06;
			// the offline POS writes through the writer without it).
			source := SourceDamage
			if b.Kind == catalogueapi.MovementKindExpired {
				source = SourceExpired
			}
			m, err = s.writer.Remove(ctx, q, Removal{
				StoreID: sc.StoreID, ItemID: id, Qty: qty, Source: source, CapAvailable: true, Note: b.Note, Who: who,
			})
		}
		if err != nil {
			return writerProblem(err)
		}
		written = append(written, m)
		return nil
	})
	if err != nil {
		return catalogueapi.MovementResult{}, err
	}
	item, _, err := s.item(ctx, sc.StoreID, id)
	if err != nil {
		return catalogueapi.MovementResult{}, err
	}
	out := catalogueapi.MovementResult{Item: item, Movements: make([]catalogueapi.Movement, 0, len(written))}
	for _, m := range written {
		out.Movements = append(out.Movements, toMovement(m))
	}
	return out, nil
}

func toMovement(m Movement) catalogueapi.Movement {
	out := catalogueapi.Movement{
		Id: m.ID.String(), Delta: int(m.Delta), Source: catalogueapi.MovementSource(m.Source),
		RefType: m.RefType, Note: m.Note, CreatedAt: m.CreatedAt.UTC(),
		Batches: make([]catalogueapi.BatchAllocation, 0, len(m.Batches)),
	}
	if m.RefID != nil {
		out.RefId = ptr(m.RefID.String())
	}
	for _, a := range m.Batches {
		out.Batches = append(out.Batches, catalogueapi.BatchAllocation{BatchId: a.BatchID.String(), Delta: int(a.Delta)})
	}
	return out
}

// history cursor key: the movement's created_at as Unix nanoseconds.
func (s *Service) history(ctx context.Context, bid, sid, itemID string, p catalogueapi.GetStoreItemHistoryParams) (catalogueapi.MovementList, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.MovementList{}, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.MovementList{}, err
	}
	id, err := pathID(itemID)
	if err != nil {
		return catalogueapi.MovementList{}, err
	}
	// The item must be this store's (another store's item is 404).
	if _, err := LookupItem(ctx, s.db.Q(ctx), sc.StoreID, id); err != nil {
		return catalogueapi.MovementList{}, writerProblem(err)
	}
	lim := limitOf(p.Limit)
	arg := cataloguedb.ItemHistoryParams{StoreItemID: ids.UUID(id), Lim: lim + 1}
	if p.Cursor != nil {
		key, cid, err := decodeCursor(*p.Cursor)
		if err != nil {
			return catalogueapi.MovementList{}, badCursor()
		}
		ns, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return catalogueapi.MovementList{}, badCursor()
		}
		at := time.Unix(0, ns).UTC()
		arg.BeforeAt, arg.BeforeID = &at, ids.UUID(cid)
	}
	rows, err := s.q(ctx).ItemHistory(ctx, arg)
	if err != nil {
		return catalogueapi.MovementList{}, fmt.Errorf("catalogue: history: %w", err)
	}
	out := catalogueapi.MovementList{Items: make([]catalogueapi.Movement, 0, min(len(rows), int(lim)))}
	for i, r := range rows {
		if i == int(lim) {
			last := rows[i-1]
			lid, _ := ids.FromUUID(last.ID)
			out.NextCursor = ptr(encodeCursor(strconv.FormatInt(last.CreatedAt.UnixNano(), 10), lid))
			break
		}
		var alloc []struct {
			BatchID string `json:"batchId"`
			Delta   int32  `json:"delta"`
		}
		if err := json.Unmarshal(r.Batches, &alloc); err != nil {
			return catalogueapi.MovementList{}, fmt.Errorf("catalogue: history batches: %w", err)
		}
		m := Movement{Delta: r.Delta, Source: r.Source, RefType: r.RefType, Note: r.Note, CreatedAt: r.CreatedAt}
		m.ID, _ = ids.FromUUID(r.ID)
		if rid, ok := ids.FromUUID(r.RefID); ok {
			m.RefID = &rid
		}
		for _, a := range alloc {
			bid, err := uuidText(a.BatchID)
			if err != nil {
				return catalogueapi.MovementList{}, err
			}
			m.Batches = append(m.Batches, Allocation{BatchID: bid, Delta: a.Delta})
		}
		out.Items = append(out.Items, toMovement(m))
	}
	return out, nil
}
