package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river/rivertype"

	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/lifecycle"
	"github.com/todayz/backend/internal/platform/migrate"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// runWorker works jobs until SIGTERM, then drains (02 §2.8, D-98; see
// serveWorker). The rest closes in order on the way out: the migrator, the
// LISTEN/NOTIFY loop, the DB pool, then the telemetry flush.
func runWorker(ctx context.Context, getenv func(string) string, out io.Writer) error {
	a, err := boot(ctx, "worker", getenv, out)
	if err != nil {
		return err
	}
	defer a.close()

	pool, err := a.appPool(ctx, "worker")
	if err != nil {
		return err
	}
	defer a.closePool(ctx, pool)

	l := pgnotify.New(pool, a.log)
	clk, off, _, err := a.clocks(ctx, pool, l)
	if err != nil {
		return err
	}
	st := a.settingsCache(pool, l)
	stopLoops, err := startListener(ctx, l)
	if err != nil {
		return err
	}
	defer lifecycle.Step(ctx, a.log, "background loops", stopLoops)

	// tz_queue_depth and tz_queue_oldest_available_age_seconds, read from river_job at each scrape (W1-07).
	unregQueue, err := telemetry.RegisterQueue(nil, pool, clk.Now)
	if err != nil {
		return err
	}
	defer func() { _ = unregQueue() }()

	m, err := migrate.New(pool, nil)
	if err != nil {
		return err
	}
	defer func() { _ = m.Close() }()
	var qh queue.Health
	ready := &health.Readiness{Log: a.log, Checks: []health.Check{
		health.DBCheck(pool),
		health.MigrationsCheck(m),
		{Name: "river", Fn: func(ctx context.Context) error { return queue.Check(ctx, pool, &qh) }},
	}}

	// Every kind is declared through jobs.Define (D-95) and worked through
	// the jobs middleware; the runner also schedules the scheduled kinds.
	kinds := platformjobs.NewRegistry()
	jobs.Register(kinds, jobs.Deps{Pool: pool, Clock: clk, Settings: st, Config: a.cfg, Log: a.log})
	runner, err := platformjobs.NewRunner(kinds, platformjobs.RunnerOptions{
		Pool: pool, Clock: clk, Log: a.log, Draining: ready.Draining,
	})
	if err != nil {
		return err
	}
	defer runner.Close()
	reg := queue.NewRegistry()
	runner.Register(reg)
	var riverTime rivertype.TimeGenerator
	if off != nil {
		riverTime = off // local/test: River follows /v1/test/clock
	}
	c, err := queue.NewWorkerClient(pool, reg, queue.Options{Log: a.log, Time: riverTime})
	if err != nil {
		return err
	}
	r := chi.NewRouter()
	health.Register(r, ready, health.Options{Version: true}) // the internal port: /version in every env
	srv := httpx.NewServer(a.cfg.WorkerAddr, r)

	if err := queue.Start(ctx, c); err != nil { // detached: a signal drains, it does not abort jobs
		return err
	}
	qh.Watch(c)
	go runner.RunScheduler(ctx, c) // stops with the signal; idle while draining
	a.log.InfoContext(ctx, "worker started", slog.Int("kinds", len(reg.Kinds())))

	return serveWorker(ctx, a.log, lifecycle.Server{Srv: srv, Ready: ready}, c, queue.DrainGrace, queue.HardGrace)
}

// serveWorker serves the worker's health port until ctx ends (SIGTERM), then
// drains: /readyz turns 503 at once, River stops fetching and running jobs
// get grace to finish, then StopAndCancel gets hardGrace (queue.Drain), all
// while the health port keeps answering; then the health server shuts down.
// If the health server fails before any signal, River is drained all the
// same.
func serveWorker(ctx context.Context, log *slog.Logger, s lifecycle.Server, c *queue.Client, grace, hardGrace time.Duration) error {
	s.Name, s.Log, s.Drain = "worker health", log, lifecycle.HealthShutdown
	drained := false
	drain := func() error {
		drained = true
		log.InfoContext(context.WithoutCancel(ctx), "worker draining: no new jobs; running jobs finish",
			slog.String("grace", grace.String()), slog.String("hard_grace", hardGrace.String()))
		err := queue.Drain(c, grace, hardGrace, log)
		if err == nil {
			log.InfoContext(context.WithoutCancel(ctx), "worker drained: river stopped")
		}
		return err
	}
	s.Before = drain
	err := s.Run(ctx)
	if !drained {
		err = errors.Join(err, drain())
	}
	return err
}
