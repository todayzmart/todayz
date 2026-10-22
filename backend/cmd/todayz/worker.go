package main

import (
	"context"
	"io"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river/rivertype"

	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/migrate"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/platform/queue"
)

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
	defer pool.Close()

	l := pgnotify.New(pool, a.log)
	clk, off, _, err := a.clocks(ctx, pool, l)
	if err != nil {
		return err
	}
	st := a.settingsCache(pool, l)
	if err := waitListener(ctx, l); err != nil {
		return err
	}

	reg := queue.NewRegistry()
	jobs.Register(reg, jobs.Deps{Pool: pool, Clock: clk, Settings: st, Config: a.cfg, Log: a.log})
	var riverTime rivertype.TimeGenerator
	if off != nil {
		riverTime = off // local/test: River follows /v1/test/clock
	}
	c, err := queue.NewWorkerClient(pool, reg, queue.Options{Log: a.log, Time: riverTime})
	if err != nil {
		return err
	}

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
	r := chi.NewRouter()
	health.Register(r, ready, health.Options{Version: true}) // the internal port: /version in every env
	srv := httpx.NewServer(a.cfg.WorkerAddr, r)

	if err := queue.Start(ctx, c); err != nil { // detached: a signal drains, it does not abort jobs
		return err
	}
	qh.Watch(c)
	a.log.InfoContext(ctx, "worker started", slog.Int("kinds", len(reg.Kinds())), slog.Int("periodic", len(reg.Periodic)))

	err = serve(ctx, a.log, srv, "worker health")

	// Running jobs finish (up to DrainGrace), then the rest are cancelled.
	if serr := queue.Drain(c, queue.DrainGrace, a.log); serr != nil {
		a.log.WarnContext(context.WithoutCancel(ctx), "worker stop", slog.String("err", serr.Error()))
	}
	return err
}
