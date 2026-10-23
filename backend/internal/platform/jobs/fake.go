package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// Fake is an in-memory jobs runner for unit tests of domain code. It is an
// Enqueuer (enqueues are kept in memory; the tx is ignored) and runs a
// registry's handlers the way the worker does: the same Run fields, the
// same status rules, UniqueBy (a period that succeeded is skipped),
// UniqueKey, retries up to MaxAttempts, and Yield while draining.
//
// It records no job_runs rows and emits no metrics; the integration tests
// of this package cover those.
type Fake struct {
	reg *Registry
	clk clock.Clock

	mu       sync.Mutex
	pending  []*FakeJob
	runs     []FakeRun
	done     map[string]bool // kind + "\x00" + period key that succeeded
	draining bool
}

// FakeJob is an enqueued job waiting in a Fake.
type FakeJob struct {
	Kind        string
	Trigger     Trigger
	Args        json.RawMessage
	ScheduledAt time.Time
	UniqueKey   string
	PeriodKey   string
	Seq         int
	Attempt     int // attempts made so far
}

// FakeRun is one attempt a Fake worked.
type FakeRun struct {
	Run    Run
	Status Status
	Result Result
	Err    error
}

// NewFake returns a Fake over the kinds in r, with c as its clock.
func NewFake(r *Registry, c clock.Clock) *Fake {
	return &Fake{reg: r, clk: c, done: map[string]bool{}}
}

// EnqueueTx implements Enqueuer.
func (f *Fake) EnqueueTx(_ context.Context, _ pgx.Tx, kind string, args any, o EnqueueOpts) error {
	if _, err := checkEnqueue(f.reg, kind, o); err != nil {
		return err
	}
	b, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("jobs: marshal %s args: %w", kind, err)
	}
	at := o.ScheduledAt
	if at.IsZero() {
		at = f.clk.Now()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if o.UniqueKey != "" {
		for _, j := range f.pending {
			if j.Kind == kind && j.UniqueKey == o.UniqueKey {
				return nil
			}
		}
	}
	f.pending = append(f.pending, &FakeJob{Kind: kind, Trigger: TriggerEvent, Args: b, ScheduledAt: at.UTC(), UniqueKey: o.UniqueKey})
	return nil
}

// SetDraining makes Yield in batched handlers enqueue a continuation and
// return ErrYielded, as on a worker shutdown.
func (f *Fake) SetDraining(v bool) {
	f.mu.Lock()
	f.draining = v
	f.mu.Unlock()
}

// Pending returns copies of the jobs waiting, oldest first.
func (f *Fake) Pending() []FakeJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeJob, len(f.pending))
	for i, j := range f.pending {
		out[i] = *j
	}
	return out
}

// Runs returns every attempt worked so far, in order.
func (f *Fake) Runs() []FakeRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeRun(nil), f.runs...)
}

// RunDue works, once each, the pending jobs due at the clock's now (an
// attempt that fails is put back for its next attempt unless it was the
// last) and returns the attempts it made.
func (f *Fake) RunDue(ctx context.Context) []FakeRun {
	now := f.clk.Now()
	f.mu.Lock()
	sort.SliceStable(f.pending, func(i, j int) bool { return f.pending[i].ScheduledAt.Before(f.pending[j].ScheduledAt) })
	var due, rest []*FakeJob
	for _, j := range f.pending {
		if !j.ScheduledAt.After(now) {
			due = append(due, j)
		} else {
			rest = append(rest, j)
		}
	}
	f.pending = rest
	f.mu.Unlock()

	out := make([]FakeRun, 0, len(due))
	for _, j := range due {
		fr := f.work(ctx, j)
		out = append(out, fr)
		if fr.Status == StatusFailed || fr.Status == StatusCancelled {
			f.mu.Lock()
			f.pending = append(f.pending, j)
			f.mu.Unlock()
		}
	}
	return out
}

// RunScheduled works one run of the scheduled kind named kind for the
// period holding the clock's last tick, as the schedule would (trigger
// schedule). A period that already succeeded is skipped.
func (f *Fake) RunScheduled(ctx context.Context, kind string) (FakeRun, error) {
	e, ok := f.reg.entry(kind)
	if !ok {
		return FakeRun{}, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	if e.sched == nil {
		return FakeRun{}, fmt.Errorf("jobs: kind %q is event-triggered", kind)
	}
	tick := e.sched.Prev(f.clk.Now())
	return f.work(ctx, &FakeJob{Kind: kind, Trigger: TriggerSchedule, ScheduledAt: tick, PeriodKey: e.kind.UniqueBy.Key(tick)}), nil
}

// RunEvent works one run of the event kind named kind with args, now,
// without enqueueing it.
func (f *Fake) RunEvent(ctx context.Context, kind string, args any) (FakeRun, error) {
	if _, err := checkEnqueue(f.reg, kind, EnqueueOpts{}); err != nil {
		return FakeRun{}, err
	}
	b, err := json.Marshal(args)
	if err != nil {
		return FakeRun{}, err
	}
	return f.work(ctx, &FakeJob{Kind: kind, Trigger: TriggerEvent, Args: b, ScheduledAt: f.clk.Now()}), nil
}

type fakeYielder struct {
	f   *Fake
	job *FakeJob
	did bool
}

func (y *fakeYielder) yield(context.Context) (bool, error) {
	y.f.mu.Lock()
	defer y.f.mu.Unlock()
	if !y.f.draining {
		return false, nil
	}
	if !y.did {
		y.did = true
		next := *y.job
		next.Trigger = TriggerContinuation
		next.Seq++
		next.Attempt = 0
		next.ScheduledAt = y.f.clk.Now()
		y.f.pending = append(y.f.pending, &next)
	}
	return true, nil
}

func (f *Fake) work(ctx context.Context, j *FakeJob) FakeRun {
	e, _ := f.reg.entry(j.Kind)
	k := e.kind
	j.Attempt++
	now := f.clk.Now()
	run := Run{
		Kind: k.Name, Class: k.Class, ID: ids.New(now), Trigger: j.Trigger,
		PeriodKey: j.PeriodKey, ScheduledAt: j.ScheduledAt, Seq: j.Seq, Attempt: j.Attempt,
		args: j.Args,
	}
	if j.PeriodKey != "" {
		run.PeriodStart, _ = k.UniqueBy.Start(j.PeriodKey)
	}
	doneKey := k.Name + "\x00" + j.PeriodKey
	if j.PeriodKey != "" && j.Trigger == TriggerSchedule {
		f.mu.Lock()
		skip := f.done[doneKey]
		f.mu.Unlock()
		if skip {
			fr := FakeRun{Run: run, Status: StatusSkipped}
			f.record(fr)
			return fr
		}
	}
	rctx, cancel := context.WithTimeout(ctx, k.Timeout)
	defer cancel()
	y := &fakeYielder{f: f, job: j}
	if k.Batched {
		rctx = withYielder(rctx, y)
	}
	res, err := k.Handler(rctx, run)
	st := outcome(ctx, err, y.did, j.Attempt, k.MaxAttempts)
	fr := FakeRun{Run: run, Status: st, Result: res, Err: err}
	if st == StatusSucceeded && j.PeriodKey != "" {
		f.mu.Lock()
		f.done[doneKey] = true
		f.mu.Unlock()
	}
	f.record(fr)
	return fr
}

func (f *Fake) record(fr FakeRun) {
	f.mu.Lock()
	f.runs = append(f.runs, fr)
	f.mu.Unlock()
}
