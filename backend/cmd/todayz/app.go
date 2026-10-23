package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/todayz/backend/internal/platform/buildinfo"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/dbenv"
	"github.com/todayz/backend/internal/platform/egress"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/lifecycle"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
	"github.com/todayz/backend/internal/platform/testclock"
)

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func asUsage(err error, out *usageError) bool { return errors.As(err, out) }

func runVersion(stdout io.Writer) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildinfo.Get()); err != nil {
		return 1
	}
	return 0
}

// app is what every long-running command shares.
type app struct {
	cfg      *config.Config
	log      *slog.Logger
	shutdown func(context.Context) error
}

func version(cfg *config.Config) string {
	if cfg != nil && cfg.Version != "" {
		return cfg.Version
	}
	return buildinfo.Get().Version
}

// boot loads config, builds the logger and starts telemetry. It logs the
// names of unset variables, never a value.
func boot(ctx context.Context, service string, getenv config.Getenv, out io.Writer) (*app, error) {
	cfg, log, err := loadAndLog(service, getenv, out)
	if err != nil {
		return nil, err
	}
	_, shutdown, err := telemetry.Setup(ctx, telemetry.Options{
		Service:      service,
		Env:          string(cfg.Env),
		Version:      version(cfg),
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
		OTLPHeaders:  cfg.Telemetry.OTLPHeaders.Reveal(),
		SentryDSN:    cfg.Telemetry.SentryDSN.Reveal(),
		HTTPClient:   httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: cfg.NoEgress}),
		Log:          log,
	})
	if err != nil {
		return nil, err
	}
	log.InfoContext(ctx, "starting", slog.Bool("no_egress", cfg.NoEgress))
	return &app{cfg: cfg, log: log, shutdown: shutdown}, nil
}

// loadAndLog loads config and builds the process logger. Before anything
// else can log or dial, it makes the redacting logger the process default
// (slog.Default and the standard log package) and, under no-egress, replaces
// http.DefaultTransport with the loopback-only guard.
func loadAndLog(service string, getenv config.Getenv, out io.Writer) (*config.Config, *slog.Logger, error) {
	pre := telemetry.NewLogger(out, telemetry.LogOptions{Service: service, Env: getenv("APP_ENV"), Version: buildinfo.Get().Version})
	telemetry.SetDefault(pre)
	cfg, err := config.Load(getenv, pre)
	if err != nil {
		return nil, nil, err
	}
	log := telemetry.NewLogger(out, telemetry.LogOptions{
		Level: cfg.LogLevel, Service: service, Env: string(cfg.Env), Version: version(cfg),
	})
	telemetry.SetDefault(log)
	if cfg.NoEgress {
		egress.InstallGuard()
	}
	return cfg, log, nil
}

// close flushes telemetry (bounded): the last step of the shutdown, after the
// DB pool has closed (02 §2.8).
func (a *app) close() {
	ctx, cancel := context.WithTimeout(context.Background(), lifecycle.TelemetryFlush)
	defer cancel()
	if err := a.shutdown(ctx); err != nil {
		a.log.Warn("telemetry shutdown", slog.String("err", err.Error()))
	}
	a.log.Info("shutdown: telemetry flushed")
}

// appPool opens DATABASE_URL (todayz_app: DML only).
func (a *app) appPool(ctx context.Context, name string) (*pgxpool.Pool, error) {
	if !a.cfg.DatabaseURL.IsSet() {
		return nil, errors.New("DATABASE_URL is not set")
	}
	return db.Open(ctx, a.cfg.DatabaseURL.Reveal(), db.Options{AppName: "todayz-" + name})
}

// clocks returns the process clock and, in local and test, the test-clock
// service that moves it (D-80). Staging and prod get the real clock and nil.
func (a *app) clocks(ctx context.Context, pool *pgxpool.Pool, l *pgnotify.Listener) (clock.Clock, *clock.Offset, *testclock.Service, error) {
	if !a.cfg.Env.AllowsTestRoutes() {
		return clock.Real{}, nil, nil, nil
	}
	// The database must say local or test too (W4): a local binary pointed at
	// a staging or prod database runs on the real clock, without the route.
	if err := dbenv.RequireDev(ctx, pool, a.cfg.Env, "test clock"); err != nil {
		a.log.ErrorContext(ctx, "test clock disabled", slog.String("err", err.Error()))
		return clock.Real{}, nil, nil, nil
	}
	off := clock.NewOffset(nil)
	tc := testclock.New(off, testclock.PGStore{Pool: pool}, a.log)
	if err := tc.Load(ctx); err != nil {
		return nil, nil, nil, err
	}
	tc.Attach(l)
	if off.Offset() != 0 {
		a.log.WarnContext(ctx, "test clock is shifted", slog.Int64("offset_ms", off.Offset().Milliseconds()))
	}
	return off, off, tc, nil
}

// settingsCache returns the settings cache following NOTIFY on l.
func (a *app) settingsCache(pool *pgxpool.Pool, l *pgnotify.Listener) *settings.Cache {
	c := settings.NewCache(settings.PGLoader(pool), settings.Options{Log: a.log})
	c.Attach(l)
	return c
}

// startListener starts l and waits for its first LISTEN (bounded). l is an
// in-process loop (settings cache, test clock): it runs on its own context,
// not the signal context, so settings keep following NOTIFY while requests
// and jobs drain (02 §2.8). stop ends it and waits for it to return, which
// must happen before the pool closes.
func startListener(ctx context.Context, l *pgnotify.Listener) (stop func(), err error) {
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = l.Run(lctx)
	}()
	stop = func() {
		cancel()
		<-done
	}
	select {
	case <-l.Ready():
		return stop, nil
	case <-ctx.Done():
		stop()
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		stop()
		return nil, fmt.Errorf("pgnotify: no LISTEN connection after 10s")
	}
}
