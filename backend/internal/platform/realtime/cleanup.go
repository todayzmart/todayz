package realtime

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Retention is how long outbox_events rows are kept for Last-Event-ID replay
// (02 §5: 24 h). It is an architecture constant, not a product rule: a client
// away longer gets `reset` and refetches.
const Retention = 24 * time.Hour

// cleanupBatch bounds one DELETE so the cleanup never holds long locks.
const cleanupBatch = 5000

// cleanupSQL deletes one batch of expired rows and raises the watermark to
// the greatest xid it deleted, in one statement: a reader can never see the
// rows gone and the watermark still low (it would replay a gap instead of
// answering reset).
const cleanupSQL = `
WITH del AS (
  DELETE FROM outbox_events
  WHERE seq IN (SELECT seq FROM outbox_events WHERE created_at < $1 ORDER BY seq LIMIT $2)
  RETURNING xid
), top AS (
  SELECT count(*) AS n, max(xid) AS x FROM del
), wm AS (
  UPDATE outbox_watermark
  SET deleted_through_xid = GREATEST(deleted_through_xid, (SELECT x FROM top)), updated_at = $3
  WHERE id = 1 AND (SELECT n FROM top) > 0
)
SELECT n FROM top`

// CleanupResult counts one cleanup run.
type CleanupResult struct {
	Deleted int64
	Batches int64
}

// Cleanup deletes outbox_events rows older than Retention (by now, the
// application clock) in batches, raising outbox_watermark as it goes. It
// stops at maxBatches (the next run continues) or when ctx ends.
func Cleanup(ctx context.Context, pool *pgxpool.Pool, now time.Time, maxBatches int) (CleanupResult, error) {
	var res CleanupResult
	cutoff := now.Add(-Retention)
	for res.Batches < int64(maxBatches) {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		var n int64
		if err := pool.QueryRow(ctx, cleanupSQL, cutoff, cleanupBatch, now).Scan(&n); err != nil {
			return res, fmt.Errorf("realtime: outbox cleanup: %w", err)
		}
		res.Batches++
		res.Deleted += n
		if n < cleanupBatch {
			break
		}
	}
	return res, nil
}
