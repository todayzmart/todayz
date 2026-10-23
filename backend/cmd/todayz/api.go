package main

import (
	"context"
	"fmt"
	"io"

	"github.com/todayz/backend/gen/api/spec"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/mw"
	"github.com/todayz/backend/internal/platform/httpx/openapi"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/lifecycle"
	"github.com/todayz/backend/internal/platform/migrate"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/platform/queue"
)

// buildRouter builds the router: the 02 §2.4 pipeline, then routes, then
// Router.Check (D-100): a route the contract does not know, a sub-router,
// or a code policy weaker than the contract's refuses to start the API. v
// validates every request against the contract and supplies the route
// policies the contract declares (x-idempotency, x-upload); nil skips both
// (only for tests of other slots).
func buildRouter(d deps, idem idempotency.Store, v *openapi.Validator) (*httpx.Router, error) {
	o := httpx.Options{
		Log:               d.Log,
		CORSOrigins:       d.Config.CORSOrigins(),
		PublicCORSOrigins: d.Config.PublicCORSOrigins(),
		ClientIP:          mw.ClientIPOptions{TrustCloudflare: d.Config.Env.IsDeployed()},
		Idempotency:       idempotency.Options{Store: idem, Clock: d.Clock, Log: d.Log},
	}
	if v != nil {
		o.Validator, o.Policies = v, v
	}
	r := httpx.NewRouter(o)
	routes(r, d)
	if err := r.Check(); err != nil {
		return nil, fmt.Errorf("routes: %w", err)
	}
	return r, nil
}

// runAPI serves the API until SIGTERM, then drains (02 §2.8, D-98): /readyz
// turns 503 at once, the server keeps serving for SHUTDOWN_PRESTOP, then
// srv.Shutdown waits up to SHUTDOWN_DRAIN for in-flight requests. The rest
// closes in order on the way out (the defers run last-first): the migrator,
// the LISTEN/NOTIFY loop, the DB pool, then the telemetry flush.
func runAPI(ctx context.Context, getenv func(string) string, out io.Writer) error {
	a, err := boot(ctx, "api", getenv, out)
	if err != nil {
		return err
	}
	defer a.close()

	// Refuse to start with a drain the deploy would cut (SC-GS-03).
	t, err := lifecycle.APITimings(a.cfg.Env.IsDeployed(), a.cfg.Shutdown.PreStop, a.cfg.Shutdown.Drain)
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}

	pool, err := a.appPool(ctx, "api")
	if err != nil {
		return err
	}
	defer a.closePool(ctx, pool)

	l := pgnotify.New(pool, a.log)
	clk, _, tc, err := a.clocks(ctx, pool, l)
	if err != nil {
		return err
	}
	st := a.settingsCache(pool, l)
	stopLoops, err := startListener(ctx, l)
	if err != nil {
		return err
	}
	defer lifecycle.Step(ctx, a.log, "background loops", stopLoops)

	q, err := queue.NewInsertClient(pool, queue.Options{Log: a.log})
	if err != nil {
		return err
	}
	m, err := migrate.New(pool, nil)
	if err != nil {
		return err
	}
	defer func() { _ = m.Close() }()

	ready := &health.Readiness{Log: a.log, Checks: []health.Check{
		health.DBCheck(pool),
		health.MigrationsCheck(m),
		{Name: "river", Fn: func(ctx context.Context) error { return queue.Check(ctx, pool, nil) }},
	}}
	d := deps{
		Config: a.cfg, Log: a.log, Clock: clk, DB: db.New(pool), Settings: st,
		Queue: q, Ready: ready, TestClock: tc,
	}
	// Stored idempotent responses are sealed under PII_ENC_KEY (D-18).
	idem, err := idempotency.NewPGStore(pool, a.cfg.Keys.PIIEncKey.Bytes())
	if err != nil {
		return fmt.Errorf("api: PII_ENC_KEY is required (make setup writes it; make env-check shows set/missing): %w", err)
	}
	// The bundled contract (gen/api/spec, written by make gen) validates every request.
	v, err := openapi.New(spec.Bundle)
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}
	h, err := buildRouter(d, idem, v)
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}
	s := lifecycle.Server{
		Name: "api", Srv: httpx.NewServer(a.cfg.APIAddr, h), Ready: ready,
		PreStop: t.PreStop, Drain: t.Drain, Log: a.log,
	}
	return s.Run(ctx)
}
