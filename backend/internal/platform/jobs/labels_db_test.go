package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

// TestEveryKindCarriesOnlyAllowedLabels is SC-JB-13 over a whole registry:
// the platform kinds plus one kind of every class, each run once. Every
// tz_job_* series (runs, duration, lateness, missed, hard-cancelled) carries
// only kind, class and status; kind is a registry name, class and status are
// known values; a River job of a kind outside the registry never shows up.
func TestEveryKindCarriesOnlyAllowedLabels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := testkit.NewDB(t)
	clk := clock.NewOffset(nil)
	st := settings.NewCache(settings.PGLoader(db.Pool), settings.Options{})

	reg := NewRegistry()
	DefinePlatform(reg, PlatformDeps{Pool: db.Pool, Clock: clk, Settings: st, Log: quiet()})
	ok := func(context.Context, Run) (Result, error) { return Result{Summary: map[string]int64{"rows": 1}}, nil }
	for _, k := range []Kind{
		{Name: "order_accept_expiry", Class: ClassTimer, Runbook: "docs/runbooks/orders-accept-by.md"},
		{Name: "ledger_reconcile", Class: ClassMoney, Runbook: "docs/runbooks/payouts-ledger.md", Schedule: "30 2 * * *", UniqueBy: PerDay},
		{Name: "notify_push", Class: ClassNotify},
		{Name: "offers_broadcast", Class: ClassMarketing},
		{Name: "coach_nudge_draft", Class: ClassAI},
		{Name: "rollup_today", Class: ClassRollup, Schedule: "*/5 * * * *", UniqueBy: PerTick},
	} {
		k.Owner, k.Description, k.Timeout, k.MaxAttempts, k.Handler = "W1-11", "Label test kind.", 10*time.Second, 1, ok
		Define(reg, k)
	}

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	tp := sdktrace.NewTracerProvider()
	run, err := NewRunner(reg, RunnerOptions{Pool: db.Pool, Clock: clk, Log: quiet(), MeterProvider: mp, TracerProvider: tp})
	require.NoError(t, err)
	q := queue.NewRegistry()
	run.Register(q)
	c, err := queue.NewWorkerClient(db.Pool, q, queue.Options{Log: quiet(), Time: clk, MeterProvider: mp})
	require.NoError(t, err)
	require.NoError(t, queue.Start(ctx, c))
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Stop(stop)
		run.Close()
		_ = tp.Shutdown(stop)
		_ = mp.Shutdown(stop)
	}()
	h := &harness{t: t, ctx: ctx, db: db, reg: reg, run: run, reader: reader, clk: clk}

	// One run of every kind: a manual run of a past tick for scheduled kinds,
	// an event for the rest.
	now := clk.Now()
	for _, k := range reg.Kinds() {
		e := envelope{kind: k.Name, T: TriggerEvent, S: now}
		if k.Scheduled() {
			s, found := reg.ScheduleOf(k.Name)
			require.True(t, found)
			tick := s.Next(now.Add(-25 * time.Hour))
			id := ids.New(now)
			e = envelope{kind: k.Name, U: "manual:" + id.String(), P: k.UniqueBy.Key(tick), T: TriggerManual, S: tick, R: id.String()}
		}
		_, err := c.Insert(ctx, e, nil)
		require.NoError(t, err, k.Name)
	}
	// A River job whose kind no worker knows: River fails it without our
	// middleware, so its name never reaches a label.
	_, err = db.Pool.Exec(ctx, `INSERT INTO river_job (kind, args, max_attempts, queue, state, scheduled_at, priority)
		VALUES ('not_a_registered_kind', '{}', 1, 'default', 'available', now() - interval '1 second', 1)`)
	require.NoError(t, err)

	h.waitCount(len(reg.Kinds()), `SELECT count(DISTINCT kind)::int FROM job_stats_hourly
		WHERE succeeded + failed + cancelled + discarded + skipped + yielded > 0`)
	h.waitCount(1, `SELECT count(*)::int FROM river_job WHERE kind = 'not_a_registered_kind' AND attempt > 0`)
	require.Zero(t, h.count(`SELECT count(*)::int FROM job_runs WHERE kind = 'not_a_registered_kind'`))
	require.Zero(t, h.count(`SELECT count(*)::int FROM job_stats_hourly WHERE kind <> ALL($1)`, reg.Names()))

	names := map[string]bool{}
	for _, n := range reg.Names() {
		names[n] = true
	}
	classes := map[string]bool{}
	for _, cl := range Classes() {
		classes[string(cl)] = true
	}
	statuses := map[string]bool{}
	for _, s := range Statuses() {
		statuses[string(s)] = true
	}
	allowed := map[attribute.Key]map[string]bool{"kind": names, "class": classes, "status": statuses}

	series := h.jobMetrics()
	for _, m := range []string{MetricRuns, MetricDuration, MetricLateness, MetricMissed, queue.HardCancelledMetric} {
		require.NotEmpty(t, series[m], m)
	}
	seen := map[string]bool{}
	for m, sets := range series {
		for _, set := range sets {
			for _, kv := range set.ToSlice() {
				values, known := allowed[kv.Key]
				require.True(t, known, "%s has label %s", m, kv.Key)
				require.True(t, values[kv.Value.AsString()], "%s %s=%q is not an allowed value", m, kv.Key, kv.Value.AsString())
				if kv.Key == "kind" && m == MetricRuns {
					seen[kv.Value.AsString()] = true
				}
			}
		}
	}
	for _, n := range reg.Names() {
		require.True(t, seen[n], "tz_job_runs_total has no series for %s", n)
	}
	// The scheduled kinds carry tz_job_missed; event kinds do not (the
	// alerts tell them apart with "unless on (kind) tz_job_missed").
	for _, k := range reg.Kinds() {
		want := int64(-1)
		if k.Scheduled() {
			want = 0
		}
		require.Equal(t, want, h.missedGauge(k.Name), k.Name)
	}
}
