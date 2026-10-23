// Package jobs is the jobs platform (02 §2.7, D-95, D-98): every background
// job is a registered kind, declared with Define, and every River worker goes
// through it. A test fails if a worker is registered any other way.
//
// Adding a job is one declaration plus a handler, in the owning domain's
// stub (internal/jobs/<domain>.go):
//
//	func registerorders(r *Registry, d Deps) {
//		svc := orders.NewTimers(d.Pool, d.Clock)
//		jobs.Define(r, jobs.Kind{
//			Name:        "orders_accept_by",
//			Owner:       "W2-02",
//			Description: "Expires an order the shop did not accept in time.",
//			Class:       jobs.ClassTimer,
//			Timeout:     30 * time.Second,
//			MaxAttempts: 10,
//			Runbook:     "docs/runbooks/orders-accept-by.md",
//			Handler:     svc.ExpireAcceptBy,
//		})
//	}
//
// A kind is scheduled (Schedule, an IST cron expression, with UniqueBy, the
// period a run covers) or event-triggered (no Schedule; enqueued with an
// Enqueuer inside the transaction that needs it). The handler returns a
// Result; the jobs middleware records the run, the hourly stats and the
// metrics. Handlers must be idempotent: an attempt can be retried.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river/rivertype"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// Class is a kind's class. It decides who may control the kind (04 §2.7)
// and whether it needs a runbook.
type Class string

// The seven classes (D-95).
const (
	// ClassTimer: order and hold expiries. Never paused, run or retried by
	// hand. Needs a runbook.
	ClassTimer Class = "timer"
	// ClassMoney: ledger, payouts, credit expiry. Controlled only by
	// super_admin with step-up. Needs a runbook.
	ClassMoney Class = "money"
	// ClassNotify: notification sends; caps and quiet rules come from settings.
	ClassNotify Class = "notify"
	// ClassMarketing: marketing sends; caps and quiet rules come from settings.
	ClassMarketing Class = "marketing"
	// ClassAI: AI jobs. They write drafts only; a person confirms.
	ClassAI Class = "ai"
	// ClassRollup: analytics rollups (05 §3).
	ClassRollup Class = "rollup"
	// ClassMaintenance: partitions, retention, platform housekeeping.
	ClassMaintenance Class = "maintenance"
)

// Classes lists every class, in the order of D-95.
func Classes() []Class {
	return []Class{ClassTimer, ClassMoney, ClassNotify, ClassMarketing, ClassAI, ClassRollup, ClassMaintenance}
}

// Valid reports whether c is one of Classes.
func (c Class) Valid() bool {
	for _, x := range Classes() {
		if c == x {
			return true
		}
	}
	return false
}

// NeedsRunbook reports whether kinds of class c must name a runbook.
func (c Class) NeedsRunbook() bool { return c == ClassTimer || c == ClassMoney }

// Trigger is why a run started (job_runs.trigger).
type Trigger string

// Triggers.
const (
	TriggerSchedule     Trigger = "schedule"     // the kind's IST cron tick
	TriggerEvent        Trigger = "event"        // enqueued by domain code
	TriggerManual       Trigger = "manual"       // run-now from the admin API
	TriggerRetry        Trigger = "retry"        // retry of a failed run from the admin API
	TriggerBackfill     Trigger = "backfill"     // a backfill command for a past period
	TriggerContinuation Trigger = "continuation" // a batched run that yielded at a shutdown (D-98)
)

// Triggers lists every trigger.
func Triggers() []Trigger {
	return []Trigger{TriggerSchedule, TriggerEvent, TriggerManual, TriggerRetry, TriggerBackfill, TriggerContinuation}
}

// Status is how a run ended (job_runs.status).
type Status string

// Statuses.
const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"    // the attempt failed; River retries it
	StatusCancelled Status = "cancelled" // the attempt was cancelled (shutdown hard cancel)
	StatusSkipped   Status = "skipped"   // the period had already succeeded (UniqueBy)
	StatusDiscarded Status = "discarded" // the last attempt failed; no more retries
	StatusMissed    Status = "missed"    // written by jobs_watchdog, never by a run
	StatusYielded   Status = "yielded"   // a batched run stopped at a shutdown and enqueued its continuation
)

// Statuses lists every status.
func Statuses() []Status {
	return []Status{StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled, StatusSkipped, StatusDiscarded, StatusMissed, StatusYielded}
}

// Period is what UniqueBy keys a scheduled kind's runs on: a period runs at
// most once. Period keys are IST.
type Period uint8

// Periods. The zero value is for event-triggered kinds.
const (
	PeriodNone Period = iota
	// PerTick: every cron tick is its own period ("2006-01-02T15:04").
	PerTick
	// PerHour: one run per IST hour ("2006-01-02T15").
	PerHour
	// PerDay: one run per IST day ("2006-01-02").
	PerDay
	// PerMonth: one run per IST month ("2006-01").
	PerMonth
)

func (p Period) layout() string {
	switch p {
	case PerTick:
		return "2006-01-02T15:04"
	case PerHour:
		return "2006-01-02T15"
	case PerDay:
		return "2006-01-02"
	case PerMonth:
		return "2006-01"
	}
	return ""
}

// String names the period.
func (p Period) String() string {
	switch p {
	case PerTick:
		return "tick"
	case PerHour:
		return "hour"
	case PerDay:
		return "day"
	case PerMonth:
		return "month"
	}
	return "none"
}

// Key is the period key of the period holding t (IST).
func (p Period) Key(t time.Time) string {
	l := p.layout()
	if l == "" {
		return ""
	}
	return t.In(clock.IST).Format(l)
}

// Start parses a period key back to the start of its period (UTC).
func (p Period) Start(key string) (time.Time, error) {
	l := p.layout()
	if l == "" {
		return time.Time{}, errors.New("jobs: no period")
	}
	t, err := time.ParseInLocation(l, key, clock.IST)
	if err != nil {
		return time.Time{}, fmt.Errorf("jobs: period key %q is not a %s key", key, p)
	}
	return t.UTC(), nil
}

// Result is what a handler returns: counts for the run's summary (for
// example {"sent":120,"skipped":3}) and an optional short note. Neither may
// hold personal data, codes or secrets.
type Result struct {
	Summary map[string]int64
	Note    string
}

// Handler works one run of a kind.
type Handler func(ctx context.Context, run Run) (Result, error)

// Run is what a handler is told about the run it works.
type Run struct {
	// Kind and Class of the run.
	Kind  string
	Class Class
	// ID is the job_runs id (also in the logs as run_id).
	ID ids.ID
	// Trigger is why the run started.
	Trigger Trigger
	// PeriodKey and PeriodStart name the period a scheduled kind's run
	// covers (zero for event kinds). A manual, retry or backfill run of a
	// scheduled kind carries the period it was asked for.
	PeriodKey   string
	PeriodStart time.Time
	// ScheduledAt is when the run was due: the cron tick, or when an event
	// job was scheduled for. Lateness is measured from it.
	ScheduledAt time.Time
	// Seq is the continuation sequence: 0 for a first run, n for the n-th
	// continuation of a batched run (unique key <period>#<n>).
	Seq int
	// Attempt is the River attempt, from 1.
	Attempt int

	args json.RawMessage
}

// Decode unmarshals the event job's arguments (what was passed to
// Enqueuer.EnqueueTx) into v. Scheduled kinds have no arguments.
func (r Run) Decode(v any) error {
	if len(r.args) == 0 {
		return errors.New("jobs: run has no arguments")
	}
	if err := json.Unmarshal(r.args, v); err != nil {
		return fmt.Errorf("jobs: decode %s args: %w", r.Kind, err)
	}
	return nil
}

// Args returns the raw JSON arguments (nil for scheduled kinds).
func (r Run) Args() json.RawMessage { return r.args }

// WithArgs returns a copy of r carrying raw arguments, for tests that call
// a handler directly.
func (r Run) WithArgs(args json.RawMessage) Run {
	r.args = append(json.RawMessage(nil), args...)
	return r
}

// Kind declares a job kind (02 §2.7).
type Kind struct {
	// Name is unique and snake_case, for example "rollup_today". It is the
	// River kind, the metrics `kind` label and the A-21 row.
	Name string
	// Owner is the owning work package, for example "W3-07".
	Owner string
	// Description is one sentence, shown on A-21.
	Description string
	// Class decides who may control the kind and whether Runbook is required.
	Class Class
	// Schedule is a 5-field cron expression in IST ("minute hour day-of-month
	// month day-of-week"; *, lists, ranges and steps). Empty means the kind
	// is event-triggered.
	Schedule string
	// UniqueBy is the period a scheduled run covers; a period runs at most
	// once. Required for scheduled kinds, zero for event kinds.
	UniqueBy Period
	// Timeout caps one attempt. At most 60 s unless Batched (D-98).
	Timeout time.Duration
	// Batched kinds work in batches of at most 60 s, commit a checkpoint per
	// batch and call Yield(ctx) between batches.
	Batched bool
	// MaxAttempts is how many attempts River makes before the run is
	// discarded (1–25).
	MaxAttempts int
	// Queue is the River queue; empty means the default queue.
	Queue string
	// Concurrency caps how many runs of this kind work at once across all
	// workers; 0 means no cap beyond the queue's. At most 10: each capped run
	// holds a pool connection for its whole run (see maxConcurrency).
	Concurrency int
	// Runbook is the repo path of the kind's runbook
	// ("docs/runbooks/<name>.md"). Required for timer and money kinds.
	Runbook string
	// Handler works one run.
	Handler Handler
}

// Scheduled reports whether k runs on a schedule (else it is event-triggered).
func (k Kind) Scheduled() bool { return k.Schedule != "" }

// MaxTimeout is the cap on a non-batched kind's Timeout (D-98).
const MaxTimeout = 60 * time.Second

// ErrYielded is returned by Yield while the worker drains: the handler
// returns it (or any error wrapping it) and the run ends yielded.
var ErrYielded = errors.New("jobs: yielded for shutdown; a continuation is enqueued")

// outcome is the status of a finished attempt. It matches the state River
// gives the job: a person's JobCancel (cause ErrJobCancelledRemotely) is
// cancelled whatever the attempt; a hard cancel at shutdown (StopAndCancel)
// reaches River as an ordinary error (the middleware returns the scrubbed
// text), so River retries it, or discards it on the last attempt, and the run
// is recorded discarded there, where A-21 and the "Job failing" alert see it.
func outcome(ctx context.Context, err error, yielded bool, attempt, maxAttempts int) Status {
	switch {
	case yielded:
		return StatusYielded
	case err == nil:
		return StatusSucceeded
	case errors.Is(err, ErrYielded):
		return StatusYielded
	case errors.Is(ctx.Err(), context.Canceled):
		if attempt >= maxAttempts && !errors.Is(context.Cause(ctx), rivertype.ErrJobCancelledRemotely) {
			return StatusDiscarded
		}
		return StatusCancelled
	case attempt >= maxAttempts:
		return StatusDiscarded
	default:
		return StatusFailed
	}
}
