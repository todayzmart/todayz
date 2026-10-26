package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/todayz/backend/internal/imageimport"
	"github.com/todayz/backend/internal/platform/buildinfo"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/storage"
)

// maxImportLimit bounds one `todayz images import` call.
const maxImportLimit = 20000 //settings:ok an operator guard, not a product number

// runImages is `todayz images import` (D-252): products without an image get
// a draft image from Open Food Facts, Open Beauty Facts or Open Products
// Facts, copied into the private bucket and waiting for an admin's approval.
// It calls those sites, so it never runs under TODAYZ_NO_EGRESS.
func runImages(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	if len(args) > 0 && args[0] == "types" {
		return runImageTypes(ctx, args[1:], getenv, out)
	}
	if len(args) == 0 || args[0] != "import" {
		return usageError{msg: "images takes import [--dry-run] [--limit N] [--source off|obf|opf] [--contact URL] [--report FILE], or types [--dry-run] [--limit N] [--contact URL] [--report FILE]"}
	}
	fs := flag.NewFlagSet("images import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dry := fs.Bool("dry-run", false, "look up only; copy and store nothing")
	limit := fs.Int("limit", 20, "products to try")
	source := fs.String("source", "", "only this database: off, obf or opf")
	contact := fs.String("contact", "https://todayz.in", "contact in the User-Agent")
	report := fs.String("report", "", "write a Markdown report to this file")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		return usageError{msg: "images import takes [--dry-run] [--limit N] [--source off|obf|opf] [--contact URL] [--report FILE]"}
	}
	if *limit < 1 || *limit > maxImportLimit {
		return usageError{msg: "--limit must be between 1 and 20000"}
	}
	if *contact == "" {
		return usageError{msg: "--contact must not be empty"}
	}

	cfg, log, err := cliBoot("images", getenv, out)
	if err != nil {
		return err
	}
	if cfg.NoEgress {
		return errors.New("images import calls Open Food Facts; it cannot run with TODAYZ_NO_EGRESS")
	}
	if !cfg.DatabaseURL.IsSet() {
		return errors.New("DATABASE_URL is not set")
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal(), db.Options{AppName: "todayz-images", MaxConns: 2})
	if err != nil {
		return err
	}
	defer pool.Close()

	deps := imageimport.Deps{
		DB:     db.New(pool),
		Client: httpx.NewOutboundClient(httpx.OutboundOptions{Timeout: 30 * time.Second}),
		Clock:  clock.Real{},
		Log:    log,
	}
	if st := storage.New(cfg.Storage, httpx.NewOutboundClient(httpx.OutboundOptions{})); st != nil {
		deps.Store = st
	}
	im, err := imageimport.New(deps, imageimport.Options{
		UserAgent: imageimport.UserAgent(buildinfo.Get().Version, *contact),
		Limit:     *limit, DryRun: *dry, Only: *source,
	})
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "image import started", slog.Int("limit", *limit), slog.Bool("dry_run", *dry))
	rep, runErr := im.Run(ctx)
	log.InfoContext(ctx, "image import done", slog.Int("tried", rep.Tried), slog.Int("found", len(rep.Imported)),
		slog.Int("not_found", len(rep.Missed)), slog.Int("medicines", len(rep.Medicines)),
		slog.Int("no_key", len(rep.NoKey)), slog.Int("failed", len(rep.Failed)))
	if *report != "" {
		f, err := os.Create(*report)
		if err != nil {
			return err
		}
		werr := rep.WriteMarkdown(f)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
	}
	return runErr
}

// runImageTypes is `todayz images types` (D-253): each kind of item with a
// product still on the placeholder gets up to three free-licence candidate
// photos from Wikimedia Commons, waiting for an admin to pick one in A-08;
// approved kinds' photos go to products added since. It calls Commons, so it
// never runs under TODAYZ_NO_EGRESS.
func runImageTypes(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("images types", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dry := fs.Bool("dry-run", false, "search only; copy and store nothing")
	again := fs.Bool("again", false, "first drop waiting candidates, so their kinds are searched again")
	limit := fs.Int("limit", 20, "kinds to search")
	contact := fs.String("contact", "https://todayz.in", "contact in the User-Agent")
	report := fs.String("report", "", "write a Markdown report to this file")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return usageError{msg: "images types takes [--dry-run] [--again] [--limit N] [--contact URL] [--report FILE]"}
	}
	if *limit < 1 || *limit > maxImportLimit {
		return usageError{msg: "--limit must be between 1 and 20000"}
	}
	if *contact == "" {
		return usageError{msg: "--contact must not be empty"}
	}
	cfg, log, err := cliBoot("images", getenv, out)
	if err != nil {
		return err
	}
	if cfg.NoEgress {
		return errors.New("images types calls Wikimedia Commons; it cannot run with TODAYZ_NO_EGRESS")
	}
	if !cfg.DatabaseURL.IsSet() {
		return errors.New("DATABASE_URL is not set")
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal(), db.Options{AppName: "todayz-images", MaxConns: 2})
	if err != nil {
		return err
	}
	defer pool.Close()
	deps := imageimport.Deps{
		DB:     db.New(pool),
		Client: httpx.NewOutboundClient(httpx.OutboundOptions{Timeout: 30 * time.Second}),
		Clock:  clock.Real{},
		Log:    log,
	}
	if st := storage.New(cfg.Storage, httpx.NewOutboundClient(httpx.OutboundOptions{})); st != nil {
		deps.Store = st
	}
	ti, err := imageimport.NewTypes(deps, imageimport.TypeOptions{
		UserAgent: imageimport.UserAgent(buildinfo.Get().Version, *contact), Limit: *limit, DryRun: *dry, Again: *again,
	})
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "type photos started", slog.Int("limit", *limit), slog.Bool("dry_run", *dry))
	rep, runErr := ti.Run(ctx)
	log.InfoContext(ctx, "type photos done", slog.Int("filled", rep.Filled), slog.Int("cleared", rep.Cleared), slog.Int("searched", rep.Searched),
		slog.Int("found", len(rep.Found)), slog.Int("not_found", len(rep.Missed)),
		slog.Int("medicines", len(rep.Medicines)), slog.Int("failed", len(rep.Failed)))
	if *report != "" {
		f, err := os.Create(*report)
		if err != nil {
			return err
		}
		werr := rep.WriteMarkdown(f)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
	}
	return runErr
}
