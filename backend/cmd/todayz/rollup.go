package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/dbenv"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/testclock"
	"github.com/todayz/backend/internal/rollups"
)

// maxBackfillDays bounds one `todayz rollup` call; a longer backfill is
// several calls.
const maxBackfillDays = 400 //settings:ok an operator guard, not a product number

// runRollup is `todayz rollup --from YYYY-MM-DD --to YYYY-MM-DD` (W3-07, 05 §3):
// it recomputes every daily rollup for those IST days, then the cohorts and
// item pairs, as todayz_app. Rollups are recomputed from the raw tables, so
// running it twice writes the same rows. In local and test it runs on the
// database's test clock, like the worker.
func runRollup(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("rollup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fromS := fs.String("from", "", "first IST day, YYYY-MM-DD")
	toS := fs.String("to", "", "last IST day, YYYY-MM-DD (default: --from)")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return usageError{msg: "rollup takes --from YYYY-MM-DD [--to YYYY-MM-DD]"}
	}
	if *fromS == "" {
		return usageError{msg: "rollup needs --from YYYY-MM-DD"}
	}
	if *toS == "" {
		*toS = *fromS
	}
	from, err := rollups.ParseDay(*fromS)
	if err != nil {
		return usageError{msg: err.Error()}
	}
	to, err := rollups.ParseDay(*toS)
	if err != nil {
		return usageError{msg: err.Error()}
	}
	if to.Before(from) {
		return usageError{msg: "--to is before --from"}
	}
	if !to.Before(from.Add(maxBackfillDays)) {
		return usageError{msg: fmt.Sprintf("at most %d days per call", maxBackfillDays)}
	}

	cfg, log, err := cliBoot("rollup", getenv, out)
	if err != nil {
		return err
	}
	if !cfg.DatabaseURL.IsSet() {
		return errors.New("DATABASE_URL is not set")
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal(), db.Options{AppName: "todayz-rollup", MaxConns: 4})
	if err != nil {
		return err
	}
	defer pool.Close()

	var clk clock.Clock = clock.Real{}
	if cfg.Env.AllowsTestRoutes() && dbenv.RequireDev(ctx, pool, cfg.Env, "test clock") == nil {
		off := clock.NewOffset(nil)
		if err := testclock.New(off, testclock.PGStore{Pool: pool}, log).Load(ctx); err != nil {
			return err
		}
		clk = off
	}
	svc := rollups.New(rollups.Deps{DB: db.New(pool), Clock: clk,
		Settings: settings.NewCache(settings.PGLoader(pool), settings.Options{Log: log})})
	if svc.Today().Before(to) {
		return usageError{msg: fmt.Sprintf("--to %s is after today (%s)", to, svc.Today())}
	}
	log.InfoContext(ctx, "rollup backfill started", slog.String("trigger", "backfill"),
		slog.String("from", from.String()), slog.String("to", to.String()))
	res, err := svc.Backfill(ctx, from, to)
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "rollup backfill done", slog.String("trigger", "backfill"),
		slog.Int64("days", res.Days), slog.Int64("store_rows", res.Stores), slog.Int64("demand_rows", res.Demand),
		slog.Int64("banner_rows", res.Banners), slog.Int64("cohort_rows", res.Cohorts), slog.Int64("item_pairs", res.Pairs))
	return nil
}
