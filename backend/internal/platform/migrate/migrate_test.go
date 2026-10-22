package migrate_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/migrate"
	settingspkg "github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestRiverVersionIsLatest fails after a River upgrade that ships new schema
// migrations, until a new Go migration installs them.
func TestRiverVersionIsLatest(t *testing.T) {
	m, err := rivermigrate.New(riverpgxv5.New(nil), nil)
	require.NoError(t, err)
	all := m.AllVersions()
	require.Equal(t, migrate.RiverVersion, all[len(all)-1].Version)
}

func tables(t *testing.T, db *testkit.DB) []string {
	t.Helper()
	rows, err := db.Pool.Query(context.Background(),
		`SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename NOT IN ('goose_db_version', 'spatial_ref_sys') ORDER BY 1`)
	require.NoError(t, err)
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	return out
}

func TestUpDownUp(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewEmptyDB(t)
	m, err := migrate.New(db.OwnerPool, quiet())
	require.NoError(t, err)
	defer func() { _ = m.Close() }()

	ok, err := m.AtHead(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	n, err := m.Up(ctx)
	require.NoError(t, err)
	st, err := m.Status(ctx)
	require.NoError(t, err)
	require.Len(t, st, n) // numbers have gaps (D-89 blocks), so count, not Head()
	ok, err = m.AtHead(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	first := tables(t, db)
	for _, want := range []string{"platform_settings", "audit_log", "audit_log_default", "risk_flags", "idempotency_keys", "test_clock", "river_job", "river_queue", "river_migration"} {
		require.Contains(t, first, want)
	}

	var settings int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM platform_settings`).Scan(&settings))
	require.Equal(t, len(settingspkg.AllKeys), settings) // every 01 §17 key is seeded

	st, err = m.Status(ctx)
	require.NoError(t, err)
	for _, s := range st {
		require.True(t, s.Applied, s.Name)
	}

	require.NoError(t, m.DownTo(ctx, 0))
	require.Empty(t, tables(t, db), "down removes everything W0-03 created")
	cur, head, err := m.Versions(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), cur)
	require.Equal(t, m.Head(), head)

	_, err = m.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, first, tables(t, db))

	// One step down and up again.
	require.NoError(t, m.Down(ctx))
	ok, err = m.AtHead(ctx)
	require.NoError(t, err)
	require.False(t, ok)
	_, err = m.Up(ctx)
	require.NoError(t, err)
}

func TestExtensions(t *testing.T) {
	db := testkit.NewDB(t)
	rows, err := db.Pool.Query(context.Background(), `SELECT extname FROM pg_extension`)
	require.NoError(t, err)
	var got []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		got = append(got, s)
	}
	for _, want := range []string{"postgis", "pg_trgm", "unaccent", "citext", "pgcrypto"} {
		require.Contains(t, got, want)
	}
}

// TestRoleContract: migrations run as todayz_owner; the services connect as
// todayz_app, which can read and write rows but never change the schema.
func TestRoleContract(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t)

	var owner string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE tablename = 'platform_settings'`).Scan(&owner))
	require.Equal(t, testkit.OwnerRole, owner)

	for _, ddl := range []string{
		`CREATE TABLE app_made (id int)`,
		`ALTER TABLE risk_flags ADD COLUMN x int`,
		`DROP TABLE risk_flags`,
		`TRUNCATE risk_flags`,
		`CREATE INDEX ix_app ON risk_flags (rule)`,
	} {
		_, err := db.Pool.Exec(ctx, ddl)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr, ddl)
		require.Equal(t, "42501", pgErr.Code, ddl) // insufficient_privilege
	}

	// DML works: insert, read, update, delete.
	_, err := db.Pool.Exec(ctx, `INSERT INTO risk_flags (id, subject_type, subject_id, rule, score)
		VALUES (gen_random_uuid(), 'store', gen_random_uuid(), 'test.rule', 10)`)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `UPDATE risk_flags SET score = 20`)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `DELETE FROM risk_flags`)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `UPDATE platform_settings SET value = '31' WHERE key = 'pickup_hold_min'`)
	require.NoError(t, err, "the settings NOTIFY trigger fires as todayz_app")

	// The app role runs the SECURITY DEFINER partition helper, and the
	// partitions it creates belong to the owner.
	var created int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT ensure_partitions('audit_log', now() + interval '3 years')`).Scan(&created))
	require.Positive(t, created, "the months up to the date are created")
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT tableowner FROM pg_tables WHERE tablename LIKE 'audit_log_p%' ORDER BY tablename DESC LIMIT 1`).Scan(&owner))
	require.Equal(t, testkit.OwnerRole, owner)

	// The app role cannot use the maintenance switch to delete audit rows.
	_, err = db.Pool.Exec(ctx, `INSERT INTO audit_log (id, actor_kind, action, subject_type, created_at) VALUES (gen_random_uuid(), 'system', 'jobs.ran', 'job', now())`)
	require.NoError(t, err)
	tx, err := db.Pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT set_config('todayz.partition_maintenance', 'on', true)`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM audit_log`)
	require.ErrorContains(t, err, "permission denied", "the app has no DELETE at all")
	_ = tx.Rollback(ctx)

	// The switch is honoured only for the table owner (ensure_partitions runs
	// as the owner); any other role that somehow had DELETE is still refused
	// by the trigger.
	_, err = db.OwnerPool.Exec(ctx, `GRANT DELETE ON audit_log TO todayz_app`)
	require.NoError(t, err)
	tx, err = db.Pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT set_config('todayz.partition_maintenance', 'on', true)`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM audit_log`)
	require.ErrorContains(t, err, "append-only")
}

// TestMissingExtensionFailsClearly: the owner is not a superuser, so a
// missing extension it cannot create stops the migration with a message
// that says what to do.
func TestMissingExtensionFailsClearly(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewEmptyDB(t)
	require.NoError(t, db.AdminExec(ctx, `DROP EXTENSION postgis`))
	m, err := migrate.New(db.OwnerPool, quiet())
	require.NoError(t, err)
	defer func() { _ = m.Close() }()
	_, err = m.Up(ctx)
	require.ErrorContains(t, err, "extension postgis is missing")
}
