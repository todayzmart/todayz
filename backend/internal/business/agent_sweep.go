package business

// The field photos sweep: a field photo whose upload was never confirmed by
// its confirm_by is rejected and its object deleted, so a failed upload
// neither holds a place in the 5-per-kind cap nor leaves a file behind. Owner
// uploads (no confirm step yet, confirm_by null) are not touched.
//
// Each batch is claimed in a short transaction (sweep_retry_at set a lease
// ahead) and the objects are deleted after it commits, so no row lock is held
// over storage calls. A photo whose object could not be deleted stays pending
// and waits out a backoff (sweep_retry_at, sweep_attempts) while the sweep
// goes on with the rest; tz_field_photo_sweep_failed_total and the run's
// "failed" count it.

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// sweepBatch is how many rows one claim takes.
const sweepBatch = 200

const (
	// sweepLease is how long a claimed row is left to the sweep that claimed
	// it (the job's timeout is a minute); a sweep that died leaves its rows
	// to a run after it.
	sweepLease = 10 * time.Minute
	// A failed delete is tried again after sweepRetryBase, doubling per
	// failed attempt up to sweepRetryMax (the job runs hourly).
	sweepRetryBase = time.Hour
	sweepRetryMax  = 24 * time.Hour
)

func sweepBackoff(attempts int32) time.Duration {
	d := sweepRetryBase
	for i := int32(0); i < attempts && d < sweepRetryMax; i++ {
		d *= 2
	}
	return min(d, sweepRetryMax)
}

func claimParams(now time.Time) businessdb.ClaimStaleFieldDocumentsParams {
	return businessdb.ClaimStaleFieldDocumentsParams{Now: now, LeaseUntil: now.Add(sweepLease), Lim: sweepBatch}
}

// SweepResult is what one sweep did: rows rejected, and objects it could not
// delete (left pending, retried after their backoff).
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
	log := telemetry.Logger(ctx)
	for ctx.Err() == nil {
		now := s.clock.Now()
		var rows []businessdb.ClaimStaleFieldDocumentsRow
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			var err error
			rows, err = s.q(ctx).ClaimStaleFieldDocuments(ctx, claimParams(now))
			return err
		})
		if err != nil {
			return res, err
		}
		gone := make([]pgtype.UUID, 0, len(rows))
		for _, r := range rows {
			if ctx.Err() != nil {
				break // the rest wait out their lease
			}
			if err := s.presign.remove(ctx, r.ObjectKey, now); err != nil {
				if ctx.Err() != nil {
					break
				}
				log.WarnContext(ctx, "business: sweep could not delete a field photo", "document", idString(r.ID), "attempts", r.SweepAttempts+1, "error", err)
				res.Failed++
				countSweepFailed(ctx)
				if err := s.q(ctx).FailStaleDocumentSweep(ctx, businessdb.FailStaleDocumentSweepParams{
					ID: r.ID, RetryAt: now.Add(sweepBackoff(r.SweepAttempts)),
				}); err != nil {
					return res, err
				}
				continue
			}
			gone = append(gone, r.ID)
		}
		if len(gone) > 0 && ctx.Err() == nil {
			d, err := s.q(ctx).RejectStaleDocuments(ctx, businessdb.RejectStaleDocumentsParams{Now: now, Ids: gone})
			res.Rejected += d
			if err != nil {
				return res, err
			}
		}
		if len(rows) < sweepBatch {
			break
		}
	}
	return res, nil
}

var (
	sweepMetricsOnce sync.Once
	mSweepFailed     metric.Int64Counter
)

// countSweepFailed counts one field photo whose object the sweep could not
// delete (tz_field_photo_sweep_failed_total). No key or id is a label.
func countSweepFailed(ctx context.Context) {
	sweepMetricsOnce.Do(func() {
		mSweepFailed, _ = otel.Meter("todayz/business").Int64Counter(telemetry.MetricPrefix+"field_photo_sweep_failed_total",
			metric.WithDescription("Stale field photos whose object the sweep could not delete (left pending, retried after a backoff)"))
	})
	if mSweepFailed != nil {
		mSweepFailed.Add(ctx, 1)
	}
}
