package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/todayz/backend/internal/platform/queue"
)

// The scheduler enqueues each scheduled kind's latest due tick. It is the
// jobs platform's own, not River's periodic jobs, for three reasons:
//
//   - the tick is computed from the injected clock (the test clock locally,
//     D-80), so a clock jump runs the period it lands in, the way SC-JB-02
//     and SC-JB-05 drive it;
//   - the run carries its period key and the tick it was due at (lateness,
//     UniqueBy), and its River unique key is "<period>#0", so several
//     workers, a restart or a catch-up never run a period twice;
//   - after downtime only the latest period is enqueued (a missed earlier
//     one is the watchdog's to report, not to replay), and on start the
//     latest period runs if it has not (River's RunOnStart, minus the
//     duplicate).

// periodTakenSQL: a period with a succeeded or running run, or any scheduled
// or continuation run, is not enqueued again by the scheduler (a failed
// scheduled run is River's to retry; a discarded one waits for a person or
// the next period).
const periodTakenSQL = `SELECT EXISTS (SELECT 1 FROM job_runs WHERE kind = $1 AND period_key = $2
	AND status <> 'missed' AND (status IN ('succeeded', 'running') OR trigger IN ('schedule', 'continuation')))`

// EnqueueDue enqueues the latest due tick of every scheduled kind that has
// not been enqueued by this runner yet, is not paused and whose period has
// not run. It returns how many runs it enqueued.
func (r *Runner) EnqueueDue(ctx context.Context, c *queue.Client) (int, error) {
	now := r.o.Clock.Now()
	n := 0
	var errs []error
	for _, name := range r.reg.Names() {
		e, _ := r.reg.entry(name)
		if e.sched == nil {
			continue
		}
		tick := e.sched.Prev(now)
		if tick.IsZero() {
			continue
		}
		r.mu.Lock()
		last := r.lastTick[name]
		r.mu.Unlock()
		if tick.Equal(last) {
			continue
		}
		ok, err := r.enqueueTick(ctx, c, e, tick)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		r.mu.Lock()
		r.lastTick[name] = tick
		r.mu.Unlock()
		if ok {
			n++
		}
	}
	return n, errors.Join(errs...)
}

func (r *Runner) enqueueTick(ctx context.Context, c *queue.Client, e *entry, tick time.Time) (bool, error) {
	k := e.kind
	period := k.UniqueBy.Key(tick)
	if k.Class != ClassTimer {
		paused, err := r.paused(ctx, k.Name)
		if err != nil {
			return false, fmt.Errorf("jobs: %s: %w", k.Name, err)
		}
		if paused { // D-95: a paused scheduled kind does not run at its ticks
			r.o.Log.InfoContext(ctx, "job paused; tick not enqueued", slog.String("kind", k.Name), slog.String("period", period))
			return false, nil
		}
	}
	var taken bool
	if err := r.o.Pool.QueryRow(ctx, periodTakenSQL, k.Name, period).Scan(&taken); err != nil {
		return false, fmt.Errorf("jobs: %s: %w", k.Name, err)
	}
	if taken {
		return false, nil
	}
	env := envelope{kind: k.Name, U: period + "#0", P: period, T: TriggerSchedule, S: tick}
	res, err := c.Insert(ctx, env, &river.InsertOpts{Queue: queueOf(k), MaxAttempts: k.MaxAttempts,
		ScheduledAt: tick, UniqueOpts: river.UniqueOpts{ByArgs: true}})
	if err != nil {
		return false, fmt.Errorf("jobs: enqueue %s %s: %w", k.Name, period, err)
	}
	return !res.UniqueSkippedAsDuplicate, nil
}

// RunScheduler calls EnqueueDue every SchedulerPoll until ctx ends. It does
// nothing while the worker drains. Errors are logged at most once a minute
// per distinct message.
func (r *Runner) RunScheduler(ctx context.Context, c *queue.Client) {
	t := time.NewTicker(r.o.SchedulerPoll)
	defer t.Stop()
	var lastErr string
	var lastLogged time.Time
	for {
		if !r.draining() {
			if _, err := r.EnqueueDue(ctx, c); err != nil && ctx.Err() == nil {
				now := r.o.Clock.Now()
				if msg := err.Error(); msg != lastErr || now.Sub(lastLogged) >= time.Minute {
					r.o.Log.ErrorContext(ctx, "jobs scheduler: enqueue failed", slog.String("err", scrubError(err)))
					lastErr, lastLogged = msg, now
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
