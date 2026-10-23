package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// The worker drain (02 §2.8, D-98). A SIGTERM'd worker stops fetching jobs
// and gives running ones DrainGrace to finish (more than the 60 s job cap of
// W1-11); a job still running after that is cancelled (StopAndCancel), which
// gets HardGrace to return. DrainGrace + HardGrace (120 s) must stay under the
// worker's container stop timeout (180 s, lifecycle.WorkerStopTimeout), or the
// container runtime kills the process mid-drain.
const (
	DrainGrace = 90 * time.Second
	HardGrace  = 30 * time.Second
)

// HardCancelledMetric counts job attempts cancelled by StopAndCancel at
// shutdown, by kind (05 §8). The alert "Job hard-cancelled at shutdown"
// watches it; the cancelled attempt is retried by River.
const HardCancelledMetric = telemetry.MetricPrefix + "job_hard_cancelled_total"

// inflight tracks the jobs a worker client is working right now, so Drain can
// name (log and count) the ones it hard-cancels. It is installed as River
// worker middleware by NewWorkerClient.
type inflight struct {
	river.MiddlewareDefaults

	mu      sync.Mutex
	running map[int64]string // job ID → kind

	counter metric.Int64Counter
}

func newInflight(mp metric.MeterProvider) *inflight {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	f := &inflight{running: map[int64]string{}}
	c, err := mp.Meter("todayz/queue").Int64Counter(HardCancelledMetric,
		metric.WithDescription("Job attempts cancelled by StopAndCancel at shutdown (D-98)"))
	if err == nil {
		f.counter = c
	}
	return f
}

// zero adds 0 to the counter once per kind, so every kind's series exists at
// 0 for the worker's whole life. An increase() alert compares two samples;
// a series whose first sample is already 1 never shows an increase.
func (f *inflight) zero(kinds []string) {
	if f.counter == nil {
		return
	}
	for _, k := range kinds {
		f.counter.Add(context.Background(), 0, metric.WithAttributes(attribute.String("kind", k)))
	}
}

// Work implements rivertype.WorkerMiddleware.
func (f *inflight) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	f.mu.Lock()
	f.running[job.ID] = job.Kind
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.running, job.ID)
		f.mu.Unlock()
	}()
	return doInner(ctx)
}

type runningJob struct {
	id   int64
	kind string
}

// snapshot lists the jobs running now, by ID.
func (f *inflight) snapshot() []runningJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]runningJob, 0, len(f.running))
	for id, kind := range f.running {
		out = append(out, runningJob{id: id, kind: kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// trackers maps a worker client to its in-flight tracker. River's client has
// no slot for it, and Drain takes the client.
var trackers sync.Map // *Client → *inflight

func trackerFor(c *Client) *inflight {
	if v, ok := trackers.Load(c); ok {
		return v.(*inflight)
	}
	return nil
}

// Drain stops c gracefully (D-98): no new jobs are fetched, and running jobs
// get up to grace to finish. Jobs still running then are hard-cancelled: each
// is logged at error level and counted in tz_job_hard_cancelled_total{kind},
// then StopAndCancel cancels their contexts and waits up to hardGrace for
// them to return. River retries a cancelled attempt; every job is idempotent.
//
// Drain does not depend on the caller's context: it runs after SIGTERM.
func Drain(c *Client, grace, hardGrace time.Duration, log *slog.Logger) error {
	if log == nil {
		log = telemetry.Default()
	}
	defer trackers.Delete(c)
	bg := context.Background()
	ctx, cancel := context.WithTimeout(bg, grace)
	defer cancel()
	err := c.Stop(ctx)
	if err == nil {
		return nil
	}
	// Any failed Stop escalates: a deadline means jobs outlived the grace;
	// any other error leaves the client in an unknown state, and only
	// StopAndCancel is sure to end its jobs before the process exits.
	var stopErr error
	if !errors.Is(err, context.DeadlineExceeded) {
		stopErr = fmt.Errorf("queue: stop: %w", err)
		log.ErrorContext(bg, "queue: stop failed; hard-cancelling", slog.String("err", err.Error()))
	}

	f := trackerFor(c)
	var still []runningJob
	if f != nil {
		still = f.snapshot()
	}
	log.ErrorContext(bg, "queue: jobs still running after the drain grace; hard-cancelling them (they are retried)",
		slog.String("grace", grace.String()), slog.String("hard_grace", hardGrace.String()), slog.Int("jobs", len(still)))
	for _, j := range still {
		log.ErrorContext(bg, "queue: job hard-cancelled at shutdown",
			slog.String("kind", j.kind), slog.Int64("job_id", j.id))
		if f.counter != nil {
			f.counter.Add(bg, 1, metric.WithAttributes(attribute.String("kind", j.kind)))
		}
	}

	hctx, hcancel := context.WithTimeout(bg, hardGrace)
	defer hcancel()
	if err := c.StopAndCancel(hctx); err != nil {
		log.ErrorContext(bg, "queue: jobs did not return after the hard cancel",
			slog.String("hard_grace", hardGrace.String()), slog.String("err", err.Error()))
		return errors.Join(stopErr, fmt.Errorf("queue: stop and cancel: %w", err))
	}
	return stopErr
}
