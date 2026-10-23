package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func insertRow(t *testing.T, ctx context.Context, db *testkit.DB, row runRow) {
	t.Helper()
	if row.id == (ids.ID{}) {
		row.id = ids.New(row.createdAt)
	}
	a, err := row.args()
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, insertRunSQL, a...)
	require.NoError(t, err)
}

// TestWatchdogFlagsMissedPeriods is SC-JB-05, plus the abandoned-run sweep.
func TestWatchdogFlagsMissedPeriods(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := testkit.NewDB(t)
	day := clock.Real{}.Now().In(clock.IST).AddDate(0, 0, 2) // a day with no other rows
	clk := clock.NewFake(ist(day.Year(), day.Month(), day.Day(), 8, 0))

	reg := NewRegistry()
	for _, name := range []string{"rollup_daily", "rollup_paused"} {
		Define(reg, Kind{Name: name, Owner: "W3-07", Description: "Daily rollup.", Class: ClassRollup,
			Schedule: "0 9 * * *", UniqueBy: PerDay, Timeout: 10 * time.Second, MaxAttempts: 3, Handler: noop})
	}
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(ctx) }()
	run, err := NewRunner(reg, RunnerOptions{Pool: db.Pool, Clock: clk, Log: quiet(), MeterProvider: mp})
	require.NoError(t, err)
	defer run.Close()
	h := &harness{t: t, ctx: ctx, db: db, reg: reg, run: run, reader: reader}
	d := PlatformDeps{Pool: db.Pool, Clock: clk, Log: quiet()}
	wd := func() Result {
		res, err := watchdog(ctx, reg, d)
		require.NoError(t, err)
		return res
	}

	// No watchdog history: nothing is reported.
	clk.Set(ist(day.Year(), day.Month(), day.Day(), 9, 30))
	require.Zero(t, wd().Summary["missed_new"])
	clk.Set(ist(day.Year(), day.Month(), day.Day(), 8, 0))

	// The platform has been running since 08:00 (the watchdog's first row);
	// a run abandoned at 08:30 by a crashed worker is still "running".
	insertRow(t, ctx, db, runRow{kind: KindWatchdog, class: ClassMaintenance, trigger: TriggerSchedule, attempt: 1,
		status: StatusSucceeded, createdAt: clk.Now(), startedAt: clk.Now()})
	_, err = db.Pool.Exec(ctx, `INSERT INTO job_controls (kind, paused, reason) VALUES ('rollup_paused', true, 'test')`)
	require.NoError(t, err)
	at830 := ist(day.Year(), day.Month(), day.Day(), 8, 30)
	insertRow(t, ctx, db, runRow{kind: "rollup_daily", class: ClassRollup, trigger: TriggerManual, attempt: 1,
		status: StatusRunning, createdAt: at830, startedAt: at830})

	// 09:10: inside the 15-minute allowance; the abandoned row is closed.
	clk.Set(ist(day.Year(), day.Month(), day.Day(), 9, 10))
	res := wd()
	require.Zero(t, res.Summary["missed_new"])
	require.Equal(t, int64(1), res.Summary["abandoned"])
	require.Equal(t, 1, h.count(`SELECT count(*) FROM job_runs WHERE kind = 'rollup_daily' AND status = 'failed' AND error LIKE 'abandoned%'`))

	// 09:16: the 09:00 period never ran.
	clk.Set(ist(day.Year(), day.Month(), day.Day(), 9, 16))
	res = wd()
	require.Equal(t, int64(1), res.Summary["missed_new"])
	period := PerDay.Key(ist(day.Year(), day.Month(), day.Day(), 9, 0))
	require.Equal(t, 1, h.count(`SELECT count(*) FROM job_runs WHERE kind = 'rollup_daily' AND status = 'missed'
		AND period_key = $1 AND attempt = 0 AND trigger = 'schedule'`, period))
	require.Equal(t, int64(1), h.missedGauge("rollup_daily"))
	require.Equal(t, int64(1), h.runsTotal("rollup_daily", StatusMissed))
	require.Equal(t, []string{"rollup_daily"}, reg.MissedKinds())

	// A paused kind is not flagged.
	require.Zero(t, h.count(`SELECT count(*) FROM job_runs WHERE kind = 'rollup_paused'`))
	require.Equal(t, int64(0), h.missedGauge("rollup_paused"))

	// Further watchdog ticks in the same period write no second row.
	for _, mm := range []int{20, 25, 50} {
		clk.Set(ist(day.Year(), day.Month(), day.Day(), 9, mm))
		require.Zero(t, wd().Summary["missed_new"])
	}
	require.Equal(t, 1, h.count(`SELECT count(*) FROM job_runs WHERE kind = 'rollup_daily' AND status = 'missed'`))
	require.Equal(t, int64(1), h.missedGauge("rollup_daily"))

	// After the next succeeded run the gauge is 0 again.
	at955 := ist(day.Year(), day.Month(), day.Day(), 9, 55)
	insertRow(t, ctx, db, runRow{kind: "rollup_daily", class: ClassRollup, trigger: TriggerManual, periodKey: period,
		attempt: 1, status: StatusSucceeded, createdAt: at955, startedAt: at955, finishedAt: at955})
	clk.Set(ist(day.Year(), day.Month(), day.Day(), 10, 0))
	require.Equal(t, int64(0), wd().Summary["missed_now"])
	require.Equal(t, int64(0), h.missedGauge("rollup_daily"))
	require.Empty(t, reg.MissedKinds())
}

func TestMissedAllowance(t *testing.T) {
	require.Equal(t, 10*time.Minute, missedAllowance(5*time.Minute))
	require.Equal(t, 12*time.Hour, missedAllowance(6*time.Hour))
	require.Equal(t, 15*time.Minute, missedAllowance(24*time.Hour))
	require.Equal(t, 15*time.Minute, missedAllowance(7*24*time.Hour))
}

// partitionEnds lists job_runs' partition upper bounds (not the default).
func partitionEnds(t *testing.T, ctx context.Context, db *testkit.DB) []time.Time {
	t.Helper()
	rows, err := db.OwnerPool.Query(ctx, `SELECT (regexp_match(pg_get_expr(c.relpartbound, c.oid), 'TO \(''([^'']+)''\)'))[1]::timestamptz
		FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'job_runs'::regclass AND c.relname <> 'job_runs_default' ORDER BY 1`)
	require.NoError(t, err)
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var hi time.Time
		require.NoError(t, rows.Scan(&hi))
		out = append(out, hi)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestRetentionDropsOldPartitions is SC-JB-12.
func TestRetentionDropsOldPartitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := testkit.NewDB(t)
	_, err := db.OwnerPool.Exec(ctx, `SELECT ensure_partitions('job_runs', now() - interval '200 days'),
		ensure_partitions('job_runs', now() - interval '60 days')`)
	require.NoError(t, err)
	st := settings.NewCache(settings.PGLoader(db.Pool), settings.Options{})
	d := PlatformDeps{Pool: db.Pool, Clock: clock.NewFake(clock.Real{}.Now()), Settings: st, Log: quiet()}

	check := func(days int) int64 {
		t.Helper()
		var dbNow time.Time
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT now()`).Scan(&dbNow))
		cut := dbNow.AddDate(0, 0, -days)
		var older int64
		for _, hi := range partitionEnds(t, ctx, db) {
			if !hi.After(cut) {
				older++
			}
		}
		res, err := retention(ctx, d)
		require.NoError(t, err)
		require.Equal(t, older, res.Summary["partitions_dropped"])
		ends := partitionEnds(t, ctx, db)
		require.NotEmpty(t, ends, "newer partitions stay")
		for _, hi := range ends {
			require.True(t, hi.After(cut.Add(-time.Minute)), "partition ending %s is past the %d-day cut-off", hi, days)
		}
		return older
	}
	require.Positive(t, check(90), "the 200-day-old months go")

	// The setting changes the cut-off without a deploy.
	_, err = db.OwnerPool.Exec(ctx, `UPDATE platform_settings SET value = '30' WHERE key = 'job_runs_retention_days'`)
	require.NoError(t, err)
	st.Invalidate()
	require.Positive(t, check(30), "the 60-day-old month goes once the setting is 30")

	// The function refuses a table without a policy, and a cut-off inside
	// the floor.
	_, err = db.Pool.Exec(ctx, `SELECT drop_old_partitions('platform_settings', now() - interval '1 year')`)
	require.ErrorContains(t, err, "no row in partition_policies")
	_, err = db.Pool.Exec(ctx, `SELECT drop_old_partitions('job_runs', now())`)
	require.ErrorContains(t, err, "too recent")
	// todayz_app cannot DROP or DELETE job_runs itself.
	_, err = db.Pool.Exec(ctx, `DELETE FROM job_runs`)
	require.Error(t, err)
}

// TestMaintenanceCreatesPartitionsAhead: the W0-03 maintenance job, now the
// platform_maintenance kind, keeps partitions ahead of the clock.
func TestMaintenanceCreatesPartitionsAhead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := testkit.NewDB(t)
	future := clock.Real{}.Now().AddDate(2, 0, 0)
	res, err := maintenance(ctx, PlatformDeps{Pool: db.Pool, Clock: clock.NewFake(future), Log: quiet()})
	require.NoError(t, err)
	require.Positive(t, res.Summary["partitions_created"])
	var n int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relname = $1`,
		"job_runs_p"+future.UTC().Format("200601")).Scan(&n))
	require.Equal(t, 1, n)
	res, err = maintenance(ctx, PlatformDeps{Pool: db.Pool, Clock: clock.NewFake(future), Log: quiet()})
	require.NoError(t, err)
	require.Zero(t, res.Summary["partitions_created"], "idempotent")
}
