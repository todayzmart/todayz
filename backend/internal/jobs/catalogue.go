package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

// KindCatalogueReconcile compares store_items.on_hand and each batch's
// quantity with their ledgers and repairs drift from the ledger (03 stock
// rules 1 and 8, D-99; maintenance class).
const KindCatalogueReconcile = "catalogue_reconcile"

// registercatalogue declares the catalogue domain's job kinds.
func registercatalogue(r *Registry, d Deps) {
	store := db.New(d.Pool)
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindCatalogueReconcile, Owner: "W1-05", Class: platformjobs.ClassMaintenance,
		Description: "Repairs stock counters that drifted from the movement ledger and logs items whose batches hold more than their stock",
		Schedule:    "40 3 * * *", UniqueBy: platformjobs.PerDay, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			res, err := catalogue.Reconcile(ctx, store, d.Clock)
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{
				"on_hand_repaired": res.OnHandRepaired, "batches_repaired": res.BatchesRepaired,
				"batch_over": res.BatchOver, "failed": res.Failed,
			}}, nil
		},
	})
}
