package offers

import (
	"context"
	"fmt"

	"github.com/todayz/backend/gen/sqlc/offersdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

// TidyResult counts what one Tidy run changed.
type TidyResult struct{ Started, Ended, Spent int64 }

// Tidy brings the status labels up to date: scheduled offers that have begun
// go live, offers past their end or with every redemption used go ended. It is
// only housekeeping: pricing reads the dates and counters itself, so an offer
// ends on time even when this job runs late. Each change bumps the store's
// catalogue sequence so the POS picks it up (D-36).
func Tidy(ctx context.Context, d *db.DB, c clock.Clock) (TidyResult, error) {
	var res TidyResult
	now := c.Now()
	err := d.InTx(ctx, func(ctx context.Context) error {
		q := offersdb.New(d.Q(ctx))
		type change struct{ id, store ids.ID }
		var changes []change
		// The stores rows first, as every catalogue writer takes them (D-280).
		if err := q.LockStoresWithDueOffers(ctx, now); err != nil {
			return fmt.Errorf("offers: lock stores: %w", err)
		}
		started, err := q.StartDueOffers(ctx, now)
		if err != nil {
			return fmt.Errorf("offers: start due: %w", err)
		}
		for _, r := range started {
			changes = append(changes, change{idOf(r.ID), idOf(r.StoreID)})
		}
		ended, err := q.EndDueOffers(ctx, now)
		if err != nil {
			return fmt.Errorf("offers: end due: %w", err)
		}
		for _, r := range ended {
			changes = append(changes, change{idOf(r.ID), idOf(r.StoreID)})
		}
		spent, err := q.EndSpentOffers(ctx, now)
		if err != nil {
			return fmt.Errorf("offers: end spent: %w", err)
		}
		for _, r := range spent {
			changes = append(changes, change{idOf(r.ID), idOf(r.StoreID)})
		}
		res = TidyResult{Started: int64(len(started)), Ended: int64(len(ended)), Spent: int64(len(spent))}
		for _, ch := range changes {
			if _, err := q.RecordCatalogueChange(ctx, offersdb.RecordCatalogueChangeParams{
				StoreID: ids.UUID(ch.store), EntityID: ids.UUID(ch.id), Op: "upsert", Now: now,
			}); err != nil {
				return fmt.Errorf("offers: catalogue change: %w", err)
			}
		}
		return nil
	})
	return res, err
}
