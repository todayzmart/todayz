package business

// The field photos sweep: a field photo whose upload was never confirmed by
// its confirm_by is rejected and its object deleted, so a failed upload
// neither holds a place in the 5-per-kind cap nor leaves a file behind. Owner
// uploads (no confirm step yet, confirm_by null) are not touched.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// sweepBatch is how many rows one sweep transaction takes.
const sweepBatch = 200

// SweepResult is what one sweep did: rows rejected, and objects it could not
// delete (left pending for the next run).
type SweepResult struct {
	Rejected int64
	Failed   int64
}

// SweepFieldPhotos rejects the field photos past their confirm_by and
// deletes their objects, in batches until none is left or the context ends
// (the job's timeout; the next run takes the rest).
func (s *Service) SweepFieldPhotos(ctx context.Context) (SweepResult, error) {
	var res SweepResult
	if s.db == nil {
		return res, errUnavailable
	}
	if s.presign == nil {
		return res, errNoStorage
	}
	for ctx.Err() == nil {
		var n int
		var failed bool
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			q := s.q(ctx)
			now := s.clock.Now()
			rows, err := q.StaleFieldDocuments(ctx, businessdb.StaleFieldDocumentsParams{Now: now, Lim: sweepBatch})
			if err != nil {
				return err
			}
			n = len(rows)
			gone := make([]pgtype.UUID, 0, len(rows))
			for _, r := range rows {
				if err := s.presign.remove(ctx, r.ObjectKey, now); err != nil {
					telemetry.Logger(ctx).WarnContext(ctx, "business: sweep could not delete a field photo", "error", err)
					res.Failed++
					failed = true
					continue
				}
				gone = append(gone, r.ID)
			}
			if len(gone) == 0 {
				return nil
			}
			d, err := q.RejectStaleDocuments(ctx, businessdb.RejectStaleDocumentsParams{Now: now, Ids: gone})
			res.Rejected += d
			return err
		})
		if err != nil {
			return res, err
		}
		if n < sweepBatch || failed {
			break
		}
	}
	return res, nil
}
