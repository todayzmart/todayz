package coach

import (
	"context"
	"log/slog"
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/coachdb"
	"github.com/todayz/backend/internal/coach/detectors"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// batchSize is the stores one checkpoint covers in a batched run (D-98).
const batchSize = 50 //settings:ok a batching bound, not a product number

// RunBatched runs a daily or weekly job over every live store, in id order
// from the period's checkpoint: after each batch it saves the checkpoint and
// yields when the worker drains, so a continuation goes on from there and a
// store is never run twice in a period (a rerun of a store finds its cards
// live and skips them anyway).
func (s *Service) RunBatched(ctx context.Context, j detectors.Job, period string) (Summary, error) {
	var sum Summary
	q := coachdb.New(s.d.DB.Q(ctx))
	after := ids.UUID(ids.Zero)
	var done int32
	cp, err := q.Checkpoint(ctx, coachdb.CheckpointParams{Job: string(j), Period: period})
	switch {
	case err == nil:
		after, done = cp.After, cp.Stores
	case !db.IsNoRows(err):
		return sum, err
	}
	for {
		stores, err := q.LiveStoresAfter(ctx, coachdb.LiveStoresAfterParams{After: after, Lim: batchSize})
		if err != nil {
			return sum, err
		}
		if len(stores) == 0 {
			return sum, nil
		}
		for _, st := range stores {
			id, _ := ids.FromUUID(st.ID)
			r, err := s.RunStore(ctx, j, id)
			if err != nil {
				// One store's failure is counted and logged; the run goes on.
				telemetry.Logger(ctx).ErrorContext(ctx, "coach: store run failed",
					slog.String("job", string(j)), slog.String("store_id", id.String()), slog.String("error", err.Error()))
				r.Failed++
			}
			sum.Add(r)
		}
		after = stores[len(stores)-1].ID
		done += int32(len(stores)) // #nosec G115 -- a batch is at most batchSize
		if err := q.PutCheckpoint(ctx, coachdb.PutCheckpointParams{
			Job: string(j), Period: period, After: after, Stores: done, Now: s.d.Clock.Now(),
		}); err != nil {
			return sum, err
		}
		if err := platformjobs.Yield(ctx); err != nil {
			return sum, err
		}
	}
}

// RunRealtime runs the realtime rules (expiry_penalty) for the stores they can
// fire for or still show a card of: only those, so a 15-minute tick stays
// inside a non-batched job's time.
func (s *Service) RunRealtime(ctx context.Context) (Summary, error) {
	var sum Summary
	bp, err := s.d.Settings.BP(ctx, settings.ShopExpiryPenaltyThresholdBp)
	if err != nil {
		return sum, err
	}
	rate := pgtype.Numeric{Int: big.NewInt(int64(bp)), Exp: -4, Valid: true}
	names := make([]string, 0, 1)
	for _, d := range detectors.For(detectors.Realtime) {
		names = append(names, d.Name)
	}
	stores, err := coachdb.New(s.d.DB.Q(ctx)).RealtimeStores(ctx, coachdb.RealtimeStoresParams{RateAbove: rate, Detectors: names})
	if err != nil {
		return sum, err
	}
	for _, u := range stores {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		id, _ := ids.FromUUID(u)
		r, err := s.RunStore(ctx, detectors.Realtime, id)
		if err != nil {
			telemetry.Logger(ctx).ErrorContext(ctx, "coach: store run failed",
				slog.String("job", string(detectors.Realtime)), slog.String("store_id", id.String()), slog.String("error", err.Error()))
			r.Failed++
		}
		sum.Add(r)
	}
	return sum, nil
}
