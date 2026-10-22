package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func TestWriteAppendsAndJoinsTx(t *testing.T) {
	tdb := testkit.NewDB(t)
	d := db.New(tdb.Pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	w := New(d, clock.NewFake(now), []byte("0123456789abcdef0123456789abcdef"))

	actor := ids.New(now)
	rctx := reqctx.WithRequestID(reqctx.WithClientIP(ctx, "203.0.113.7"), "req-1")
	id, err := w.Write(rctx, Entry{
		ActorKind: ActorAdmin, ActorID: actor, Action: "settings.update",
		SubjectType: "platform_setting", SubjectID: "pickup_hold_min",
		Before: map[string]int{"value": 30}, After: json.RawMessage(`{"value":45}`),
	})
	require.NoError(t, err)

	var (
		kind, action, subject, reqID string
		before, after                []byte
		ipHash                       []byte
		created                      time.Time
	)
	require.NoError(t, d.Pool.QueryRow(ctx, `SELECT actor_kind, action, subject_id, request_id, before, after, ip_hash, created_at
		FROM audit_log WHERE id = $1`, ids.UUID(id)).Scan(&kind, &action, &subject, &reqID, &before, &after, &ipHash, &created))
	require.Equal(t, ActorAdmin, kind)
	require.Equal(t, "settings.update", action)
	require.Equal(t, "pickup_hold_min", subject)
	require.Equal(t, "req-1", reqID)
	require.JSONEq(t, `{"value":30}`, string(before))
	require.JSONEq(t, `{"value":45}`, string(after))
	require.Len(t, ipHash, 32, "the IP is stored only as a keyed hash")
	require.NotContains(t, string(ipHash), "203.0.113.7")
	require.True(t, created.Equal(now))

	// Inside a rolled-back transaction the row disappears with the change.
	boom := errors.New("boom")
	var rolledBack ids.ID
	err = d.InTx(ctx, func(ctx context.Context) error {
		rolledBack, err = w.Write(ctx, Entry{ActorKind: ActorSystem, Action: "jobs.ran", SubjectType: "job"})
		require.NoError(t, err)
		return boom
	})
	require.ErrorIs(t, err, boom)
	var n int
	require.NoError(t, d.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE id = $1`, ids.UUID(rolledBack)).Scan(&n))
	require.Zero(t, n)

	// Without a key no ip_hash is stored.
	w2 := New(d, clock.Real{}, nil)
	id2, err := w2.Write(rctx, Entry{ActorKind: ActorSystem, Action: "jobs.ran", SubjectType: "job"})
	require.NoError(t, err)
	require.NoError(t, d.Pool.QueryRow(ctx, `SELECT ip_hash FROM audit_log WHERE id = $1`, ids.UUID(id2)).Scan(&ipHash))
	require.Nil(t, ipHash)
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	tdb := testkit.NewDB(t)
	d := db.New(tdb.Pool)
	ctx := context.Background()
	w := New(d, clock.Real{}, nil)
	id, err := w.Write(ctx, Entry{ActorKind: ActorSystem, Action: "jobs.ran", SubjectType: "job"})
	require.NoError(t, err)

	// The app role has no UPDATE, DELETE or TRUNCATE privilege on the table or
	// on any partition (a partition is checked only when named directly).
	for _, rel := range append([]string{"audit_log"}, partitions(t, tdb.OwnerPool)...) {
		for _, sql := range []string{
			`UPDATE ` + rel + ` SET action = 'x.y' WHERE id = $1`,
			`DELETE FROM ` + rel + ` WHERE id = $1`,
		} {
			_, err := d.Pool.Exec(ctx, sql, ids.UUID(id))
			require.ErrorContains(t, err, "permission denied", sql)
		}
		_, err := d.Pool.Exec(ctx, `TRUNCATE `+rel)
		require.ErrorContains(t, err, "permission denied", rel)
	}
	// Even the schema owner is refused UPDATE, DELETE and TRUNCATE, on the
	// table and on every partition. The row trigger fires only on a row, so
	// the default partition gets one too (an old date lands there); a
	// partition without rows is checked with TRUNCATE.
	_, err = w.Write(ctx, Entry{ActorKind: ActorSystem, Action: "jobs.ran", SubjectType: "job"})
	require.NoError(t, err)
	_, err = tdb.OwnerPool.Exec(ctx, `INSERT INTO audit_log (id, actor_kind, action, subject_type, created_at)
		VALUES (gen_random_uuid(), 'system', 'jobs.ran', 'job', '2001-01-01')`)
	require.NoError(t, err)
	for _, rel := range append([]string{"audit_log"}, partitions(t, tdb.OwnerPool)...) {
		var n int
		require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT count(*) FROM `+rel).Scan(&n))
		stmts := []string{`TRUNCATE ` + rel}
		if n > 0 {
			stmts = append(stmts, `UPDATE `+rel+` SET action = 'x.y'`, `DELETE FROM `+rel)
		}
		for _, sql := range stmts {
			_, err := tdb.OwnerPool.Exec(ctx, sql)
			require.ErrorContains(t, err, "append-only", sql)
		}
	}
	requireRows(t, tdb.OwnerPool, 3)
}

// partitions lists audit_log's partitions (default included).
func partitions(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'audit_log'::regclass ORDER BY 1`)
	require.NoError(t, err)
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.NotEmpty(t, names)
	return names
}

func requireRows(t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log`).Scan(&n))
	require.Equal(t, want, n, "audit_log rows")
}

// appConn opens a new todayz_app session (temporary tables and SET live per
// session, so pooled connections are not reused).
func appConn(t *testing.T, tdb *testkit.DB) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), tdb.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// TestAuditLogShadowAttack is the review's B1 attack: as todayz_app, turn on
// the maintenance switch, shadow pg_class with a temporary table that claims
// the app owns audit_log, then DELETE every row. Three walls stop it, and
// each is tested on its own: no TEMPORARY privilege, no DELETE privilege, and
// an append_only trigger that pins its search_path and names pg_catalog.
func TestAuditLogShadowAttack(t *testing.T) {
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	w := New(db.New(tdb.Pool), clock.Real{}, nil)
	for range 3 {
		_, err := w.Write(ctx, Entry{ActorKind: ActorSystem, Action: "jobs.ran", SubjectType: "job"})
		require.NoError(t, err)
	}
	attack := []string{
		`SET todayz.partition_maintenance = 'on'`,
		`CREATE TEMP TABLE pg_class (oid oid, relowner oid, relname name)`,
		`INSERT INTO pg_temp.pg_class SELECT c.oid, r.oid, c.relname FROM pg_catalog.pg_class c, pg_catalog.pg_roles r
		  WHERE r.rolname = current_user AND c.relname LIKE 'audit_log%'`,
		`DELETE FROM audit_log`,
	}
	run := func(c *pgx.Conn) error {
		for _, sql := range attack {
			if _, err := c.Exec(ctx, sql); err != nil {
				return err
			}
		}
		return nil
	}

	// Wall 1: the app cannot create temporary tables (the attack stops at
	// its CREATE TEMP step).
	c := appConn(t, tdb)
	_, err := c.Exec(ctx, attack[0])
	require.NoError(t, err)
	_, err = c.Exec(ctx, attack[1])
	require.ErrorContains(t, err, "permission denied to create temporary tables")
	require.Error(t, run(appConn(t, tdb)))
	requireRows(t, tdb.OwnerPool, 3)

	// Wall 2: even with TEMPORARY, the app has no DELETE privilege.
	_, err = tdb.OwnerPool.Exec(ctx, `GRANT TEMPORARY ON DATABASE `+tdb.Name+` TO todayz_app`)
	require.NoError(t, err)
	err = run(appConn(t, tdb))
	require.ErrorContains(t, err, "permission denied for table audit_log")
	requireRows(t, tdb.OwnerPool, 3)

	// Wall 3: with TEMPORARY and DELETE granted by the owner, the trigger
	// still reads the real pg_catalog.pg_class and refuses.
	rels := strings.Join(append([]string{"audit_log"}, partitions(t, tdb.OwnerPool)...), ", ")
	_, err = tdb.OwnerPool.Exec(ctx, `GRANT DELETE ON `+rels+` TO todayz_app`)
	require.NoError(t, err)
	err = run(appConn(t, tdb))
	require.ErrorContains(t, err, "append-only")
	requireRows(t, tdb.OwnerPool, 3)
}

// TestEnsurePartitionsShadowAttack: a session that shadows partition_policies,
// pg_class and pg_partitioned_table with temporary tables, and names tables
// that have no policy, gets no partitions.
func TestEnsurePartitionsShadowAttack(t *testing.T) {
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	countRels := func() int {
		var n int
		require.NoError(t, tdb.OwnerPool.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relkind IN ('r','p')
			AND relnamespace = 'public'::regnamespace`).Scan(&n))
		return n
	}
	// A real partitioned table in public with no policy row.
	_, err := tdb.OwnerPool.Exec(ctx, `CREATE TABLE no_policy (created_at timestamptz NOT NULL) PARTITION BY RANGE (created_at)`)
	require.NoError(t, err)
	_, err = tdb.OwnerPool.Exec(ctx, `GRANT TEMPORARY ON DATABASE `+tdb.Name+` TO todayz_app`)
	require.NoError(t, err)
	before := countRels()

	c := appConn(t, tdb)
	// The app cannot add itself a policy.
	_, err = c.Exec(ctx, `INSERT INTO partition_policies (parent, granularity, ahead) VALUES ('no_policy', 'day', 400)`)
	require.ErrorContains(t, err, "permission denied")
	_, err = c.Exec(ctx, `UPDATE partition_policies SET granularity = 'day', ahead = 400`)
	require.ErrorContains(t, err, "permission denied")

	for _, sql := range []string{
		`CREATE TEMP TABLE partition_policies (parent text, granularity text, ahead int)`,
		`INSERT INTO pg_temp.partition_policies VALUES ('audit_log', 'day', 400), ('no_policy', 'day', 400), ('evil', 'day', 400)`,
		`CREATE TEMP TABLE pg_class (oid oid, relname name, relnamespace oid, relkind "char", relowner oid)`,
		`INSERT INTO pg_temp.pg_class SELECT c.oid, c.relname, 'public'::regnamespace, 'p', r.oid
		   FROM pg_catalog.pg_class c, pg_catalog.pg_roles r WHERE r.rolname = 'todayz_owner' AND c.relname = 'no_policy'`,
		`CREATE TEMP TABLE evil (created_at timestamptz NOT NULL) PARTITION BY RANGE (created_at)`,
	} {
		_, err := c.Exec(ctx, sql)
		require.NoError(t, err, sql)
	}
	// The real policy for audit_log is month/3, so nothing new is due: a
	// shadowed day/400 policy would create 400 daily partitions here.
	var created int
	require.NoError(t, c.QueryRow(ctx, `SELECT ensure_partitions('audit_log', now())`).Scan(&created))
	require.Zero(t, created)
	for _, parent := range []string{"no_policy", "evil", "pg_temp.evil", "audit_log_default"} {
		_, err := c.Exec(ctx, `SELECT ensure_partitions($1, now())`, parent)
		require.ErrorContains(t, err, "no row in partition_policies", parent)
	}
	// A policy row the owner wrote for a table that is not partitioned is refused too.
	_, err = tdb.OwnerPool.Exec(ctx, `CREATE TABLE plain (created_at timestamptz NOT NULL);
		INSERT INTO partition_policies VALUES ('plain', 'day', 2)`)
	require.NoError(t, err)
	_, err = c.Exec(ctx, `SELECT ensure_partitions('plain', now())`)
	require.ErrorContains(t, err, "is not a partitioned table")
	require.Equal(t, before+1, countRels(), "only the owner's own plain table is new")
}

// TestNewPartitionsKeepTheGuards: partitions created later carry the parent's
// privileges (no UPDATE/DELETE/TRUNCATE for the app, although the default
// privileges would grant UPDATE and DELETE) and its TRUNCATE trigger.
func TestNewPartitionsKeepTheGuards(t *testing.T) {
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	future := time.Now().UTC().AddDate(3, 0, 0)
	var created int
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT ensure_partitions('audit_log', $1)`, future).Scan(&created))
	require.Equal(t, 3, created)
	part := "audit_log_p" + future.Format("200601")

	var privs []string
	rows, err := tdb.OwnerPool.Query(ctx, `SELECT a.privilege_type FROM pg_class c, aclexplode(c.relacl) a
		WHERE c.relname = $1 AND a.grantee = 'todayz_app'::regrole ORDER BY 1`, part)
	require.NoError(t, err)
	privs, err = pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.Equal(t, []string{"INSERT", "SELECT"}, privs)

	_, err = tdb.OwnerPool.Exec(ctx, `TRUNCATE `+part)
	require.ErrorContains(t, err, "append-only")
	_, err = tdb.Pool.Exec(ctx, `TRUNCATE `+part)
	require.ErrorContains(t, err, "permission denied")
}

func TestEnsurePartitionsMovesDefaultRows(t *testing.T) {
	tdb := testkit.NewDB(t)
	d := db.New(tdb.Pool)
	ctx := context.Background()
	// Two years ahead: past the three months the migration creates, so the row
	// lands in the default partition.
	future := time.Now().UTC().AddDate(2, 0, 0)
	w := New(d, clock.NewFake(future), nil)
	id, err := w.Write(ctx, Entry{ActorKind: ActorSystem, Action: "jobs.ran", SubjectType: "job"})
	require.NoError(t, err)

	where := func() string {
		var rel string
		require.NoError(t, d.Pool.QueryRow(ctx, `SELECT tableoid::regclass::text FROM audit_log WHERE id = $1`, ids.UUID(id)).Scan(&rel))
		return rel
	}
	require.Equal(t, "audit_log_default", where())

	var created int
	require.NoError(t, d.Pool.QueryRow(ctx, `SELECT ensure_partitions('audit_log', $1)`, future).Scan(&created))
	require.Equal(t, 3, created, "the audit_log policy keeps 3 months ahead")
	require.Equal(t, "audit_log_p"+future.Format("200601"), where(), "the row moved out of the default partition")

	// Running it again is a no-op.
	require.NoError(t, d.Pool.QueryRow(ctx, `SELECT ensure_partitions('audit_log', $1)`, future).Scan(&created))
	require.Zero(t, created)

	// The maintenance switch is transaction-local: a plain DELETE by the owner
	// is still refused by the trigger.
	_, err = tdb.OwnerPool.Exec(ctx, `DELETE FROM audit_log WHERE id = $1`, ids.UUID(id))
	require.ErrorContains(t, err, "append-only")
}

func TestWriteValidates(t *testing.T) {
	w := New(nil, clock.Real{}, nil)
	ctx := context.Background()
	for name, e := range map[string]Entry{
		"actor kind":   {ActorKind: "robot", Action: "a.b", SubjectType: "x"},
		"action":       {ActorKind: ActorSystem, Action: "nodot", SubjectType: "x"},
		"subject type": {ActorKind: ActorSystem, Action: "a.b", SubjectType: "Bad Type"},
		"actor id":     {ActorKind: ActorUser, Action: "a.b", SubjectType: "x"},
		"bad json":     {ActorKind: ActorSystem, Action: "a.b", SubjectType: "x", After: json.RawMessage(`{`)},
	} {
		_, err := w.Write(ctx, e)
		require.Error(t, err, name)
	}
}
