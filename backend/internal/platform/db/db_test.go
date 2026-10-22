package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	sqlc "github.com/todayz/backend/gen/sqlc/platformdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func countFlags(t *testing.T, d *db.DB) int64 {
	t.Helper()
	n, err := sqlc.New(d.Pool).CountOpenRiskFlags(context.Background())
	require.NoError(t, err)
	return n
}

const insertFlag = `INSERT INTO risk_flags (id, subject_type, subject_id, rule, score)
	VALUES (gen_random_uuid(), 'test', gen_random_uuid(), 'test_rule', 10)`

// TestTxThroughContext: Q(ctx) joins the transaction in ctx, nested InTx is a
// savepoint, and sqlc queries run inside it (as todayz_app).
func TestTxThroughContext(t *testing.T) {
	tdb := testkit.NewDB(t)
	d := db.New(tdb.Pool)
	ctx := context.Background()

	boom := errors.New("boom")
	err := d.InTx(ctx, func(ctx context.Context) error {
		_, err := d.Q(ctx).Exec(ctx, insertFlag)
		require.NoError(t, err)
		inner := d.InTx(ctx, func(ctx context.Context) error {
			_, err := d.Q(ctx).Exec(ctx, insertFlag)
			require.NoError(t, err)
			return boom
		})
		require.ErrorIs(t, inner, boom)
		n, err := sqlc.New(d.Q(ctx)).CountOpenRiskFlags(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(1), n, "the savepoint rolled back only the inner insert")
		require.Zero(t, countFlags(t, d), "not visible outside the transaction yet")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), countFlags(t, d))

	require.ErrorIs(t, d.InTx(ctx, func(ctx context.Context) error {
		_, err := d.Q(ctx).Exec(ctx, insertFlag)
		require.NoError(t, err)
		return boom
	}), boom)
	require.Equal(t, int64(1), countFlags(t, d), "an error rolls back")

	require.Panics(t, func() {
		_ = d.InTx(ctx, func(ctx context.Context) error {
			_, _ = d.Q(ctx).Exec(ctx, insertFlag)
			panic("kaboom")
		})
	})
	require.Equal(t, int64(1), countFlags(t, d), "a panic rolls back")

	s, err := sqlc.New(d.Pool).GetSetting(ctx, "pickup_hold_min")
	require.NoError(t, err)
	require.Equal(t, "minutes", s.Unit)
}

func TestOpenNeverLeaksTheURL(t *testing.T) {
	_, err := db.Open(context.Background(), "postgres://someone:hunter2-secret@127.0.0.1:1/x?connect_timeout=1", db.Options{})
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "hunter2"), err.Error())
	_, err = db.Open(context.Background(), "::not a url hunter2", db.Options{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "hunter2")
}

// TestNullableTimestamptzIsPointer: sqlc maps a nullable timestamptz to
// *time.Time, so NULL is nil and never a silent zero time.
func TestNullableTimestamptzIsPointer(t *testing.T) {
	_ = func() *time.Time { return sqlc.RiskFlag{}.ReviewedAt } // compile-time: the pointer type
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	q := sqlc.New(tdb.Pool)
	var id [16]byte
	require.NoError(t, tdb.Pool.QueryRow(ctx, insertFlag+` RETURNING id`).Scan(&id))
	f, err := q.GetRiskFlag(ctx, pgtype.UUID{Bytes: id, Valid: true})
	require.NoError(t, err)
	require.Nil(t, f.ReviewedAt, "NULL reads as nil")
	require.False(t, f.CreatedAt.IsZero())
	_, err = tdb.Pool.Exec(ctx, `UPDATE risk_flags SET reviewed_at = '2026-10-22T10:00:00Z', status = 'cleared'`)
	require.NoError(t, err)
	f, err = q.GetRiskFlag(ctx, pgtype.UUID{Bytes: id, Valid: true})
	require.NoError(t, err)
	require.NotNil(t, f.ReviewedAt)
	require.True(t, f.ReviewedAt.Equal(time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)))
}
