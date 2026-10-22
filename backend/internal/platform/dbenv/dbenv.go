// Package dbenv reads and records which APP_ENV a database belongs to, in the
// one-row platform_environment table (W4).
//
// `todayz migrate up` records APP_ENV the first time, as todayz_owner, and
// refuses to run against a database recorded for another environment. The
// app role can only read the row. The commands that destroy or fake state
// (migrate down, seed, the test clock) call RequireDev, which refuses unless
// the database says local or test and agrees with APP_ENV. So a laptop whose
// DATABASE_URL points at a staging or prod database cannot seed it, roll it
// back or move its clock, whatever APP_ENV says.
package dbenv

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/todayz/backend/internal/platform/config"
)

// Querier is the part of a pool or connection dbenv needs.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ErrMissing means the database has no platform_environment row (or no
// table: a database migrated before the table existed; `make reset` fixes a
// local one).
var ErrMissing = errors.New("dbenv: the database does not record its environment (platform_environment); run `todayz migrate up`, or `make reset` locally")

// Read returns the environment the database records.
func Read(ctx context.Context, q Querier) (config.Env, error) {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT pg_catalog.to_regclass('public.platform_environment') IS NOT NULL`).Scan(&exists); err != nil {
		return "", fmt.Errorf("dbenv: %w", err)
	}
	if !exists {
		return "", ErrMissing
	}
	var env string
	err := q.QueryRow(ctx, `SELECT env FROM public.platform_environment WHERE id`).Scan(&env)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMissing
	}
	if err != nil {
		return "", fmt.Errorf("dbenv: %w", err)
	}
	return config.Env(env), nil
}

// CheckMatches refuses when the database records an environment other than
// appEnv. A database that records none passes (migrate up records it).
func CheckMatches(ctx context.Context, q Querier, appEnv config.Env) error {
	got, err := Read(ctx, q)
	if errors.Is(err, ErrMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	if got != appEnv {
		return fmt.Errorf("refused: this database belongs to APP_ENV=%s, but APP_ENV is %s", got, appEnv)
	}
	return nil
}

// Record writes appEnv when the database records none (as the owner, after
// the migrations ran), then checks the recorded value matches.
func Record(ctx context.Context, owner Querier, appEnv config.Env) error {
	if _, err := owner.Exec(ctx, `INSERT INTO public.platform_environment (id, env) VALUES (true, $1)
		ON CONFLICT (id) DO NOTHING`, string(appEnv)); err != nil {
		return fmt.Errorf("dbenv: record: %w", err)
	}
	return CheckMatches(ctx, owner, appEnv)
}

// RequireDev refuses what unless the database records local or test and
// agrees with appEnv.
func RequireDev(ctx context.Context, q Querier, appEnv config.Env, what string) error {
	got, err := Read(ctx, q)
	if err != nil {
		return fmt.Errorf("%s refused: %w", what, err)
	}
	if got.IsDeployed() || !got.Valid() {
		return fmt.Errorf("%s refused: this database belongs to APP_ENV=%s", what, got)
	}
	if got != appEnv {
		return fmt.Errorf("%s refused: this database belongs to APP_ENV=%s, but APP_ENV is %s", what, got, appEnv)
	}
	return nil
}
