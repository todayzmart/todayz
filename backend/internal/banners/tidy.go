package banners

import (
	"context"
	"fmt"

	"github.com/todayz/backend/gen/sqlc/bannersdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
)

// TidyResult counts what one Tidy run changed.
type TidyResult struct{ Started, Ended int64 }

// Tidy brings the status labels up to date: scheduled banners that have begun
// (and passed moderation) go live, banners past their end go ended. Housekeeping
// only: customers see a banner by its dates and moderation, not this label.
func Tidy(ctx context.Context, d *db.DB, c clock.Clock) (TidyResult, error) {
	var res TidyResult
	now := c.Now()
	err := d.InTx(ctx, func(ctx context.Context) error {
		q := bannersdb.New(d.Q(ctx))
		started, err := q.StartDueBanners(ctx, now)
		if err != nil {
			return fmt.Errorf("banners: start due: %w", err)
		}
		ended, err := q.EndDueBanners(ctx, now)
		if err != nil {
			return fmt.Errorf("banners: end due: %w", err)
		}
		res = TidyResult{Started: int64(len(started)), Ended: int64(len(ended))}
		return nil
	})
	return res, err
}
