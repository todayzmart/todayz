// Package db opens the pgx v5 pool and runs transactions that travel through
// the context (02 §2.2): a service starts InTx, and every repository call made
// with that ctx joins the same transaction, so money-moving operations across
// domains commit or roll back together.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is what sqlc-generated code and repositories need. Both *pgxpool.Pool
// and pgx.Tx satisfy it.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	CopyFrom(ctx context.Context, table pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error)
}

// Options tune the pool.
type Options struct {
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	// AppName is sent as application_name (shows in pg_stat_activity).
	AppName string
}

// Open parses url, applies opts and pings. The URL is never put in an error.
func Open(ctx context.Context, url string, opts Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("db: DATABASE_URL is not a valid postgres URL")
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if opts.MinConns > 0 {
		cfg.MinConns = opts.MinConns
	}
	if opts.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = opts.MaxConnLifetime
	}
	if opts.AppName != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = opts.AppName
	}
	// All timestamps are UTC (02 §2.5).
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", redact(err))
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", redact(err))
	}
	return pool, nil
}

// redact drops connection details pgx may include (host, user) from errors.
func redact(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return fmt.Errorf("postgres %s: %s", pgErr.Code, pgErr.Message)
	}
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return errors.New("cannot connect to postgres")
	}
	return err
}

type txKey struct{}

// WithTx returns ctx carrying tx.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFrom returns the transaction in ctx, if any.
func TxFrom(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

// DB wraps the pool. Q(ctx) returns the transaction in ctx, or the pool.
type DB struct {
	Pool *pgxpool.Pool
}

// New wraps pool.
func New(pool *pgxpool.Pool) *DB { return &DB{Pool: pool} }

// Q returns the querier for ctx: the surrounding transaction, or the pool.
func (d *DB) Q(ctx context.Context) Querier {
	if tx, ok := TxFrom(ctx); ok {
		return tx
	}
	return d.Pool
}

// TxOptions select isolation and access mode.
type TxOptions = pgx.TxOptions

// InTx runs fn in a transaction passed through ctx. If ctx already carries a
// transaction, fn joins it through a savepoint (so an inner failure rolls back
// only the inner work and the outer caller decides). fn's error rolls back;
// a panic rolls back and re-panics.
func (d *DB) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return d.InTxOpts(ctx, pgx.TxOptions{}, fn)
}

// InTxOpts is InTx with options; options are ignored when joining an outer transaction.
func (d *DB) InTxOpts(ctx context.Context, opts pgx.TxOptions, fn func(ctx context.Context) error) (err error) {
	var tx pgx.Tx
	if outer, ok := TxFrom(ctx); ok {
		tx, err = outer.Begin(ctx) // savepoint
	} else {
		tx, err = d.Pool.BeginTx(ctx, opts)
	}
	if err != nil {
		return fmt.Errorf("db: begin: %w", redact(err))
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
	}()
	if err := fn(WithTx(ctx, tx)); err != nil {
		if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("db: rollback: %w", rbErr))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", redact(err))
	}
	return nil
}

// IsUniqueViolation reports a 23505 error, optionally on a named constraint.
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// IsNoRows reports pgx.ErrNoRows.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
