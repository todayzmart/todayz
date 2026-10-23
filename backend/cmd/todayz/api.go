package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/todayz/backend/gen/api/spec"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/mw"
	"github.com/todayz/backend/internal/platform/httpx/openapi"
	"github.com/todayz/backend/internal/platform/idempotency"
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
		Log:         d.Log,
		CORSOrigins: d.Config.CORSOrigins(),
		ClientIP:    mw.ClientIPOptions{TrustCloudflare: d.Config.Env.IsDeployed()},
		Idempotency: idempotency.Options{Store: idem, Clock: d.Clock, Log: d.Log},
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

func runAPI(ctx context.Context, getenv func(string) string, out io.Writer) error {
	a, err := boot(ctx, "api", getenv, out)
	if err != nil {
		return err
	}
	defer a.close()

	pool, err := a.appPool(ctx, "api")
	if err != nil {
		return err
	}
	defer pool.Close()

	l := pgnotify.New(pool, a.log)
	clk, _, tc, err := a.clocks(ctx, pool, l)
	if err != nil {
		return err
	}
	st := a.settingsCache(pool, l)
	if err := waitListener(ctx, l); err != nil {
		return err
	}

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
	srv := httpx.NewServer(a.cfg.APIAddr, h)
	return serve(ctx, a.log, srv, "api")
}

// serve runs srv until ctx is done, then shuts it down gracefully.
func serve(ctx context.Context, log *slog.Logger, srv *http.Server, what string) error {
	errc := make(chan error, 1)
	go func() {
		log.InfoContext(ctx, what+" listening", slog.String("addr", srv.Addr))
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	log.Info(what + " shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}
