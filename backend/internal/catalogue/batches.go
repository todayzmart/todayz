package catalogue

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	catalogueapi "github.com/todayz/backend/gen/api/catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// batchRow is the shape both batch queries return.
type batchRow = cataloguedb.GetStoreBatchRow

func dateOf(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := time.Date(d.Time.Year(), d.Time.Month(), d.Time.Day(), 0, 0, 0, 0, time.UTC)
	return &t
}

func pgDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

func toBatchAPI(r batchRow, today time.Time) catalogueapi.StockBatch {
	out := catalogueapi.StockBatch{
		Id: ulidOf(r.ID), ItemId: ulidOf(r.StoreItemID), ItemName: r.ItemName,
		PricePaise: catalogueapi.Paise(r.PricePaise), MrpPaise: intp(r.MrpPaise),
		Quantity: int(r.Quantity), ReceivedQuantity: int(r.ReceivedQuantity),
		ReceivedAt: r.ReceivedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
		CostPaise: intp(r.CostPaise), FoundInCount: r.FoundInCount, Etag: etag(r.Version),
	}
	if e := dateOf(r.ExpiresOn); e != nil {
		out.ExpiresOn = &openapi_types.Date{Time: *e}
		out.Expired = e.Before(today)
	}
	return out
}

func (s *Service) listBatches(ctx context.Context, bid, sid string, p catalogueapi.ListStockBatchesParams) (catalogueapi.StockBatchList, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.StockBatchList{}, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.StockBatchList{}, err
	}
	arg := cataloguedb.ListStoreBatchesParams{StoreID: ids.UUID(sc.StoreID), Lim: limitOf(p.Limit)}
	if p.Empty != nil {
		arg.IncludeEmpty = *p.Empty
	}
	if p.ItemId != nil {
		iid, err := ids.Parse(*p.ItemId)
		if err != nil {
			return catalogueapi.StockBatchList{}, fieldErr("query.itemId", "not an item id")
		}
		arg.StoreItemID = ids.UUID(iid)
	}
	arg.ExpiresBefore = pgDate(istDate(p.ExpiresBefore))
	rows, err := s.q(ctx).ListStoreBatches(ctx, arg)
	if err != nil {
		return catalogueapi.StockBatchList{}, fmt.Errorf("catalogue: batches: %w", err)
	}
	today := Today(s.clock.Now())
	out := catalogueapi.StockBatchList{Items: make([]catalogueapi.StockBatch, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, toBatchAPI(batchRow(r), today))
	}
	return out, nil
}

// batchAudit is what the audit row keeps of a batch: cost and expiry only.
type batchAudit struct {
	CostPaise *int64  `json:"costPaise"`
	ExpiresOn *string `json:"expiresOn"`
	Version   int32   `json:"version"`
}

func auditOf(cost *int64, exp *time.Time, version int32) batchAudit {
	a := batchAudit{CostPaise: cost, Version: version}
	if exp != nil {
		a.ExpiresOn = ptr(exp.Format(time.DateOnly))
	}
	return a
}

// updateBatch corrects a batch's cost or expiry (D-99): manager and above
// (the route's batches.manage), If-Match on the batch's version, audited.
// A past expiry is allowed: it corrects what was entered at stock in, and
// the printed date may already have passed.
func (s *Service) updateBatch(ctx context.Context, bid, sid, batchID string, h *string, b *catalogueapi.StockBatchUpdate) (catalogueapi.StockBatch, int32, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.StockBatch{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return catalogueapi.StockBatch{}, 0, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.StockBatch{}, 0, err
	}
	id, err := pathID(batchID)
	if err != nil {
		return catalogueapi.StockBatch{}, 0, err
	}
	if b == nil {
		return catalogueapi.StockBatch{}, 0, problem.New(genproblems.BadRequest, "")
	}
	version, err := ifMatch(h)
	if err != nil {
		return catalogueapi.StockBatch{}, 0, err
	}
	setCost, setExp := sent(ctx, "costPaise"), sent(ctx, "expiresOn")
	exp := istDate(b.ExpiresOn)
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := cataloguedb.New(s.db.Q(ctx))
		cur, err := q.GetStoreBatch(ctx, cataloguedb.GetStoreBatchParams{ID: ids.UUID(id), StoreID: ids.UUID(sc.StoreID)})
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("catalogue: batch: %w", err)
		}
		if cur.Version != version {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if setExp && exp != nil && cur.FoundInCount {
			return fieldErr("body.expiresOn", "a batch found in a count has no expiry")
		}
		itemID, _ := ids.FromUUID(cur.StoreItemID)
		// Lock order (catalogue.go): the stores row first, then the batch.
		if err := s.recordChange(ctx, s.db.Q(ctx), sc.StoreID, itemID); err != nil {
			return err
		}
		if !setCost && !setExp {
			return nil
		}
		arg := cataloguedb.UpdateBatchParams{
			SetCost: setCost, CostPaise: int64p(b.CostPaise), SetExpires: setExp, ExpiresOn: pgDate(exp),
			ID: ids.UUID(id), Version: version, StoreID: ids.UUID(sc.StoreID),
		}
		if _, err := q.UpdateBatch(ctx, arg); err != nil {
			if db.IsNoRows(err) {
				return problem.New(genproblems.PreconditionFailed, "")
			}
			return fmt.Errorf("catalogue: update batch: %w", err)
		}
		cost, after := cur.CostPaise, dateOf(cur.ExpiresOn)
		if setCost {
			cost = arg.CostPaise
		}
		if setExp {
			after = exp
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorStaff, ActorID: p.UserID, Action: "catalogue.batch_update",
			SubjectType: "stock_batch", SubjectID: id.String(),
			Before: auditOf(cur.CostPaise, dateOf(cur.ExpiresOn), cur.Version),
			After:  auditOf(cost, after, cur.Version+1),
		})
		if err != nil {
			return fmt.Errorf("catalogue: audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return catalogueapi.StockBatch{}, 0, err
	}
	r, err := s.q(ctx).GetStoreBatch(ctx, cataloguedb.GetStoreBatchParams{ID: ids.UUID(id), StoreID: ids.UUID(sc.StoreID)})
	if err != nil {
		return catalogueapi.StockBatch{}, 0, fmt.Errorf("catalogue: batch: %w", err)
	}
	return toBatchAPI(r, Today(s.clock.Now())), r.Version, nil
}
