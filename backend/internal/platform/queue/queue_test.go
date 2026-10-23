package queue

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestWorkerRunsPeriodicJobsAsAppRole: the worker, connected as todayz_app,
// elects a leader, inserts the periodic jobs and works them, and the
// maintenance job creates partitions through ensure_partitions.
func TestWorkerRunsPeriodicJobsAsAppRole(t *testing.T) {
	db := testkit.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var role string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT current_user`).Scan(&role))
	require.Equal(t, testkit.AppRole, role)

	reg := NewRegistry()
	future := time.Now().UTC().AddDate(4, 0, 0)
	hb := RegisterPlatform(reg, PlatformDeps{Pool: db.Pool, Clock: clock.NewFake(future), Log: quiet()})
	require.ElementsMatch(t, []string{"platform.heartbeat", "platform.maintenance"}, reg.Kinds())

	c, err := NewWorkerClient(db.Pool, reg, Options{Log: quiet()})
	require.NoError(t, err)
	done, cancelSub := c.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed)
	defer cancelSub()
	var h Health
	require.ErrorIs(t, Check(ctx, db.Pool, &h), ErrNotRunning)
	require.NoError(t, c.Start(ctx))
	h.Watch(c)
	require.NoError(t, Check(ctx, db.Pool, &h))

	completed := map[string]bool{}
	for len(completed) < 2 {
		select {
		case ev := <-done:
			require.Equal(t, river.EventKindJobCompleted, ev.Kind, "job %s failed: %v", ev.Job.Kind, ev.Job.Errors)
			completed[ev.Job.Kind] = true
		case <-ctx.Done():
			t.Fatalf("periodic jobs did not run; completed: %v", completed)
		}
	}
	require.GreaterOrEqual(t, hb.Runs.Load(), int64(1))

	var n int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE tablename = $1`, "audit_log_p"+future.Format("200601")).Scan(&n))
	require.Equal(t, 1, n, "maintenance created the partition for the (fake) current month")

	require.NoError(t, c.Stop(ctx))
	require.Eventually(t, func() bool { return Check(ctx, db.Pool, &h) != nil }, 5*time.Second, 10*time.Millisecond)
}

// TestScheduledJobFollowsOffsetClock: with the local/test Offset clock as
// River's time, a job scheduled 30 minutes ahead runs once the clock is
// advanced past it (what POST /v1/test/clock relies on).
func TestScheduledJobFollowsOffsetClock(t *testing.T) {
	db := testkit.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	off := clock.NewOffset(nil)
	reg := NewRegistry()
	hb := &HeartbeatWorker{}
	Add[HeartbeatArgs](reg, hb)
	c, err := NewWorkerClient(db.Pool, reg, Options{Log: quiet(), Time: off})
	require.NoError(t, err)
	done, cancelSub := c.Subscribe(river.EventKindJobCompleted)
	defer cancelSub()

	at := off.Now().Add(30 * time.Minute)
	res, err := c.Insert(ctx, HeartbeatArgs{}, &river.InsertOpts{ScheduledAt: at})
	require.NoError(t, err)
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(context.WithoutCancel(ctx)) }()

	// Deterministic: the job is stored as scheduled for the offset clock's
	// now + 30m, and nothing has completed.
	var state string
	var scheduledAt time.Time
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT state, scheduled_at FROM river_job WHERE id = $1`, res.Job.ID).Scan(&state, &scheduledAt))
	require.Equal(t, "scheduled", state)
	require.WithinDuration(t, at, scheduledAt, time.Millisecond)
	select {
	case <-done:
		t.Fatal("the job ran before its time")
	default:
	}
	require.Zero(t, hb.Runs.Load())
	off.Advance(31 * time.Minute)
	select {
	case ev := <-done:
		require.Equal(t, "platform.heartbeat", ev.Job.Kind)
	case <-time.After(20 * time.Second):
		t.Fatal("the job did not run after the clock moved past it")
	}
	require.Equal(t, int64(1), hb.Runs.Load())
}

// TestInsertClient: the API's insert-only client inserts inside a transaction.
func TestInsertClient(t *testing.T) {
	db := testkit.NewDB(t)
	ctx := context.Background()
	c, err := NewInsertClient(db.Pool, Options{Log: quiet()})
	require.NoError(t, err)
	tx, err := db.Pool.Begin(ctx)
	require.NoError(t, err)
	_, err = c.InsertTx(ctx, tx, HeartbeatArgs{}, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	var n int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job`).Scan(&n))
	require.Zero(t, n, "a rolled-back insert leaves no job")
	require.NoError(t, Check(ctx, db.Pool, nil))
}
