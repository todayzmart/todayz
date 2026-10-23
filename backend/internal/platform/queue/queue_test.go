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

// heartbeatArgs and heartbeatWorker are a test kind that counts its runs.
type heartbeatArgs struct{}

func (heartbeatArgs) Kind() string { return "test_heartbeat" }

type heartbeatWorker struct {
	river.WorkerDefaults[heartbeatArgs]
	Runs atomic.Int64
}

func (w *heartbeatWorker) Work(context.Context, *river.Job[heartbeatArgs]) error {
	w.Runs.Add(1)
	return nil
}

// namedArgs goes to a named queue.
type namedArgs struct{}

func (namedArgs) Kind() string { return "test_named" }

type namedWorker struct {
	river.WorkerDefaults[namedArgs]
	Runs atomic.Int64
}

func (w *namedWorker) Work(context.Context, *river.Job[namedArgs]) error {
	w.Runs.Add(1)
	return nil
}

// TestWorkerRunsJobsAsAppRole: the worker, connected as todayz_app, elects a
// leader and works jobs from the default queue and from a named queue added
// with AddQueue.
func TestWorkerRunsJobsAsAppRole(t *testing.T) {
	db := testkit.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var role string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT current_user`).Scan(&role))
	require.Equal(t, testkit.AppRole, role)

	reg := NewRegistry()
	hb, nw := &heartbeatWorker{}, &namedWorker{}
	Add[heartbeatArgs](reg, hb)
	AddArgs(reg, namedArgs{}, river.Worker[namedArgs](nw))
	reg.AddQueue("bulk", 0)
	require.ElementsMatch(t, []string{"test_heartbeat", "test_named"}, reg.Kinds())

	c, err := NewWorkerClient(db.Pool, reg, Options{Log: quiet()})
	require.NoError(t, err)
	done, cancelSub := c.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed)
	defer cancelSub()
	var h Health
	require.ErrorIs(t, Check(ctx, db.Pool, &h), ErrNotRunning)
	require.NoError(t, c.Start(ctx))
	h.Watch(c)
	require.NoError(t, Check(ctx, db.Pool, &h))
	_, err = c.Insert(ctx, heartbeatArgs{}, nil)
	require.NoError(t, err)
	_, err = c.Insert(ctx, namedArgs{}, &river.InsertOpts{Queue: "bulk"})
	require.NoError(t, err)

	completed := map[string]bool{}
	for len(completed) < 2 {
		select {
		case ev := <-done:
			require.Equal(t, river.EventKindJobCompleted, ev.Kind, "job %s failed: %v", ev.Job.Kind, ev.Job.Errors)
			completed[ev.Job.Kind] = true
		case <-ctx.Done():
			t.Fatalf("jobs did not run; completed: %v", completed)
		}
	}
	require.Equal(t, int64(1), hb.Runs.Load())
	require.Equal(t, int64(1), nw.Runs.Load(), "the named queue is worked")

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
	hb := &heartbeatWorker{}
	Add[heartbeatArgs](reg, hb)
	c, err := NewWorkerClient(db.Pool, reg, Options{Log: quiet(), Time: off})
	require.NoError(t, err)
	done, cancelSub := c.Subscribe(river.EventKindJobCompleted)
	defer cancelSub()

	at := off.Now().Add(30 * time.Minute)
	res, err := c.Insert(ctx, heartbeatArgs{}, &river.InsertOpts{ScheduledAt: at})
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
		require.Equal(t, "test_heartbeat", ev.Job.Kind)
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
	_, err = c.InsertTx(ctx, tx, heartbeatArgs{}, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	var n int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job`).Scan(&n))
	require.Zero(t, n, "a rolled-back insert leaves no job")
	require.NoError(t, Check(ctx, db.Pool, nil))
}
