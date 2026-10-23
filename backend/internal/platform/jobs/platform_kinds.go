package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Platform kind names.
const (
	KindHeartbeat   = "platform_heartbeat"
	KindMaintenance = "platform_maintenance"
	KindWatchdog    = "jobs_watchdog"
	KindRetention   = "job_runs_retention"
)

// PlatformDeps are what the platform kinds need.
type PlatformDeps struct {
	Pool     *pgxpool.Pool
	Clock    clock.Clock
	Settings *settings.Cache
	Log      *slog.Logger
}

// Watchdog tuning (D-95): a scheduled kind is missed when its tick has had
// twice its interval, or 15 minutes for a daily or longer kind, without a
// run; a running row older than its kind's Timeout plus abandonGrace with no
// live River job is closed as failed.
const (
	missedFactor      = 2
	dailyMissedGrace  = 15 * time.Minute
	abandonGrace      = 5 * time.Minute
	watchdogLockKey   = "tz_jobs:watchdog"
	abandonedErrorMsg = "abandoned: the worker stopped without finishing the run"
)

// DefinePlatform declares the platform's own kinds in r: the heartbeat, the
// partition and idempotency maintenance (W0-03's periodic jobs, now kinds),
// jobs_watchdog and job_runs_retention.
func DefinePlatform(r *Registry, d PlatformDeps) {
	Define(r, Kind{
		Name: KindHeartbeat, Owner: "W0-03", Class: ClassMaintenance,
		Description: "No-op proof that the worker picks up and runs scheduled kinds",
		Schedule:    "*/5 * * * *", UniqueBy: PerTick, Timeout: 10 * time.Second, MaxAttempts: 1,
		Handler: func(context.Context, Run) (Result, error) { return Result{}, nil },
	})
	Define(r, Kind{
		Name: KindMaintenance, Owner: "W0-03", Class: ClassMaintenance,
		Description: "Creates partitions ahead for every partition_policies table and purges expired idempotency keys",
		Schedule:    "0 */6 * * *", UniqueBy: PerTick, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, run Run) (Result, error) { return maintenance(ctx, d) },
	})
	Define(r, Kind{
		Name: KindWatchdog, Owner: "W1-11", Class: ClassMaintenance,
		Description: "Flags scheduled kinds that missed a period and closes abandoned running rows",
		Schedule:    "*/5 * * * *", UniqueBy: PerTick, Timeout: 60 * time.Second, MaxAttempts: 1,
		Handler: func(ctx context.Context, _ Run) (Result, error) { return watchdog(ctx, r, d) },
	})
	Define(r, Kind{
		Name: KindRetention, Owner: "W1-11", Class: ClassMaintenance,
		Description: "Drops job_runs partitions older than job_runs_retention_days",
		Schedule:    "30 3 * * *", UniqueBy: PerDay, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ Run) (Result, error) { return retention(ctx, d) },
	})
}

func needPool(d PlatformDeps) error {
	if d.Pool == nil || d.Clock == nil {
		return errors.New("jobs: platform kind needs Pool and Clock")
	}
	return nil
}

// maintenance keeps every partition_policies table partitioned ahead (no Go
// list of tables, D-50) and purges expired idempotency keys.
func maintenance(ctx context.Context, d PlatformDeps) (Result, error) {
	if err := needPool(d); err != nil {
		return Result{}, err
	}
	log := telemetry.LoggerOr(ctx, d.Log)
	now := d.Clock.Now()
	rows, err := d.Pool.Query(ctx, `SELECT p.parent, public.ensure_partitions(p.parent, $1)
		FROM public.partition_policies p ORDER BY p.parent`, now)
	if err != nil {
		return Result{}, fmt.Errorf("maintenance: partitions: %w", err)
	}
	var created int64
	for rows.Next() {
		var (
			table string
			n     int
		)
		if err := rows.Scan(&table, &n); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("maintenance: partitions: %w", err)
		}
		if n > 0 {
			log.InfoContext(ctx, "partitions created", slog.String("table", table), slog.Int("count", n))
		}
		created += int64(n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("maintenance: partitions: %w", err)
	}
	purged, err := idempotency.NewPurger(d.Pool).Purge(ctx, now)
	if err != nil {
		return Result{}, fmt.Errorf("maintenance: idempotency purge: %w", err)
	}
	return Result{Summary: map[string]int64{"partitions_created": created, "idempotency_keys_purged": purged}}, nil
}

// retention drops job_runs partitions that ended more than
// job_runs_retention_days ago (SC-JB-12). The cut-off is taken from the
// earlier of the injected clock and the database clock: drop_old_partitions
// refuses a cut-off newer than its floor by the database clock, and a test
// clock ahead of it must not make the job fail.
func retention(ctx context.Context, d PlatformDeps) (Result, error) {
	if err := needPool(d); err != nil {
		return Result{}, err
	}
	if d.Settings == nil {
		return Result{}, errors.New("job_runs_retention: no settings")
	}
	keep, err := d.Settings.Duration(ctx, settings.JobRunsRetentionDays)
	if err != nil {
		return Result{}, fmt.Errorf("job_runs_retention: %w", err)
	}
	var dbNow time.Time
	if err := d.Pool.QueryRow(ctx, `SELECT now()`).Scan(&dbNow); err != nil {
		return Result{}, fmt.Errorf("job_runs_retention: %w", err)
	}
	now := d.Clock.Now()
	if dbNow.Before(now) {
		now = dbNow
	}
	before := now.Add(-keep)
	var dropped int64
	if err := d.Pool.QueryRow(ctx, `SELECT drop_old_partitions('job_runs', $1)`, before).Scan(&dropped); err != nil {
		return Result{}, fmt.Errorf("job_runs_retention: %w", err)
	}
	return Result{Summary: map[string]int64{"partitions_dropped": dropped}}, nil
}

// missedAllowance is how long after a tick a scheduled kind has to run it.
func missedAllowance(interval time.Duration) time.Duration {
	if interval >= 24*time.Hour {
		return dailyMissedGrace
	}
	return missedFactor * interval
}

// watchdog (D-95, SC-JB-05) closes abandoned running rows, then, for every
// scheduled kind that is not paused, writes one `missed` row for the latest
// period whose allowance has passed without a run (and was not inside a
// pause window), and counts the kinds whose missed row is newer than their
// latest success. tz_job_missed{kind} reports the same from the database in
// every worker (monitor.current); the watchdog never sets it inside its tx.
func watchdog(ctx context.Context, r *Registry, d PlatformDeps) (Result, error) {
	if err := needPool(d); err != nil {
		return Result{}, err
	}
	now := d.Clock.Now()
	res := Result{Summary: map[string]int64{"abandoned": 0, "missed_new": 0, "missed_now": 0}}
	err := pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		var got bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext($1))`, watchdogLockKey).Scan(&got); err != nil {
			return err
		}
		if !got {
			res.Note = "another watchdog run holds the lock"
			return nil
		}
		controls, err := pausedKinds(ctx, tx)
		if err != nil {
			return err
		}
		baseline, err := watchdogBaseline(ctx, tx)
		if err != nil {
			return err
		}
		for _, k := range r.Kinds() {
			n, err := closeAbandoned(ctx, tx, k, now)
			if err != nil {
				return err
			}
			res.Summary["abandoned"] += n
			sched, ok := r.ScheduleOf(k.Name)
			if !ok {
				continue
			}
			c := controls[k.Name]
			if k.Class != ClassTimer && c.paused {
				continue // a paused kind is not flagged (and its gauge reads 0)
			}
			isNew, err := checkMissed(ctx, tx, r, k, sched, now, baseline, c)
			if err != nil {
				return err
			}
			if isNew {
				res.Summary["missed_new"]++
			}
			missed, err := missedNow(ctx, tx, k.Name)
			if err != nil {
				return err
			}
			if missed {
				res.Summary["missed_now"]++
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("jobs_watchdog: %w", err)
	}
	// Every worker's gauge reads the database (monitor.current); this one
	// reads it again now that the watchdog's rows are committed.
	r.monitor().invalidate()
	return res, nil
}

// control is a kind's job_controls row: whether it is paused now, and its
// latest pause window (pausedAt to resumedAt; resumedAt nil while paused).
type control struct {
	paused              bool
	pausedAt, resumedAt *time.Time
}

// pausedDuring: the kind was paused at some point in [from, to] by its
// latest pause window (only the latest window is kept).
func (c control) pausedDuring(from, to time.Time) bool {
	if c.pausedAt == nil || c.pausedAt.After(to) {
		return false
	}
	return c.resumedAt == nil || !c.resumedAt.Before(from)
}

func pausedKinds(ctx context.Context, tx pgx.Tx) (map[string]control, error) {
	rows, err := tx.Query(ctx, `SELECT kind, paused, paused_at, resumed_at FROM job_controls`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]control{}
	for rows.Next() {
		var (
			kind string
			c    control
		)
		if err := rows.Scan(&kind, &c.paused, &c.pausedAt, &c.resumedAt); err != nil {
			return nil, err
		}
		out[kind] = c
	}
	return out, rows.Err()
}

// watchdogBaseline is when the jobs platform first ran here: the watchdog's
// earliest row. A tick before it is never reported missed (a fresh database
// or a newly deployed platform has no history to miss).
func watchdogBaseline(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var t *time.Time
	if err := tx.QueryRow(ctx, `SELECT min(created_at) FROM job_runs WHERE kind = $1`, KindWatchdog).Scan(&t); err != nil {
		return time.Time{}, err
	}
	if t == nil {
		return time.Time{}, nil
	}
	return *t, nil
}

// closeAbandoned marks k's running rows that are older than its Timeout plus
// abandonGrace and whose River job is no longer running as failed.
func closeAbandoned(ctx context.Context, tx pgx.Tx, k Kind, now time.Time) (int64, error) {
	cutoff := now.Add(-(k.Timeout + abandonGrace))
	tag, err := tx.Exec(ctx, `UPDATE job_runs jr SET status = 'failed', finished_at = $3, error = $4
		WHERE jr.kind = $1 AND jr.status = 'running' AND jr.started_at < $2
		  AND NOT EXISTS (SELECT 1 FROM river_job j WHERE j.id = jr.river_job_id AND j.state = 'running')`,
		k.Name, cutoff, now, abandonedErrorMsg)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// checkMissed writes a missed row for k's latest tick whose allowance has
// passed, if that period (or a later one) has no run and no missed row yet.
// A tick inside a pause window (paused at the tick or during its allowance,
// then resumed) is not missed: the scheduler did not enqueue it, by design.
// A skipped row counts as a run: it is either a duplicate of a succeeded
// period or the scheduler's first-seen marker for a kind with no history.
func checkMissed(ctx context.Context, tx pgx.Tx, r *Registry, k Kind, sched *Schedule, now, baseline time.Time, c control) (bool, error) {
	if baseline.IsZero() {
		return false, nil
	}
	allow := missedAllowance(sched.Interval(now))
	p := sched.Prev(now.Add(-allow))
	if p.IsZero() || p.Before(baseline) {
		return false, nil
	}
	if k.Class != ClassTimer && c.pausedDuring(p, p.Add(allow)) {
		return false, nil
	}
	key := k.UniqueBy.Key(p)
	var ran bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_runs WHERE kind = $1 AND created_at >= $2
		AND status IN ('succeeded', 'running', 'yielded', 'skipped') AND (period_key IS NULL OR period_key >= $3))`,
		k.Name, p, key).Scan(&ran); err != nil {
		return false, err
	}
	if ran {
		return false, nil
	}
	var flagged bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_runs WHERE kind = $1 AND period_key = $2 AND status = 'missed')`,
		k.Name, key).Scan(&flagged); err != nil {
		return false, err
	}
	if flagged {
		return false, nil
	}
	row := &runRow{id: ids.New(now), kind: k.Name, class: k.Class, trigger: TriggerSchedule, periodKey: key,
		status: StatusMissed, scheduledAt: p, createdAt: now}
	a, err := row.args()
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, insertRunSQL, a...); err != nil {
		return false, err
	}
	r.monitor().recordMissed(ctx, k)
	telemetry.Logger(ctx).WarnContext(ctx, "scheduled job missed a period",
		slog.String("missed_kind", k.Name), slog.String("period", key))
	return true, nil
}

// missedNow: a missed row is newer than the kind's latest success.
func missedNow(ctx context.Context, tx pgx.Tx, kind string) (bool, error) {
	var v bool
	err := tx.QueryRow(ctx, `SELECT
		COALESCE((SELECT max(created_at) FROM job_runs WHERE kind = $1 AND status = 'missed'), '-infinity')
		> COALESCE((SELECT max(COALESCE(finished_at, created_at)) FROM job_runs WHERE kind = $1 AND status = 'succeeded'), '-infinity')`,
		kind).Scan(&v)
	return v, err
}
