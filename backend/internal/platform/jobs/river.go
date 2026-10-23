package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/queue"
)

// envelope is the River args type of every kind: the kind name lives in
// river_job.kind (one worker per kind), the envelope says why the run
// started and, for event kinds, carries the domain's args.
type envelope struct {
	kind string // set on insert; River stores it as river_job.kind

	// U is the River unique key: "<period>#<n>" for a scheduled run and
	// its continuations, "manual:<run id>" / "retry:<run id>", or the
	// event's EnqueueOpts.UniqueKey. Only U is unique-tagged.
	U string `json:"u,omitempty" river:"unique"`
	// P is the period key a scheduled kind's run covers.
	P string `json:"p,omitempty"`
	// N is the continuation sequence (0 for a first run).
	N int `json:"n,omitempty"`
	// T is the trigger (event when empty).
	T Trigger `json:"t,omitempty"`
	// S is when the run was due (the cron tick, the event's ScheduledAt):
	// lateness is measured from it on the first attempt.
	S time.Time `json:"s,omitzero"`
	// R is the job_runs id chosen at enqueue (manual and retry runs, so the
	// admin API can answer with it); the first attempt uses it.
	R string `json:"r,omitempty"`
	// By is the admin user who triggered a manual or retry run.
	By string `json:"by,omitempty"`
	// A is the event kind's args (JSON).
	A json.RawMessage `json:"a,omitempty"`
}

// Kind implements river.JobArgs.
func (e envelope) Kind() string { return e.kind }

// seqBase is the unique key without its "#<n>" suffix.
func (e envelope) seqBase() string {
	base, _, _ := strings.Cut(e.U, "#")
	return base
}

func queueOf(k Kind) string {
	if k.Queue == "" {
		return river.QueueDefault
	}
	return k.Queue
}

// eventUniqueStates: an event job with a UniqueKey is a no-op while one of
// the same kind and key is pending or running (EnqueueOpts.UniqueKey);
// once it completed, the key may be used again.
var eventUniqueStates = []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
	rivertype.JobStateRetryable, rivertype.JobStateScheduled,
}

// kindWorker is the River worker of one kind.
type kindWorker struct {
	river.WorkerDefaults[envelope]
	r *Runner
	e *entry
}

// Timeout implements river.Worker: the kind's declared cap.
func (w *kindWorker) Timeout(*river.Job[envelope]) time.Duration { return w.e.kind.Timeout }

// Work implements river.Worker through the jobs middleware.
func (w *kindWorker) Work(ctx context.Context, job *river.Job[envelope]) error {
	return w.r.work(ctx, w.e, job)
}

// RiverEnqueuer is the River-backed Enqueuer (api and worker). It also
// enqueues the admin API's manual and retry runs.
type RiverEnqueuer struct {
	reg *Registry
	c   *queue.Client
	clk clock.Clock
}

// NewEnqueuer returns an Enqueuer that inserts through c (the API's
// insert-only client or the worker's client).
func NewEnqueuer(reg *Registry, c *queue.Client, clk clock.Clock) *RiverEnqueuer {
	return &RiverEnqueuer{reg: reg, c: c, clk: clk}
}

func (q *RiverEnqueuer) insert(ctx context.Context, tx pgx.Tx, env envelope, opts *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	if q.c == nil {
		return nil, errors.New("jobs: no queue client")
	}
	if tx == nil {
		return q.c.Insert(ctx, env, opts)
	}
	return q.c.InsertTx(ctx, tx, env, opts)
}

// EnqueueTx implements Enqueuer.
func (q *RiverEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, kind string, args any, o EnqueueOpts) error {
	k, err := checkEnqueue(q.reg, kind, o)
	if err != nil {
		return err
	}
	b, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("jobs: marshal %s args: %w", kind, err)
	}
	at := o.ScheduledAt
	if at.IsZero() {
		at = q.clk.Now()
	}
	at = at.UTC()
	env := envelope{kind: k.Name, U: o.UniqueKey, T: TriggerEvent, S: at, A: b}
	opts := &river.InsertOpts{Queue: queueOf(k), MaxAttempts: k.MaxAttempts, ScheduledAt: at}
	if o.UniqueKey != "" {
		opts.UniqueOpts = river.UniqueOpts{ByArgs: true, ByState: eventUniqueStates}
	}
	if _, err := q.insert(ctx, tx, env, opts); err != nil {
		return fmt.Errorf("jobs: enqueue %s: %w", kind, err)
	}
	return nil
}

// RunOpts describe a run the admin API asks for.
type RunOpts struct {
	// Trigger is TriggerManual, TriggerRetry or TriggerBackfill.
	Trigger Trigger
	// PeriodKey is the period to run; empty means the period of the kind's
	// latest tick.
	PeriodKey string
	// By is the admin user (zero for none).
	By ids.ID
}

// Errors from EnqueueRunTx.
var (
	ErrNotScheduled = errors.New("jobs: kind is not scheduled")
	ErrBadPeriod    = errors.New("jobs: bad period key")
	ErrBadTrigger   = errors.New("jobs: trigger must be manual, retry or backfill")
)

// EnqueueRunTx enqueues one run of the scheduled kind named kind for a
// period, now, inside tx, and returns the job_runs id the run will have.
// Who may ask and whether the kind may run now (paused, already running,
// timer class) is the caller's check.
func (q *RiverEnqueuer) EnqueueRunTx(ctx context.Context, tx pgx.Tx, kind string, o RunOpts) (ids.ID, string, error) {
	e, ok := q.reg.entry(kind)
	if !ok {
		return ids.ID{}, "", fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	if e.sched == nil {
		return ids.ID{}, "", fmt.Errorf("%w: %q", ErrNotScheduled, kind)
	}
	switch o.Trigger {
	case TriggerManual, TriggerRetry, TriggerBackfill:
	default:
		return ids.ID{}, "", ErrBadTrigger
	}
	k := e.kind
	now := q.clk.Now()
	period := o.PeriodKey
	if period == "" {
		period = k.UniqueBy.Key(e.sched.Prev(now))
	}
	if _, err := k.UniqueBy.Start(period); err != nil {
		return ids.ID{}, "", fmt.Errorf("%w: %w", ErrBadPeriod, err)
	}
	id := ids.New(now)
	env := envelope{kind: k.Name, U: string(o.Trigger) + ":" + id.String(), P: period, T: o.Trigger, S: now, R: id.String()}
	if o.By != (ids.ID{}) {
		env.By = o.By.String()
	}
	opts := &river.InsertOpts{Queue: queueOf(k), MaxAttempts: k.MaxAttempts, ScheduledAt: now,
		UniqueOpts: river.UniqueOpts{ByArgs: true}}
	if _, err := q.insert(ctx, tx, env, opts); err != nil {
		return ids.ID{}, "", fmt.Errorf("jobs: enqueue %s run: %w", kind, err)
	}
	return id, period, nil
}
