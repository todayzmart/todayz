// Package migrate runs the goose migrations embedded from backend/db/migrations
// plus one Go migration that installs River's schema (version RiverVersion),
// so the whole schema has one version line and readyz can check it is at head.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"

	dbschema "github.com/todayz/backend/db"
)

// RiverGooseVersion is the goose version of the Go migration that installs River.
const RiverGooseVersion = 3

// RiverVersion is the River schema version that migration installs. When River
// is upgraded and ships new migrations, add a new Go migration with the new
// target; TestRiverVersionIsLatest fails until you do.
const RiverVersion = 8

// Migrator wraps a goose provider over a pgx pool.
type Migrator struct {
	p  *goose.Provider
	db *sql.DB
}

// New builds a migrator on pool. It takes a session advisory lock while
// migrating, so two instances never migrate at once.
func New(pool *pgxpool.Pool, log *slog.Logger) (*Migrator, error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	return newWith(sqlDB, dbschema.Migrations, log)
}

func newWith(sqlDB *sql.DB, files fs.FS, log *slog.Logger) (*Migrator, error) {
	sub, err := fs.Sub(files, "migrations")
	if err != nil {
		return nil, err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	opts := []goose.ProviderOption{
		goose.WithSessionLocker(locker),
		goose.WithDisableGlobalRegistry(true),
		goose.WithGoMigrations(riverMigration()),
	}
	if log != nil {
		opts = append(opts, goose.WithSlog(log))
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, sub, opts...)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Migrator{p: p, db: sqlDB}, nil
}

func riverMigration() *goose.Migration {
	// River runs each of its versions in its own transaction (version 006
	// adds an enum value and then uses it, which one transaction refuses), so
	// this Go migration runs outside goose's transaction. River records its
	// own versions in river_migration, so a re-run after a partial failure
	// resumes where it stopped.
	run := func(dir rivermigrate.Direction, target int) func(ctx context.Context, db *sql.DB) error {
		return func(ctx context.Context, db *sql.DB) error {
			m, err := rivermigrate.New(riverdatabasesql.New(db), nil)
			if err != nil {
				return err
			}
			_, err = m.Migrate(ctx, dir, &rivermigrate.MigrateOpts{TargetVersion: target})
			return err
		}
	}
	return goose.NewGoMigration(RiverGooseVersion,
		&goose.GoFunc{RunDB: run(rivermigrate.DirectionUp, RiverVersion)},
		&goose.GoFunc{RunDB: run(rivermigrate.DirectionDown, -1)},
	)
}

// Close releases the database/sql wrapper (not the pool).
func (m *Migrator) Close() error { return m.db.Close() }

// Up applies every pending migration and returns how many ran.
func (m *Migrator) Up(ctx context.Context) (int, error) {
	res, err := m.p.Up(ctx)
	return len(res), err
}

// Down rolls back the latest migration.
func (m *Migrator) Down(ctx context.Context) error {
	_, err := m.p.Down(ctx)
	if errors.Is(err, goose.ErrNoNextVersion) {
		return nil
	}
	return err
}

// DownTo rolls back to version (0 removes everything).
func (m *Migrator) DownTo(ctx context.Context, version int64) error {
	_, err := m.p.DownTo(ctx, version)
	return err
}

// Status is one migration's state.
type Status struct {
	Version int64
	Name    string
	Applied bool
}

// Status lists every migration.
func (m *Migrator) Status(ctx context.Context) ([]Status, error) {
	st, err := m.p.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(st))
	for _, s := range st {
		name := "river"
		if s.Source.Path != "" {
			name = s.Source.Path
		}
		out = append(out, Status{Version: s.Source.Version, Name: name, Applied: s.State == goose.StateApplied})
	}
	return out, nil
}

// Versions returns the database version and the newest known version.
func (m *Migrator) Versions(ctx context.Context) (current, head int64, err error) {
	return m.p.GetVersions(ctx)
}

// AtHead reports whether nothing is pending.
func (m *Migrator) AtHead(ctx context.Context) (bool, error) {
	pending, err := m.p.HasPending(ctx)
	return !pending, err
}

// Head is the newest migration version the binary carries.
func (m *Migrator) Head() int64 {
	src := m.p.ListSources()
	if len(src) == 0 {
		return 0
	}
	return src[len(src)-1].Version
}
