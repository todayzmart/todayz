package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/todayz/backend/internal/imageimport"
	"github.com/todayz/backend/internal/platform/audit"
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
		return usageError{msg: "images takes import [--dry-run] [--limit N] [--source off|obf|opf] [--newest] [--listed-first] [--search-first] [--contact URL] [--report FILE], or types [--dry-run] [--again] [--limit N] [--picks FILE|builtin] [--contact URL] [--report FILE]"}
	}
	fs := flag.NewFlagSet("images import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dry := fs.Bool("dry-run", false, "look up only; copy and store nothing")
	limit := fs.Int("limit", 20, "products to try")
	newest := fs.Bool("newest", false, "try recently added products first (local demo)")
	listedFirst := fs.Bool("listed-first", false, "try products listed in shops before the rest")
	searchFirst := fs.Bool("search-first", false, "search by name before the (slow) barcode lookup")
	source := fs.String("source", "", "only this database: off, obf or opf")
	contact := fs.String("contact", "https://todayz.in", "contact in the User-Agent")
	report := fs.String("report", "", "write a Markdown report to this file")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		return usageError{msg: "images import takes [--dry-run] [--limit N] [--source off|obf|opf] [--newest] [--listed-first] [--search-first] [--contact URL] [--report FILE]"}
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
		Limit:     *limit, DryRun: *dry, Only: *source, Newest: *newest, ListedFirst: *listedFirst, SearchFirst: *searchFirst,
	})
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "image import started", slog.Int("limit", *limit), slog.Bool("dry_run", *dry))
	rep, runErr := im.Run(ctx)
	log.InfoContext(ctx, "image import done", slog.Int("tried", rep.Tried), slog.Int("found", len(rep.Imported)),
		slog.Int("not_found", len(rep.Missed)), slog.Int("medicines", len(rep.Medicines)),
		slog.Int("no_key", len(rep.NoKey)), slog.Int("failed", len(rep.Failed)))
	return writeReport(*report, rep, runErr)
}

// runImageTypes is `todayz images types` (D-253): each kind of item with a
// product still on the placeholder gets up to three free-licence candidate
// photos from Wikimedia Commons, waiting for an admin to pick one in A-08;
// approved kinds' photos go to products added since. With --picks it instead
// re-applies a saved pick list (D-265): each listed kind without a photo gets
// that exact Commons file, approved by the system; "builtin" is the founder's
// list compiled into the binary. It calls Commons, so it never runs under
// TODAYZ_NO_EGRESS.
func runImageTypes(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("images types", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dry := fs.Bool("dry-run", false, "search only; copy and store nothing")
	again := fs.Bool("again", false, "first drop waiting candidates, so their kinds are searched again")
	limit := fs.Int("limit", 20, "kinds to search")
	contact := fs.String("contact", "https://todayz.in", "contact in the User-Agent")
	report := fs.String("report", "", "write a Markdown report to this file")
	picksFile := fs.String("picks", "", "apply this saved pick list (CSV), or builtin")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return usageError{msg: "images types takes [--dry-run] [--again] [--limit N] [--picks FILE|builtin] [--contact URL] [--report FILE]"}
	}
	var picks []imageimport.Pick
	if *picksFile != "" {
		mixed := false
		fs.Visit(func(f *flag.Flag) { mixed = mixed || f.Name == "again" || f.Name == "limit" })
		if mixed {
			return usageError{msg: "--picks does not take --again or --limit"}
		}
		var err error
		if picks, err = readPicks(*picksFile); err != nil {
			return err
		}
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
	deps.Audit = audit.New(deps.DB, deps.Clock, cfg.Keys.PIIHashKey.Bytes())
	ti, err := imageimport.NewTypes(deps, imageimport.TypeOptions{
		UserAgent: imageimport.UserAgent(buildinfo.Get().Version, *contact), Limit: *limit, DryRun: *dry, Again: *again,
	})
	if err != nil {
		return err
	}
	if *picksFile != "" {
		log.InfoContext(ctx, "type photo picks started", slog.Int("picks", len(picks)), slog.Bool("dry_run", *dry))
		rep, runErr := ti.Picks(ctx, picks)
		log.InfoContext(ctx, "type photo picks done", slog.Int("filled", rep.Filled), slog.Int("picked", len(rep.Picked)),
			slog.Int("kept", len(rep.Kept)), slog.Int("failed", len(rep.Failed)))
		return writeReport(*report, rep, runErr)
	}
	log.InfoContext(ctx, "type photos started", slog.Int("limit", *limit), slog.Bool("dry_run", *dry))
	rep, runErr := ti.Run(ctx)
	log.InfoContext(ctx, "type photos done", slog.Int("filled", rep.Filled), slog.Int("cleared", rep.Cleared), slog.Int("searched", rep.Searched),
		slog.Int("found", len(rep.Found)), slog.Int("not_found", len(rep.Missed)),
		slog.Int("medicines", len(rep.Medicines)), slog.Int("failed", len(rep.Failed)))
	return writeReport(*report, rep, runErr)
}

// readPicks reads a pick list file, or the one compiled in ("builtin").
func readPicks(name string) ([]imageimport.Pick, error) {
	if name == "builtin" {
		return imageimport.ReadPicks(bytes.NewReader(imageimport.BuiltinPicks))
	}
	f, err := os.Open(name) // #nosec G304 -- the operator names the file
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return imageimport.ReadPicks(f)
}

// writeReport writes a run's Markdown report when a file was asked for; the
// run's own error wins over the report's.
func writeReport(name string, rep interface{ WriteMarkdown(io.Writer) error }, runErr error) error {
	if name == "" {
		return runErr
	}
	f, err := os.Create(name) // #nosec G304 -- the operator names the file
	if err != nil {
		return errors.Join(runErr, err)
	}
	werr := rep.WriteMarkdown(f)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if runErr != nil {
		return runErr
	}
	return werr
}
