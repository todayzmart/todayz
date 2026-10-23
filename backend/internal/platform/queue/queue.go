// Package queue wraps River (02 §2.6): durable jobs in Postgres, inserted in
// the same transaction as the change that needs them.
//
// The worker process builds a full client from a Registry; the API builds an
// insert-only client. Domains declare job kinds with jobs.Define
// (internal/platform/jobs) through internal/jobs (one file per domain); the
// jobs platform registers the workers here and schedules the scheduled kinds
// itself (no River periodic jobs).
//
// Role contract: River's tables are created by the migrations (as
// todayz_owner); the services run River as todayz_app with DML only. River's
// reindexer needs table ownership, so it is turned off here.
package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// Client is the River client type the backend uses.
type Client = river.Client[pgx.Tx]

// JobArgs is river.JobArgs.
type JobArgs = river.JobArgs

// Worker is river.Worker.
type Worker[T river.JobArgs] = river.Worker[T]

// Registry collects workers and named queues before the client is built.
// Every worker comes from a jobs.Define declaration (internal/platform/jobs
// registers them here); a lint test refuses river.AddWorker, periodic jobs
// and Add anywhere else outside tests.
type Registry struct {
	Workers *river.Workers
	kinds   []string
	queues  map[string]int
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{Workers: river.NewWorkers(), queues: map[string]int{}} }

// Add registers a worker for its job kind. Only tests and the jobs platform
// call it (the registry lint test); domains declare kinds with jobs.Define.
func Add[T river.JobArgs](r *Registry, w river.Worker[T]) {
	river.AddWorker(r.Workers, w)
	var zero T
	r.kinds = append(r.kinds, zero.Kind())
}

// AddArgs registers w for the kind named by args.Kind() (river.AddWorkerArgs):
// the jobs platform uses one args type for every kind.
func AddArgs[T river.JobArgs](r *Registry, args T, w river.Worker[T]) {
	river.AddWorkerArgs(r.Workers, args, w)
	r.kinds = append(r.kinds, args.Kind())
}

// DefaultNamedQueueWorkers is how many jobs a named (non-default) queue works
// at once.
const DefaultNamedQueueWorkers = 5

// AddQueue makes the worker client fetch from the named queue too, with
// maxWorkers (DefaultNamedQueueWorkers when 0). The default queue is always
// configured.
func (r *Registry) AddQueue(name string, maxWorkers int) {
	if name == "" || name == river.QueueDefault {
		return
	}
	if maxWorkers <= 0 {
		maxWorkers = DefaultNamedQueueWorkers
	}
	r.queues[name] = maxWorkers
}

// Kinds lists the registered job kinds.
func (r *Registry) Kinds() []string { return append([]string(nil), r.kinds...) }

// Options configure a client.
type Options struct {
	Log *slog.Logger
	// MaxWorkers for the default queue (default 20).
	MaxWorkers int
	// Time, when set, is River's clock: the local/test *clock.Offset, so jobs
	// scheduled for "in 30 minutes" fall due when /v1/test/clock advances.
	// Nil (staging, prod) means River uses the real and database clocks.
	Time rivertype.TimeGenerator
	// MeterProvider for tz_job_hard_cancelled_total (the global provider
	// when nil; tests pass their own reader).
	MeterProvider metric.MeterProvider
}

func baseConfig(o Options) *river.Config {
	if o.Log == nil {
		o.Log = telemetry.Default()
	}
	cfg := &river.Config{
		Logger:              o.Log.With(slog.String("component", "river")),
		ReindexerIndexNames: []string{}, // needs ownership; todayz_app has DML only
	}
	if o.Time != nil {
		cfg.Test.Time = o.Time
	}
	return cfg
}

// NewWorkerClient returns a client that works jobs (the worker process).
func NewWorkerClient(pool *pgxpool.Pool, reg *Registry, o Options) (*Client, error) {
	if o.MaxWorkers <= 0 {
		o.MaxWorkers = 20
	}
	cfg := baseConfig(o)
	cfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: o.MaxWorkers}}
	for name, n := range reg.queues {
		cfg.Queues[name] = river.QueueConfig{MaxWorkers: n}
	}
	cfg.Workers = reg.Workers
	f := newInflight(o.MeterProvider) // lets Drain name the jobs it hard-cancels
	cfg.Middleware = append(cfg.Middleware, f)
	c, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("queue: %w", err)
	}
	f.zero(reg.Kinds()) // the series exist at 0 from the start, so increase() sees the first cancel
	trackers.Store(c, f)
	return c, nil
}

// NewInsertClient returns an insert-only client (the API process).
func NewInsertClient(pool *pgxpool.Pool, o Options) (*Client, error) {
	c, err := river.NewClient(riverpgxv5.New(pool), baseConfig(o))
	if err != nil {
		return nil, fmt.Errorf("queue: %w", err)
	}
	return c, nil
}

// Start starts c detached from ctx's cancellation (W1). River hard-stops
// (cancels every running job) when its start context ends, so a SIGTERM that
// cancels ctx would abort jobs mid-way; Drain stops the client instead.
func Start(ctx context.Context, c *Client) error {
	if err := c.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("queue: start: %w", err)
	}
	return nil
}

// Health tracks whether a worker client is running, for /readyz.
type Health struct {
	started atomic.Bool
	stopped atomic.Bool
}

// Watch marks h started and watches c until it stops.
func (h *Health) Watch(c *Client) {
	h.started.Store(true)
	go func() {
		<-c.Stopped()
		h.stopped.Store(true)
	}()
}

// ErrNotRunning is reported by Check for a worker client that stopped.
var ErrNotRunning = errors.New("queue: river client not running")

// Check reports whether River is usable: its tables answer for this role and,
// when h watches a worker client, the client is running.
func Check(ctx context.Context, pool *pgxpool.Pool, h *Health) error {
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_queue`).Scan(&n); err != nil {
		return fmt.Errorf("queue: river tables: %w", err)
	}
	if h != nil && (!h.started.Load() || h.stopped.Load()) {
		return ErrNotRunning
	}
	return nil
}
