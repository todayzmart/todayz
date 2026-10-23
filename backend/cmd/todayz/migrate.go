package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/dbenv"
	"github.com/todayz/backend/internal/platform/migrate"
	"github.com/todayz/backend/internal/seed"
)

// cliBoot is boot for one-shot commands: config and a logger, no telemetry.
func cliBoot(service string, getenv func(string) string, out io.Writer) (*config.Config, *slog.Logger, error) {
	return loadAndLog(service, getenv, out)
}

// runMigrate runs as todayz_owner through DATABASE_MIGRATE_URL. Up refuses a
// database recorded for another APP_ENV and records APP_ENV on a new one
// (platform_environment). Down is refused in staging and prod (deployed
// schemas only move forward) and unless the database itself says local or
// test and agrees with APP_ENV.
func runMigrate(ctx context.Context, action string, getenv func(string) string, out io.Writer) error {
	switch action {
	case "up", "down", "status":
	default:
		return usageError{msg: fmt.Sprintf("unknown migrate action %q (up, down, status)", action)}
	}
	cfg, log, err := cliBoot("migrate", getenv, out)
	if err != nil {
		return err
	}
	if action == "down" && cfg.Env.IsDeployed() {
		return fmt.Errorf("migrate down is refused when APP_ENV is %s; write a new forward migration", cfg.Env)
	}
	if !cfg.DatabaseMigrateURL.IsSet() {
		return errors.New("DATABASE_MIGRATE_URL is not set (migrations run as todayz_owner)")
	}
	pool, err := db.Open(ctx, cfg.DatabaseMigrateURL.Reveal(), db.Options{AppName: "todayz-migrate", MaxConns: 2})
	if err != nil {
		return err
	}
	defer pool.Close()
	m, err := migrate.New(pool, log)
	if err != nil {
		return err
	}
	defer func() { _ = m.Close() }()

	// Only the schema owner migrates. A migration that is only DML (a settings
	// seed) would otherwise run as todayz_app as well.
	var isOwner bool
	if err := pool.QueryRow(ctx, `SELECT pg_catalog.pg_get_userbyid(nspowner) = current_user
		FROM pg_catalog.pg_namespace WHERE nspname = 'public'`).Scan(&isOwner); err != nil {
		return err
	}
	if !isOwner {
		return errors.New("DATABASE_MIGRATE_URL must connect as the owner of schema public (todayz_owner)")
	}

	switch action {
	case "up":
		if err := dbenv.CheckMatches(ctx, pool, cfg.Env); err != nil {
			return err
		}
		n, err := m.Up(ctx)
		if err != nil {
			return err
		}
		if err := dbenv.Record(ctx, pool, cfg.Env); err != nil {
			return err
		}
		log.InfoContext(ctx, "migrated up", slog.Int("applied", n), slog.Int64("head", m.Head()))
	case "down":
		if err := dbenv.RequireDev(ctx, pool, cfg.Env, "migrate down"); err != nil {
			return err
		}
		if err := m.Down(ctx); err != nil {
			return err
		}
		cur, _, err := m.Versions(ctx)
		if err != nil {
			return err
		}
		log.InfoContext(ctx, "migrated down", slog.Int64("version", cur))
	case "status":
		st, err := m.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range st {
			state := "pending"
			if s.Applied {
				state = "applied"
			}
			_, _ = fmt.Fprintf(out, "%05d  %-8s %s\n", s.Version, state, s.Name)
		}
	}
	return nil
}

// runSeed runs as todayz_app through DATABASE_URL, so seed data obeys the
// services' privileges. Refused in staging and prod, and unless the database
// itself says local or test and agrees with APP_ENV.
func runSeed(ctx context.Context, profile string, getenv func(string) string, out io.Writer) error {
	p := seed.Profile(profile)
	if !p.Valid() {
		return usageError{msg: fmt.Sprintf("unknown seed profile %q (dev, demo, test)", profile)}
	}
	cfg, log, err := cliBoot("seed", getenv, out)
	if err != nil {
		return err
	}
	if err := seed.Allowed(cfg.Env); err != nil {
		return err
	}
	if !cfg.DatabaseURL.IsSet() {
		return errors.New("DATABASE_URL is not set")
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal(), db.Options{AppName: "todayz-seed", MaxConns: 4})
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := dbenv.RequireDev(ctx, pool, cfg.Env, "seed"); err != nil {
		return err
	}
	return seed.Run(ctx, cfg.Env, p, seed.Deps{DB: db.New(pool), Log: log, Keys: cfg.Keys})
}
