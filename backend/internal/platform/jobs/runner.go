package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// RunnerOptions configure a Runner.
type RunnerOptions struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
	Log   *slog.Logger
	// Draining reports whether the worker is shutting down (the worker's
	// /readyz draining flag); Yield in a Batched kind checks it. Nil means
	// never draining.
	Draining func() bool
	// MeterProvider and TracerProvider default to the global ones.
	MeterProvider  metric.MeterProvider
	TracerProvider trace.TracerProvider
	// SchedulerPoll is how often RunScheduler looks for due ticks (1 s).
	SchedulerPoll time.Duration
}

// Platform tuning (not product rules, so not in platform_settings).
const (
	// pausedSnooze: a paused kind's event or continuation job is looked at
	// again after this long. TODO(lead): confirm 1 minute.
	pausedSnooze = time.Minute
	// slotSnooze: a kind at its Concurrency cap tries again after this long.
	slotSnooze = 5 * time.Second
	// bookkeepingTimeout bounds the job_runs and stats writes after a run,
	// which run even when the run's context was cancelled.
	bookkeepingTimeout = 10 * time.Second
)

// Runner is the jobs middleware (D-95): it works every kind's River jobs,
// records job_runs and job_stats_hourly, emits the tz_job_* metrics, applies
// pause and concurrency, and schedules the scheduled kinds.
type Runner struct {
	reg    *Registry
	o      RunnerOptions
	mon    *monitor
	tracer trace.Tracer

	mu       sync.Mutex
	lastTick map[string]time.Time
}

// NewRunner builds the worker's runner over reg.
func NewRunner(reg *Registry, o RunnerOptions) (*Runner, error) {
	if o.Pool == nil || o.Clock == nil {
		return nil, errors.New("jobs: runner needs Pool and Clock")
	}
	o.Log = telemetry.OrDefault(o.Log)
	if o.SchedulerPoll <= 0 {
		o.SchedulerPoll = time.Second
	}
	tp := o.TracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	mon, err := newMonitor(o.MeterProvider, reg)
	if err != nil {
		return nil, fmt.Errorf("jobs: metrics: %w", err)
	}
	reg.setMonitor(mon)
	return &Runner{reg: reg, o: o, mon: mon, tracer: tp.Tracer("todayz/jobs"), lastTick: map[string]time.Time{}}, nil
}

// Close unregisters the runner's metric callback.
func (r *Runner) Close() { r.mon.close() }

// Register adds one River worker per kind, and the kinds' named queues, to q.
func (r *Runner) Register(q *queue.Registry) {
	for _, name := range r.reg.Names() {
		e, _ := r.reg.entry(name)
		queue.AddArgs[envelope](q, envelope{kind: name}, &kindWorker{r: r, e: e})
		q.AddQueue(e.kind.Queue, 0)
	}
}

func (r *Runner) draining() bool { return r.o.Draining != nil && r.o.Draining() }

// runRow is one job_runs row.
type runRow struct {
	id          ids.ID
	kind        string
	class       Class
	riverJobID  int64
	trigger     Trigger
	periodKey   string
	attempt     int
	status      Status
	scheduledAt time.Time
	startedAt   time.Time
	finishedAt  time.Time
	durationMS  *int64
	latenessMS  *int64
	summary     map[string]int64
	note        string
	errText     string
	traceID     string
	by          string
	createdAt   time.Time
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func (row *runRow) args() ([]any, error) {
	var summary any
	if row.summary != nil {
		b, err := json.Marshal(row.summary)
		if err != nil {
			return nil, err
		}
		summary = b
	}
	var by any
	if row.by != "" {
		if id, err := ids.Parse(row.by); err == nil {
			by = ids.UUID(id)
		}
	}
	var riverID any
	if row.riverJobID != 0 {
		riverID = row.riverJobID
	}
	return []any{
		ids.UUID(row.id), row.kind, string(row.class), riverID, string(row.trigger), nullStr(row.periodKey),
		row.attempt, string(row.status), nullTime(row.scheduledAt), nullTime(row.startedAt), nullTime(row.finishedAt),
		row.durationMS, row.latenessMS, summary, nullStr(row.note), nullStr(row.errText), nullStr(row.traceID), by, row.createdAt,
	}, nil
}

const insertRunSQL = `INSERT INTO job_runs (id, kind, class, river_job_id, trigger, period_key, attempt, status,
	scheduled_at, started_at, finished_at, duration_ms, lateness_ms, summary, note, error, trace_id, triggered_by, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`

const finishRunSQL = `UPDATE job_runs SET status = $3, finished_at = $4, duration_ms = $5, lateness_ms = $6,
	summary = $7, note = $8, error = $9 WHERE id = $1 AND created_at = $2`

const statsSQL = `INSERT INTO job_stats_hourly AS s (kind, hour, succeeded, failed, cancelled, skipped, discarded, yielded,
	sum_ms, max_ms, max_lateness_ms)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $10)
	ON CONFLICT (kind, hour) DO UPDATE SET
	  succeeded = s.succeeded + EXCLUDED.succeeded, failed = s.failed + EXCLUDED.failed,
	  cancelled = s.cancelled + EXCLUDED.cancelled, skipped = s.skipped + EXCLUDED.skipped,
	  discarded = s.discarded + EXCLUDED.discarded, yielded = s.yielded + EXCLUDED.yielded,
	  sum_ms = s.sum_ms + EXCLUDED.sum_ms, max_ms = GREATEST(s.max_ms, EXCLUDED.max_ms),
	  max_lateness_ms = GREATEST(s.max_lateness_ms, EXCLUDED.max_lateness_ms)`

// istHour is the start of the IST hour holding t (UTC).
func istHour(t time.Time) time.Time {
	l := t.In(clock.IST)
	return time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), 0, 0, 0, clock.IST).UTC()
}

func statsArgs(kind string, s Status, hour time.Time, durMS, lateMS int64) []any {
	c := map[Status]int64{s: 1}
	return []any{kind, hour, c[StatusSucceeded], c[StatusFailed], c[StatusCancelled], c[StatusSkipped],
		c[StatusDiscarded], c[StatusYielded], durMS, lateMS}
}

func (r *Runner) paused(ctx context.Context, kind string) (bool, error) {
	var p bool
	err := r.o.Pool.QueryRow(ctx, `SELECT paused FROM job_controls WHERE kind = $1`, kind).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return p, err
}

func (r *Runner) periodSucceeded(ctx context.Context, kind, period string) (bool, error) {
	var ok bool
	err := r.o.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_runs
		WHERE kind = $1 AND period_key = $2 AND status = 'succeeded')`, kind, period).Scan(&ok)
	return ok, err
}

// acquireSlot takes one of the kind's Concurrency session advisory locks on
// a dedicated connection, held for the run.
func (r *Runner) acquireSlot(ctx context.Context, k Kind) (release func(), ok bool, err error) {
	conn, err := r.o.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	key := "tz_jobs:" + k.Name
	for slot := range k.Concurrency {
		var got bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1), $2::int)`, key, slot).Scan(&got); err != nil {
			conn.Release()
			return nil, false, err
		}
		if got {
			return func() {
				bg, cancel := context.WithTimeout(context.Background(), bookkeepingTimeout)
				defer cancel()
				if _, err := conn.Exec(bg, `SELECT pg_advisory_unlock(hashtext($1), $2::int)`, key, slot); err != nil {
					_ = conn.Conn().Close(bg) // closing the session frees the lock; the pool drops the conn
				}
				conn.Release()
			}, true, nil
		}
	}
	conn.Release()
	return nil, false, nil
}

// work is the jobs middleware around one attempt.
func (r *Runner) work(ctx context.Context, e *entry, job *river.Job[envelope]) error {
	k := e.kind
	env := job.Args
	trig := env.T
	if trig == "" {
		trig = TriggerEvent
	}
	log := r.o.Log.With(slog.String("kind", k.Name), slog.String("trigger", string(trig)), slog.Int("attempt", job.Attempt))

	if k.Class != ClassTimer { // timer kinds are never paused
		paused, err := r.paused(ctx, k.Name)
		if err != nil {
			return fmt.Errorf("jobs: %s: read job_controls: %w", k.Name, err)
		}
		if paused {
			if trig == TriggerSchedule {
				log.InfoContext(ctx, "job paused; the tick is not run", slog.String("period", env.P))
				return nil
			}
			return river.JobSnooze(pausedSnooze)
		}
	}

	start := r.o.Clock.Now()
	row := &runRow{
		kind: k.Name, class: k.Class, riverJobID: job.ID, trigger: trig, periodKey: env.P,
		attempt: job.Attempt, scheduledAt: env.S, startedAt: start, by: env.By, createdAt: start,
	}
	row.id = ids.New(start)
	if job.Attempt == 1 && env.R != "" {
		if id, err := ids.Parse(env.R); err == nil {
			row.id = id
		}
	}

	if trig == TriggerSchedule && env.P != "" {
		done, err := r.periodSucceeded(ctx, k.Name, env.P)
		if err != nil {
			return fmt.Errorf("jobs: %s: read job_runs: %w", k.Name, err)
		}
		if done { // UniqueBy: a period that succeeded runs at most once
			row.status, row.finishedAt = StatusSkipped, start
			zero := int64(0)
			row.durationMS = &zero
			r.finish(ctx, log, k, row, true, 0, false)
			return nil
		}
	}

	if k.Concurrency > 0 {
		release, ok, err := r.acquireSlot(ctx, k)
		if err != nil {
			return fmt.Errorf("jobs: %s: concurrency slot: %w", k.Name, err)
		}
		if !ok {
			return river.JobSnooze(slotSnooze)
		}
		defer release()
	}

	ctx, span := r.tracer.Start(ctx, "job "+k.Name, trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.String("job.kind", k.Name), attribute.String("job.trigger", string(trig)),
			attribute.Int("job.attempt", job.Attempt), attribute.String("job.run_id", row.id.String())))
	defer span.End()
	if sc := span.SpanContext(); sc.HasTraceID() {
		row.traceID = sc.TraceID().String()
	}
	log = log.With(slog.String("run_id", row.id.String()))
	if row.traceID != "" {
		log = log.With(slog.String("trace_id", row.traceID))
	}

	logged := trig != TriggerEvent // D-95: every non-event run is logged from its start
	if logged {
		row.status = StatusRunning
		a, err := row.args()
		if err != nil {
			return err
		}
		if _, err := r.o.Pool.Exec(ctx, insertRunSQL, a...); err != nil {
			return fmt.Errorf("jobs: %s: record run: %w", k.Name, err)
		}
	}
	log.InfoContext(ctx, "job started", slog.String("period", env.P))

	run := Run{
		Kind: k.Name, Class: k.Class, ID: row.id, Trigger: trig, PeriodKey: env.P,
		ScheduledAt: env.S, Seq: env.N, Attempt: job.Attempt, args: env.A,
	}
	if env.P != "" && k.UniqueBy != PeriodNone {
		run.PeriodStart, _ = k.UniqueBy.Start(env.P)
	}
	hctx := telemetry.WithLogger(ctx, log)
	y := &riverYielder{r: r, k: k, env: env, runID: row.id}
	if k.Batched {
		hctx = withYielder(hctx, y)
	}
	res, err := callHandler(hctx, k, run)
	end := r.o.Clock.Now()
	st := outcome(ctx, err, y.did, job.Attempt, job.MaxAttempts)

	dur := max(end.Sub(start), 0)
	durMS := dur.Milliseconds()
	row.status, row.finishedAt, row.durationMS = st, end, &durMS
	var late time.Duration
	lateKnown := job.Attempt == 1 && !env.S.IsZero()
	if lateKnown {
		late = max(start.Sub(env.S), 0)
		lateMS := late.Milliseconds()
		row.latenessMS = &lateMS
	}
	row.summary, row.note = res.Summary, scrubNote(res.Note)
	var retErr error
	switch st {
	case StatusFailed, StatusDiscarded, StatusCancelled:
		row.errText = scrubError(err)
		// River stores the returned error in river_job.errors: give it the
		// scrubbed text, never the raw one.
		retErr = errors.New(row.errText)
		span.SetStatus(codes.Error, row.errText)
	}

	write := logged || st == StatusFailed || st == StatusDiscarded || st == StatusCancelled || job.Attempt > 1
	r.finish(ctx, log, k, row, write, late, lateKnown)
	if st == StatusSucceeded && k.Scheduled() {
		r.mon.setMissed(k.Name, false) // the gauge resets on the next success (D-95)
	}

	attrs := []any{slog.String("status", string(st)), slog.Int64("duration_ms", durMS)}
	switch st {
	case StatusDiscarded, StatusCancelled:
		log.ErrorContext(ctx, "job finished", append(attrs, slog.String("err", row.errText))...)
	case StatusFailed:
		log.WarnContext(ctx, "job finished", append(attrs, slog.String("err", row.errText))...)
	default:
		log.InfoContext(ctx, "job finished", attrs...)
	}
	return retErr
}

// finish writes the run's row (insert, or update of the running row), the
// hourly stats and the metrics. It runs even when ctx was cancelled.
func (r *Runner) finish(ctx context.Context, log *slog.Logger, k Kind, row *runRow, write bool, late time.Duration, lateKnown bool) {
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	var durMS int64
	if row.durationMS != nil {
		durMS = *row.durationMS
	}
	err := pgx.BeginFunc(bg, r.o.Pool, func(tx pgx.Tx) error {
		if write {
			if row.status != StatusSkipped && row.trigger != TriggerEvent {
				var summary any
				if row.summary != nil {
					b, err := json.Marshal(row.summary)
					if err != nil {
						return err
					}
					summary = b
				}
				if _, err := tx.Exec(bg, finishRunSQL, ids.UUID(row.id), row.createdAt, string(row.status), row.finishedAt,
					row.durationMS, row.latenessMS, summary, nullStr(row.note), nullStr(row.errText)); err != nil {
					return err
				}
			} else {
				a, err := row.args()
				if err != nil {
					return err
				}
				if _, err := tx.Exec(bg, insertRunSQL, a...); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(bg, statsSQL, statsArgs(k.Name, row.status, istHour(row.startedAt), durMS, late.Milliseconds())...)
		return err
	})
	if err != nil {
		log.ErrorContext(bg, "jobs: could not record the run", slog.String("status", string(row.status)), slog.String("err", err.Error()))
	}
	r.mon.record(bg, k, row.status, float64(durMS)/1000, late.Seconds(), lateKnown)
}

// callHandler runs the handler, turning a panic into an error (the stack is
// logged, not stored).
func callHandler(ctx context.Context, k Kind, run Run) (res Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			telemetry.Logger(ctx).ErrorContext(ctx, "job handler panicked", slog.String("stack", string(debug.Stack())))
			err = fmt.Errorf("jobs: %s handler panicked: %v", k.Name, p)
		}
	}()
	return k.Handler(ctx, run)
}

// riverYielder enqueues a Batched run's continuation through the River
// client working the job.
type riverYielder struct {
	r     *Runner
	k     Kind
	env   envelope
	runID ids.ID
	did   bool
}

func (y *riverYielder) yield(ctx context.Context) (bool, error) {
	if !y.r.draining() {
		return false, nil
	}
	if y.did {
		return true, nil
	}
	c, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return false, fmt.Errorf("jobs: yield: %w", err)
	}
	base := y.env.seqBase()
	if base == "" {
		base = "run:" + y.runID.String()
	}
	next := y.env
	next.kind = y.k.Name
	next.N = y.env.N + 1
	next.U = fmt.Sprintf("%s#%d", base, next.N)
	next.T = TriggerContinuation
	next.S = y.r.o.Clock.Now()
	next.R = ""
	_, err = c.Insert(ctx, next, &river.InsertOpts{Queue: queueOf(y.k), MaxAttempts: y.k.MaxAttempts,
		ScheduledAt: next.S, UniqueOpts: river.UniqueOpts{ByArgs: true}})
	if err != nil {
		return false, fmt.Errorf("jobs: enqueue continuation: %w", err)
	}
	y.did = true
	return true, nil
}
