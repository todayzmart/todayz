package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// HeartbeatArgs is the no-op periodic job: it proves the worker picks up
// and works periodic jobs (W0-03).
type HeartbeatArgs struct{}

// Kind implements river.JobArgs.
func (HeartbeatArgs) Kind() string { return "platform.heartbeat" }

// HeartbeatWorker does nothing but count its runs.
type HeartbeatWorker struct {
	river.WorkerDefaults[HeartbeatArgs]
	Runs atomic.Int64
}

// Work implements river.Worker.
func (w *HeartbeatWorker) Work(context.Context, *river.Job[HeartbeatArgs]) error {
	w.Runs.Add(1)
	return nil
}

// MaintenanceArgs runs the platform housekeeping.
type MaintenanceArgs struct{}

// Kind implements river.JobArgs.
func (MaintenanceArgs) Kind() string { return "platform.maintenance" }

// Purger deletes expired idempotency keys.
type Purger interface {
	Purge(ctx context.Context, now time.Time) (int64, error)
}

// MaintenanceWorker keeps every table listed in partition_policies
// partitioned ahead (a domain WP adds its partitioned table with a row in its
// own migration, so no list lives in Go) and purges expired idempotency keys.
type MaintenanceWorker struct {
	river.WorkerDefaults[MaintenanceArgs]
	Pool        *pgxpool.Pool
	Clock       clock.Clock
	Idempotency Purger
	Log         *slog.Logger
}

// Work implements river.Worker.
func (w *MaintenanceWorker) Work(ctx context.Context, _ *river.Job[MaintenanceArgs]) error {
	now := w.Clock.Now()
	rows, err := w.Pool.Query(ctx, `SELECT p.parent, public.ensure_partitions(p.parent, $1)
		FROM public.partition_policies p ORDER BY p.parent`, now)
	if err != nil {
		return fmt.Errorf("maintenance: partitions: %w", err)
	}
	for rows.Next() {
		var (
			table   string
			created int
		)
		if err := rows.Scan(&table, &created); err != nil {
			rows.Close()
			return fmt.Errorf("maintenance: partitions: %w", err)
		}
		if created > 0 {
			w.Log.InfoContext(ctx, "partitions created", slog.String("table", table), slog.Int("count", created))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("maintenance: partitions: %w", err)
	}
	if w.Idempotency != nil {
		n, err := w.Idempotency.Purge(ctx, now)
		if err != nil {
			return err
		}
		w.Log.DebugContext(ctx, "idempotency keys purged", slog.Int64("count", n))
	}
	return nil
}

// PlatformDeps are what the platform jobs need.
type PlatformDeps struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
	Log   *slog.Logger
}

// RegisterPlatform adds the heartbeat (every minute) and maintenance (every
// 6 h, and at start) jobs. It returns the heartbeat worker for tests.
func RegisterPlatform(r *Registry, d PlatformDeps) *HeartbeatWorker {
	if d.Log == nil {
		d.Log = telemetry.Default()
	}
	hb := &HeartbeatWorker{}
	Add[HeartbeatArgs](r, hb)
	Add[MaintenanceArgs](r, &MaintenanceWorker{Pool: d.Pool, Clock: d.Clock, Idempotency: idempotency.NewPurger(d.Pool), Log: d.Log})
	r.Every(time.Minute, func() river.JobArgs { return HeartbeatArgs{} })
	r.Every(6*time.Hour, func() river.JobArgs { return MaintenanceArgs{} })
	return hb
}
