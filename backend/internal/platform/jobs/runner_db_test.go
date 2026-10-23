package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// harness is a worker on a fresh database: a registry, the runner, a River
// worker client on the local/test Offset clock, a metric reader and a
// tracer that samples everything.
type harness struct {
	t        *testing.T
	ctx      context.Context
	db       *testkit.DB
	reg      *Registry
	run      *Runner
	c        *queue.Client
	clk      *clock.Offset
	reader   *sdkmetric.ManualReader
	draining atomic.Bool
}

func newHarness(t *testing.T, define func(r *Registry)) *harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	h := &harness{t: t, ctx: ctx, db: testkit.NewDB(t), reg: NewRegistry(), clk: clock.NewOffset(nil)}
	define(h.reg)
	h.reader = sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(h.reader))
	tp := sdktrace.NewTracerProvider()
	var err error
	h.run, err = NewRunner(h.reg, RunnerOptions{
		Pool: h.db.Pool, Clock: h.clk, Log: quiet(), Draining: h.draining.Load,
		MeterProvider: mp, TracerProvider: tp, SchedulerPoll: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	q := queue.NewRegistry()
	h.run.Register(q)
	h.c, err = queue.NewWorkerClient(h.db.Pool, q, queue.Options{Log: quiet(), Time: h.clk, MeterProvider: mp})
	require.NoError(t, err)
	require.NoError(t, queue.Start(ctx, h.c))
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.c.Stop(stop)
		h.run.Close()
		_ = tp.Shutdown(stop)
		_ = mp.Shutdown(stop)
	})
	return h
}

// atTick moves the clock to one second past kind's next tick and returns
// the tick.
func (h *harness) atTick(kind string) time.Time {
	s, ok := h.reg.ScheduleOf(kind)
	require.True(h.t, ok)
	tick := s.Next(h.clk.Now())
	h.clk.Set(tick.Add(time.Second))
	return tick
}

func (h *harness) count(sql string, args ...any) int {
	h.t.Helper()
	var n int
	require.NoError(h.t, h.db.Pool.QueryRow(h.ctx, sql, args...).Scan(&n))
	return n
}

func (h *harness) waitCount(want int, sql string, args ...any) {
	h.t.Helper()
	require.Eventually(h.t, func() bool {
		var n int
		if err := h.db.Pool.QueryRow(h.ctx, sql, args...).Scan(&n); err != nil {
			return false
		}
		return n == want
	}, 45*time.Second, 50*time.Millisecond, "waiting for %d from %s", want, sql)
}

// jobMetrics returns every tz_job_* data point's attributes, by metric.
func (h *harness) jobMetrics() map[string][]attribute.Set {
	h.t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(h.t, h.reader.Collect(h.ctx, &rm))
	out := map[string][]attribute.Set{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if !strings.HasPrefix(m.Name, "tz_job_") {
				continue
			}
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, p := range d.DataPoints {
					out[m.Name] = append(out[m.Name], p.Attributes)
				}
			case metricdata.Gauge[int64]:
				for _, p := range d.DataPoints {
					out[m.Name] = append(out[m.Name], p.Attributes)
				}
			case metricdata.Histogram[float64]:
				for _, p := range d.DataPoints {
					out[m.Name] = append(out[m.Name], p.Attributes)
				}
			case metricdata.ExponentialHistogram[float64]:
				for _, p := range d.DataPoints {
					out[m.Name] = append(out[m.Name], p.Attributes)
				}
			}
		}
	}
	return out
}

// runsTotal is tz_job_runs_total{kind, status}.
func (h *harness) runsTotal(kind string, s Status) int64 {
	h.t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(h.t, h.reader.Collect(h.ctx, &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != MetricRuns {
				continue
			}
			for _, p := range m.Data.(metricdata.Sum[int64]).DataPoints {
				k, _ := p.Attributes.Value("kind")
				st, _ := p.Attributes.Value("status")
				if k.AsString() == kind && st.AsString() == string(s) {
					return p.Value
				}
			}
		}
	}
	return 0
}

// missedGauge is tz_job_missed{kind} (-1 when the series is absent).
func (h *harness) missedGauge(kind string) int64 {
	h.t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(h.t, h.reader.Collect(h.ctx, &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != MetricMissed {
				continue
			}
			for _, p := range m.Data.(metricdata.Gauge[int64]).DataPoints {
				if k, _ := p.Attributes.Value("kind"); k.AsString() == kind {
					return p.Value
				}
			}
		}
	}
	return -1
}

var reTraceID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestScheduledRunIsLogged is SC-JB-02, the UniqueBy half of SC-JB-11 and
// SC-JB-13: a scheduled tick runs once and is logged with its summary,
// timings and trace; a second job for the same period is skipped; the
// tz_job_* series carry only kind, class and status, with registry kinds.
func TestScheduledRunIsLogged(t *testing.T) {
	var calls atomic.Int64
	h := newHarness(t, func(r *Registry) {
		k := validScheduled()
		k.Handler = func(context.Context, Run) (Result, error) {
			calls.Add(1)
			return Result{Summary: map[string]int64{"rows": 120}, Note: "ok, mail me at ops@example.com"}, nil
		}
		Define(r, k)
	})
	tick := h.atTick("rollup_today")
	period := PerTick.Key(tick)

	n, err := h.run.EnqueueDue(h.ctx, h.c)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	h.waitCount(1, `SELECT count(*) FROM job_runs WHERE kind = 'rollup_today' AND status = 'succeeded'`)

	var (
		trigger, status, traceID, note string
		periodKey                      string
		attempt                        int
		durMS, lateMS                  *int64
		summary                        []byte
		scheduledAt                    time.Time
		riverID                        *int64
	)
	require.NoError(t, h.db.Pool.QueryRow(h.ctx, `SELECT trigger, status, period_key, attempt, duration_ms, lateness_ms,
		summary, COALESCE(trace_id, ''), COALESCE(note, ''), scheduled_at, river_job_id FROM job_runs WHERE kind = 'rollup_today'`).
		Scan(&trigger, &status, &periodKey, &attempt, &durMS, &lateMS, &summary, &traceID, &note, &scheduledAt, &riverID))
	require.Equal(t, "schedule", trigger)
	require.Equal(t, "succeeded", status)
	require.Equal(t, period, periodKey)
	require.Equal(t, 1, attempt)
	require.NotNil(t, durMS)
	require.NotNil(t, lateMS)
	require.GreaterOrEqual(t, *lateMS, int64(1000), "started at least the 1 s after the tick the clock was set to")
	require.JSONEq(t, `{"rows":120}`, string(summary))
	require.Regexp(t, reTraceID, traceID)
	require.NotContains(t, note, "ops@example.com", "notes are redacted")
	require.True(t, scheduledAt.Equal(tick))
	require.NotNil(t, riverID)

	// The same tick is not enqueued twice, by this runner or another.
	n, err = h.run.EnqueueDue(h.ctx, h.c)
	require.NoError(t, err)
	require.Zero(t, n)
	other, err := NewRunner(h.reg, RunnerOptions{Pool: h.db.Pool, Clock: h.clk, Log: quiet()})
	require.NoError(t, err)
	n, err = other.EnqueueDue(h.ctx, h.c)
	require.NoError(t, err)
	require.Zero(t, n, "a second worker sees the period has run")
	other.Close()
	h.reg.setMonitor(h.run.mon)

	// A job for the same period that slips through anyway is skipped.
	_, err = h.c.Insert(h.ctx, envelope{kind: "rollup_today", U: period + "#9", P: period, T: TriggerSchedule, S: tick}, nil)
	require.NoError(t, err)
	h.waitCount(1, `SELECT count(*) FROM job_runs WHERE kind = 'rollup_today' AND status = 'skipped'`)
	require.Equal(t, int64(1), calls.Load(), "UniqueBy: a period runs at most once")
	require.Equal(t, 1, h.count(`SELECT succeeded::int FROM job_stats_hourly WHERE kind = 'rollup_today'`))
	require.Equal(t, 1, h.count(`SELECT skipped::int FROM job_stats_hourly WHERE kind = 'rollup_today'`))

	// SC-JB-13: only kind, class and status; only registry kinds.
	allowed := map[attribute.Key]bool{"kind": true, "class": true, "status": true}
	names := map[string]bool{}
	for _, n := range h.reg.Names() {
		names[n] = true
	}
	series := h.jobMetrics()
	for _, m := range []string{MetricRuns, MetricDuration, MetricLateness, MetricMissed} {
		require.NotEmpty(t, series[m], m)
		for _, set := range series[m] {
			for _, kv := range set.ToSlice() {
				require.True(t, allowed[kv.Key], "%s has label %s", m, kv.Key)
				if kv.Key == "kind" {
					require.True(t, names[kv.Value.AsString()], "%s kind %q is not a registry kind", m, kv.Value.AsString())
				}
			}
		}
	}
	require.Equal(t, int64(1), h.runsTotal("rollup_today", StatusSucceeded))
	require.Equal(t, int64(1), h.runsTotal("rollup_today", StatusSkipped))
}

// TestEventSuccessesAreCountedNotLogged is SC-JB-03.
func TestEventSuccessesAreCountedNotLogged(t *testing.T) {
	var calls atomic.Int64
	h := newHarness(t, func(r *Registry) {
		k := validEvent()
		k.Handler = func(_ context.Context, run Run) (Result, error) {
			var a struct{ I int }
			if err := run.Decode(&a); err != nil {
				return Result{}, err
			}
			calls.Add(1)
			return Result{}, nil
		}
		Define(r, k)
	})
	enq := NewEnqueuer(h.reg, h.c, h.clk)
	for i := range 100 {
		require.NoError(t, enq.EnqueueTx(h.ctx, nil, "orders_accept_by", map[string]int{"I": i}, EnqueueOpts{}))
	}
	h.waitCount(100, `SELECT COALESCE(sum(succeeded), 0)::int FROM job_stats_hourly WHERE kind = 'orders_accept_by'`)
	require.Equal(t, int64(100), calls.Load())
	require.Zero(t, h.count(`SELECT count(*) FROM job_runs WHERE kind = 'orders_accept_by'`), "event successes are not logged")
	require.Equal(t, int64(100), h.runsTotal("orders_accept_by", StatusSucceeded))

	// The success rate ignores skipped and yielded (D-95).
	_, err := h.db.Pool.Exec(h.ctx, `UPDATE job_stats_hourly SET skipped = 7, yielded = 3 WHERE kind = 'orders_accept_by'`)
	require.NoError(t, err)
	var rate float64
	require.NoError(t, h.db.Pool.QueryRow(h.ctx, `SELECT sum(succeeded)::float8 / NULLIF(sum(succeeded + failed + cancelled + discarded), 0)
		FROM job_stats_hourly WHERE kind = 'orders_accept_by'`).Scan(&rate))
	require.InDelta(t, 1.0, rate, 1e-9)

	// A unique key makes a second enqueue a no-op while the first waits.
	at := h.clk.Now().Add(time.Hour)
	for range 2 {
		require.NoError(t, enq.EnqueueTx(h.ctx, nil, "orders_accept_by", map[string]int{"I": 1}, EnqueueOpts{ScheduledAt: at, UniqueKey: "order:42"}))
	}
	require.Equal(t, 1, h.count(`SELECT count(*) FROM river_job WHERE kind = 'orders_accept_by' AND state = 'scheduled'`))
	require.ErrorIs(t, enq.EnqueueTx(h.ctx, nil, "rollup_nope", nil, EnqueueOpts{}), ErrUnknownKind)
}

// TestFailureIsScrubbedRetriedAndDiscarded is SC-JB-04.
func TestFailureIsScrubbedRetriedAndDiscarded(t *testing.T) {
	const phone, token = "98765 43210", "test-bearer-value-not-a-key"
	h := newHarness(t, func(r *Registry) {
		Define(r, Kind{
			Name: "notify_flaky", Owner: "W2-05", Description: "Always fails.", Class: ClassNotify,
			Timeout: 10 * time.Second, MaxAttempts: 2,
			Handler: func(context.Context, Run) (Result, error) {
				return Result{}, errors.New("send to +91 " + phone + " failed: Authorization: Bearer " + token + " " + strings.Repeat("é", 3000))
			},
		})
	})
	enq := NewEnqueuer(h.reg, h.c, h.clk)
	require.NoError(t, enq.EnqueueTx(h.ctx, nil, "notify_flaky", struct{}{}, EnqueueOpts{}))
	h.waitCount(1, `SELECT count(*) FROM job_runs WHERE kind = 'notify_flaky' AND status = 'failed' AND attempt = 1`)
	h.clk.Advance(time.Minute) // past River's retry backoff
	h.waitCount(1, `SELECT count(*) FROM job_runs WHERE kind = 'notify_flaky' AND status = 'discarded' AND attempt = 2`)

	rows, err := h.db.Pool.Query(h.ctx, `SELECT error, octet_length(error) FROM job_runs WHERE kind = 'notify_flaky'`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var e string
		var n int
		require.NoError(t, rows.Scan(&e, &n))
		require.LessOrEqual(t, n, 2048)
		require.NotContains(t, e, "43210")
		require.NotContains(t, e, token)
		require.Contains(t, e, "failed: Authorization")
	}
	require.NoError(t, rows.Err())
	var riverErrs string
	require.NoError(t, h.db.Pool.QueryRow(h.ctx, `SELECT errors::text FROM river_job WHERE kind = 'notify_flaky'`).Scan(&riverErrs))
	require.NotContains(t, riverErrs, "43210", "river_job.errors holds the scrubbed text")
	require.NotContains(t, riverErrs, token)
	require.Equal(t, int64(1), h.runsTotal("notify_flaky", StatusFailed))
	require.Equal(t, int64(1), h.runsTotal("notify_flaky", StatusDiscarded))
}

// TestPauseSkipsTicksAndSnoozesEvents is the runtime half of SC-JB-09.
func TestPauseSkipsTicksAndSnoozesEvents(t *testing.T) {
	var sched, event atomic.Int64
	h := newHarness(t, func(r *Registry) {
		k := validScheduled()
		k.Handler = func(context.Context, Run) (Result, error) { sched.Add(1); return Result{}, nil }
		Define(r, k)
		Define(r, Kind{Name: "notify_digest", Owner: "W2-05", Description: "Event kind.", Class: ClassNotify,
			Timeout: 10 * time.Second, MaxAttempts: 3,
			Handler: func(context.Context, Run) (Result, error) { event.Add(1); return Result{}, nil }})
	})
	_, err := h.db.Pool.Exec(h.ctx, `INSERT INTO job_controls (kind, paused, reason) VALUES ('rollup_today', true, 'test'), ('notify_digest', true, 'test')`)
	require.NoError(t, err)

	h.atTick("rollup_today")
	n, err := h.run.EnqueueDue(h.ctx, h.c)
	require.NoError(t, err)
	require.Zero(t, n, "a paused scheduled kind is not enqueued at its tick")
	require.Zero(t, h.count(`SELECT count(*) FROM river_job WHERE kind = 'rollup_today'`))

	require.NoError(t, NewEnqueuer(h.reg, h.c, h.clk).EnqueueTx(h.ctx, nil, "notify_digest", struct{}{}, EnqueueOpts{}))
	h.waitCount(1, `SELECT count(*) FROM river_job WHERE kind = 'notify_digest' AND state = 'scheduled' AND attempt = 0`)
	require.Zero(t, event.Load(), "a paused event kind snoozes")
	require.Zero(t, h.count(`SELECT count(*) FROM job_runs`))

	_, err = h.db.Pool.Exec(h.ctx, `UPDATE job_controls SET paused = false`)
	require.NoError(t, err)
	h.clk.Advance(2 * pausedSnooze)
	h.atTick("rollup_today") // the next tick, and past the snooze
	n, err = h.run.EnqueueDue(h.ctx, h.c)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	h.waitCount(1, `SELECT count(*) FROM job_runs WHERE kind = 'rollup_today' AND status = 'succeeded'`)
	require.Eventually(t, func() bool { return event.Load() == 1 }, 30*time.Second, 50*time.Millisecond)
	require.Equal(t, int64(1), sched.Load())
}

// TestYieldEnqueuesAContinuation: a Batched kind that yields while the
// worker drains ends `yielded` and its continuation (same period, "#1")
// finishes the work.
func TestYieldEnqueuesAContinuation(t *testing.T) {
	var h *harness
	h = newHarness(t, func(r *Registry) {
		k := validScheduled()
		k.Name, k.Batched, k.Timeout = "rollup_big", true, 10*time.Minute
		k.Handler = func(ctx context.Context, run Run) (Result, error) {
			if err := Yield(ctx); err != nil {
				h.draining.Store(false) // the "next worker" is not draining
				return Result{Summary: map[string]int64{"done": 1}}, err
			}
			return Result{Summary: map[string]int64{"seq": int64(run.Seq)}}, nil
		}
		Define(r, k)
	})
	h.draining.Store(true)
	tick := h.atTick("rollup_big")
	_, err := h.run.EnqueueDue(h.ctx, h.c)
	require.NoError(t, err)
	h.waitCount(1, `SELECT count(*) FROM job_runs WHERE kind = 'rollup_big' AND trigger = 'continuation' AND status = 'succeeded'`)
	var p1, p2 string
	require.NoError(t, h.db.Pool.QueryRow(h.ctx, `SELECT period_key FROM job_runs WHERE kind = 'rollup_big' AND trigger = 'schedule' AND status = 'yielded'`).Scan(&p1))
	require.NoError(t, h.db.Pool.QueryRow(h.ctx, `SELECT period_key FROM job_runs WHERE kind = 'rollup_big' AND trigger = 'continuation'`).Scan(&p2))
	require.Equal(t, PerTick.Key(tick), p1)
	require.Equal(t, p1, p2)
	var args []byte
	require.NoError(t, h.db.Pool.QueryRow(h.ctx, `SELECT args FROM river_job WHERE kind = 'rollup_big' AND args->>'t' = 'continuation'`).Scan(&args))
	var env envelope
	require.NoError(t, json.Unmarshal(args, &env))
	require.Equal(t, p1+"#1", env.U)
	require.Equal(t, 1, env.N)
	require.Equal(t, 1, h.count(`SELECT yielded::int FROM job_stats_hourly WHERE kind = 'rollup_big'`))
}

// TestEnqueueRunTx: a manual run carries its id, admin and the latest
// period, and is logged as manual.
func TestEnqueueRunTx(t *testing.T) {
	h := newHarness(t, func(r *Registry) {
		Define(r, validScheduled())
		Define(r, validEvent())
	})
	tick := h.atTick("rollup_today")
	admin := ids.New(h.clk.Now())
	enq := NewEnqueuer(h.reg, h.c, h.clk)
	id, period, err := enq.EnqueueRunTx(h.ctx, nil, "rollup_today", RunOpts{Trigger: TriggerManual, By: admin})
	require.NoError(t, err)
	require.Equal(t, PerTick.Key(tick), period)
	h.waitCount(1, `SELECT count(*) FROM job_runs WHERE kind = 'rollup_today' AND status = 'succeeded' AND trigger = 'manual'`)
	require.Equal(t, 1, h.count(`SELECT count(*) FROM job_runs WHERE id = $1 AND triggered_by = $2 AND period_key = $3`,
		ids.UUID(id), ids.UUID(admin), period))

	_, _, err = enq.EnqueueRunTx(h.ctx, nil, "orders_accept_by", RunOpts{Trigger: TriggerManual})
	require.ErrorIs(t, err, ErrNotScheduled)
	_, _, err = enq.EnqueueRunTx(h.ctx, nil, "rollup_today", RunOpts{Trigger: TriggerSchedule})
	require.ErrorIs(t, err, ErrBadTrigger)
	_, _, err = enq.EnqueueRunTx(h.ctx, nil, "rollup_today", RunOpts{Trigger: TriggerBackfill, PeriodKey: "yesterday"})
	require.ErrorIs(t, err, ErrBadPeriod)
}

// TestConcurrencySlots: a kind with Concurrency 1 holds one slot at a time.
func TestConcurrencySlots(t *testing.T) {
	h := newHarness(t, func(r *Registry) {
		k := validEvent()
		k.Concurrency = 1
		Define(r, k)
	})
	k, _ := h.reg.Lookup("orders_accept_by")
	rel, ok, err := h.run.acquireSlot(h.ctx, k)
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = h.run.acquireSlot(h.ctx, k)
	require.NoError(t, err)
	require.False(t, ok, "the only slot is taken")
	rel()
	rel2, ok, err := h.run.acquireSlot(h.ctx, k)
	require.NoError(t, err)
	require.True(t, ok)
	rel2()
}
