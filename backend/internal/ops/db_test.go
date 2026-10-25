package ops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	api "github.com/todayz/backend/gen/api/admin-system"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/testkit"
)

// Its own server: pg_stat_statements needs shared_preload_libraries, which a
// shared server started before it was added may not have.
func TestMain(m *testing.M) { testkit.MainOwnServer(m) }

// SC-OPS-01 (I): locally the AWS panels are not configured and make no call,
// while Postgres and River show real data.
func TestLocalPanels(t *testing.T) {
	db := testkit.NewDB(t)
	ctx := context.Background()
	s := New(Deps{Env: api.Local, DB: db.Pool, Clock: clock.Real{}})
	out := s.System(ctx)
	require.False(t, out.AwsConfigured)
	require.Equal(t, api.AdminSystemPanelStatusNotConfigured, out.Rds.Status)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Database.Status, "%v", out.Database.ErrorClass)
	require.Positive(t, *out.Database.Connections)
	require.Equal(t, 300, *out.Database.MaxConnections)
	require.Positive(t, *out.Database.SizeBytes)
	require.Equal(t, ptr(false), out.Database.MultiAz)
	require.Equal(t, api.AdminSystemStatementsStatusOk, out.Database.StatementsStatus)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.River.Status, "%v", out.River.ErrorClass)
	require.Equal(t, int64(0), *out.River.Running)
	require.Empty(t, out.River.Queues)
	require.NotNil(t, out.River.Queues)
}

// SC-OPS-02 (I): the top statements come from pg_stat_statements, normalised
// (no literal reaches the page), this database only, slowest total first.
func TestTopStatements(t *testing.T) {
	db := testkit.NewDB(t)
	ctx := context.Background()
	_, err := db.Pool.Exec(ctx, `SELECT pg_sleep(0.5)`)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `SELECT count(*) FROM users WHERE id::text = 'literal-9f3a2c'`)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `SET application_name = 'utility-not-shown'`)
	require.NoError(t, err)
	out := New(Deps{Env: api.Local, DB: db.Pool, Clock: clock.Real{}}).System(ctx)
	require.Equal(t, api.AdminSystemStatementsStatusOk, out.Database.StatementsStatus)
	st := out.Database.Statements
	require.NotEmpty(t, st)
	require.LessOrEqual(t, len(st), 10)
	require.Contains(t, st[0].Query, "pg_sleep", "the slowest total first")
	require.GreaterOrEqual(t, st[0].Calls, int64(1))
	require.GreaterOrEqual(t, st[0].MeanMs, 450.0)
	for i, s := range st {
		require.NotContains(t, s.Query, "literal-9f3a2c")
		require.NotContains(t, strings.ToLower(s.Query), "utility-not-shown")
		require.LessOrEqual(t, len(s.Query), 500)
		if i > 0 {
			require.GreaterOrEqual(t, st[i-1].TotalMs, s.TotalMs)
		}
	}
	var normalised bool
	for _, s := range st {
		normalised = normalised || strings.Contains(s.Query, "id::text = $1")
	}
	require.True(t, normalised, "the literal is a $1 placeholder")

	// The extension missing is a status, not a failed panel.
	require.NoError(t, db.AdminExec(ctx, `DROP EXTENSION pg_stat_statements`))
	clk := clock.NewFake(time.Now())
	out = New(Deps{Env: api.Local, DB: db.Pool, Clock: clk}).System(ctx)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.Database.Status)
	require.Equal(t, api.AdminSystemStatementsStatusNotInstalled, out.Database.StatementsStatus)
	require.Empty(t, out.Database.Statements)
}

// SC-OPS-04 (I): River depth and oldest age per queue, cached 5 s.
func TestRiverPanelAndCache(t *testing.T) {
	db := testkit.NewDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	clk := clock.NewFake(now)
	ins := func(queue, state string, age time.Duration) {
		attempt, attemptedAt := 0, (*time.Time)(nil)
		if state == "running" {
			attempt, attemptedAt = 1, ptr(now.Add(-age))
		}
		_, err := db.Pool.Exec(ctx, `INSERT INTO river_job (kind, args, max_attempts, queue, state, scheduled_at, priority, attempt, attempted_at)
			VALUES ('ops_test', '{}', 1, $1, $2::river_job_state, $3, 1, $4, $5)`, queue, state, now.Add(-age), attempt, attemptedAt)
		require.NoError(t, err)
	}
	ins("default", "available", 90*time.Second)
	ins("default", "available", 10*time.Second)
	ins("notify", "retryable", 30*time.Second)
	ins("default", "running", time.Minute)
	ins("default", "available", -time.Hour) // scheduled later: not waiting yet

	s := New(Deps{Env: api.Local, DB: db.Pool, Clock: clk})
	out := s.System(ctx)
	require.Equal(t, api.AdminSystemPanelStatusOk, out.River.Status, "%v", out.River.ErrorClass)
	require.Equal(t, int64(1), *out.River.Running)
	require.Equal(t, []api.AdminSystemQueue{
		{Queue: "default", Depth: 2, OldestAgeSeconds: 90},
		{Queue: "notify", Depth: 1, OldestAgeSeconds: 30},
	}, out.River.Queues)
	require.Equal(t, now, out.River.FetchedAt)

	ins("notify", "available", 5*time.Second)
	clk.Advance(4 * time.Second)
	require.Equal(t, out.River.Queues, s.System(ctx).River.Queues, "cached for 5 s")
	clk.Advance(2 * time.Second)
	again := s.System(ctx)
	require.Equal(t, now.Add(6*time.Second), again.River.FetchedAt)
	require.Equal(t, int64(2), again.River.Queues[0].Depth)
	require.Equal(t, int64(2), again.River.Queues[1].Depth, "6 s later: the new job counts")
	require.Equal(t, 96, again.River.Queues[0].OldestAgeSeconds)
}
