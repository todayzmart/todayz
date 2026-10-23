package catalogue

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// ReconcileResult counts what one reconcile run found and repaired.
type ReconcileResult struct {
	// OnHandRepaired items had on_hand ≠ Σ movements.delta.
	OnHandRepaired int64
	// BatchesRepaired batches had quantity ≠ Σ allocation rows.
	BatchesRepaired int64
	// BatchOver items hold more in batches than max(on_hand, 0); logged at
	// error level for a person, never repaired here (which batch to trim is a
	// judgement).
	BatchOver int64
	// Failed repairs (a ledger sum outside the batch's 0..received range).
	Failed int64
}

// reconcileBatch bounds each pass (the job runs in under 60 s, D-98).
const reconcileBatch = 500

// Reconcile compares the cached counters with their ledgers (03 stock rule
// 1, D-99) and repairs any drift from the ledger, which is the truth. Each
// repair is its own transaction holding one row lock (the item or the
// batch), so it never waits on a writer while holding another lock, and it
// reads the ledger after taking the lock, so a movement committed meanwhile
// is counted. Each repair is audited as the system. Drift should never
// happen (the triggers keep the counters); a repair means a bug or a manual
// change, so the run logs at error level.
func Reconcile(ctx context.Context, d *db.DB, c clock.Clock) (ReconcileResult, error) {
	var res ReconcileResult
	log := telemetry.Logger(ctx)
	aw := audit.New(d, c, nil)
	q := cataloguedb.New(d.Pool)

	items, err := q.OnHandDrift(ctx, reconcileBatch)
	if err != nil {
		return res, fmt.Errorf("catalogue: on-hand drift: %w", err)
	}
	for _, r := range items {
		var before, after int32
		err := d.InTx(ctx, func(ctx context.Context) error {
			tq := cataloguedb.New(d.Q(ctx))
			cur, err := tq.LockItemForRepair(ctx, r.ID)
			if err != nil {
				return err
			}
			sum, err := tq.ItemLedgerSum(ctx, r.ID)
			if err != nil {
				return err
			}
			if cur == sum {
				return nil
			}
			before, after = cur, sum
			if err := tq.RepairOnHand(ctx, cataloguedb.RepairOnHandParams{OnHand: sum, ID: r.ID}); err != nil {
				return err
			}
			return repairAudit(ctx, aw, "store_item", r.ID, before, after)
		})
		if err != nil {
			res.Failed++
			log.ErrorContext(ctx, "catalogue reconcile: on-hand repair failed", "store_item_id", ulidOf(r.ID), "error", err)
			continue
		}
		if before != after {
			res.OnHandRepaired++
			log.ErrorContext(ctx, "catalogue reconcile: on_hand drifted from the ledger; repaired",
				"store_item_id", ulidOf(r.ID), "store_id", ulidOf(r.StoreID), "cached", before, "ledger", after)
		}
	}

	batches, err := q.BatchDrift(ctx, reconcileBatch)
	if err != nil {
		return res, fmt.Errorf("catalogue: batch drift: %w", err)
	}
	for _, r := range batches {
		var before, after int32
		err := d.InTx(ctx, func(ctx context.Context) error {
			tq := cataloguedb.New(d.Q(ctx))
			cur, err := tq.LockBatchForRepair(ctx, r.ID)
			if err != nil {
				return err
			}
			sum, err := tq.BatchLedgerSum(ctx, r.ID)
			if err != nil {
				return err
			}
			if cur == sum {
				return nil
			}
			before, after = cur, sum
			if err := tq.RepairBatchQuantity(ctx, cataloguedb.RepairBatchQuantityParams{Quantity: sum, ID: r.ID}); err != nil {
				return err
			}
			return repairAudit(ctx, aw, "stock_batch", r.ID, before, after)
		})
		if err != nil {
			res.Failed++
			log.ErrorContext(ctx, "catalogue reconcile: batch repair failed", "batch_id", ulidOf(r.ID), "error", err)
			continue
		}
		if before != after {
			res.BatchesRepaired++
			log.ErrorContext(ctx, "catalogue reconcile: batch quantity drifted from its allocations; repaired",
				"batch_id", ulidOf(r.ID), "store_item_id", ulidOf(r.StoreItemID), "cached", before, "ledger", after)
		}
	}

	over, err := q.BatchOverItems(ctx, reconcileBatch)
	if err != nil {
		return res, fmt.Errorf("catalogue: batch over items: %w", err)
	}
	for _, r := range over {
		res.BatchOver++
		log.ErrorContext(ctx, "catalogue reconcile: batches hold more than the item's stock (03 stock rule 8)",
			"store_item_id", ulidOf(r.ID), "store_id", ulidOf(r.StoreID), "on_hand", r.OnHand, "in_batches", r.InBatches)
	}
	return res, nil
}

type repairState struct {
	Cached int32 `json:"cached"`
}

func repairAudit(ctx context.Context, aw *audit.Writer, subject string, id pgtype.UUID, before, after int32) error {
	sid, _ := ids.FromUUID(id)
	_, err := aw.Write(ctx, audit.Entry{
		ActorKind: audit.ActorSystem, Action: "catalogue.reconcile_repair",
		SubjectType: subject, SubjectID: sid.String(),
		Before: repairState{Cached: before}, After: repairState{Cached: after},
	})
	return err
}
