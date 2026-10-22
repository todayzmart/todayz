package queue

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
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

type slowArgs struct{}

func (slowArgs) Kind() string { return "test.slow" }

// slowWorker blocks until release closes or its context is cancelled.
type slowWorker struct {
	river.WorkerDefaults[slowArgs]
	started   chan struct{}
	release   chan struct{}
	cancelled atomic.Bool
	finished  atomic.Bool
}

func (w *slowWorker) Work(ctx context.Context, _ *river.Job[slowArgs]) error {
	close(w.started)
	select {
	case <-w.release:
		w.finished.Store(true)
		return nil
	case <-ctx.Done():
		w.cancelled.Store(true)
		return ctx.Err()
	}
}

func startSlow(t *testing.T, db *testkit.DB, ctx context.Context) (*Client, *slowWorker, int64) {
	t.Helper()
	w := &slowWorker{started: make(chan struct{}), release: make(chan struct{})}
	reg := NewRegistry()
	Add[slowArgs](reg, w)
	c, err := NewWorkerClient(db.Pool, reg, Options{Log: quiet()})
	require.NoError(t, err)
	require.NoError(t, Start(ctx, c))
	res, err := c.Insert(context.WithoutCancel(ctx), slowArgs{}, nil)
	require.NoError(t, err)
	select {
	case <-w.started:
	case <-time.After(20 * time.Second):
		t.Fatal("the job did not start")
	}
	return c, w, res.Job.ID
}

func jobState(t *testing.T, db *testkit.DB, id int64) string {
	t.Helper()
	var s string
	require.NoError(t, db.Pool.QueryRow(context.Background(), `SELECT state FROM river_job WHERE id = $1`, id).Scan(&s))
	return s
}

// TestDrainLetsRunningJobFinish (W1): SIGTERM cancels the process context;
// a job running at that moment still finishes and is marked completed.
func TestDrainLetsRunningJobFinish(t *testing.T) {
	db := testkit.NewDB(t)
	ctx, signal := context.WithCancel(context.Background())
	c, w, id := startSlow(t, db, ctx)

	signal() // what SIGTERM does to the worker's context
	drained := make(chan error, 1)
	go func() { drained <- Drain(c, 10*time.Second, quiet()) }()
	time.Sleep(300 * time.Millisecond) // the job is still working when the drain starts
	require.False(t, w.cancelled.Load(), "the signal cancelled the running job")
	close(w.release)

	require.NoError(t, <-drained)
	require.True(t, w.finished.Load())
	require.False(t, w.cancelled.Load())
	require.Equal(t, "completed", jobState(t, db, id))
}

// TestDrainCancelsAfterGrace (W1): a job still running after the grace
// period gets its context cancelled, and Drain returns.
func TestDrainCancelsAfterGrace(t *testing.T) {
	db := testkit.NewDB(t)
	c, w, id := startSlow(t, db, context.Background())
	require.NoError(t, Drain(c, 300*time.Millisecond, quiet()))
	require.True(t, w.cancelled.Load())
	require.False(t, w.finished.Load())
	require.NotEqual(t, "completed", jobState(t, db, id))
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
