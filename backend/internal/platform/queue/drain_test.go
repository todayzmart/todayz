package queue

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/todayz/backend/internal/testkit"
)

type slowArgs struct{}

func (slowArgs) Kind() string { return "test.slow" }

// slowWorker blocks until release closes or, unless stubborn, its context is
// cancelled.
type slowWorker struct {
	river.WorkerDefaults[slowArgs]
	started   chan struct{}
	release   chan struct{}
	stubborn  bool // ignores cancellation
	cancelled atomic.Bool
	finished  atomic.Bool
}

func (w *slowWorker) Work(ctx context.Context, _ *river.Job[slowArgs]) error {
	close(w.started)
	done := ctx.Done()
	if w.stubborn {
		done = nil
	}
	select {
	case <-w.release:
		w.finished.Store(true)
		return nil
	case <-done:
		w.cancelled.Store(true)
		return ctx.Err()
	}
}

// metrics is a meter provider the test reads directly.
type metrics struct {
	reader *sdkmetric.ManualReader
	mp     *sdkmetric.MeterProvider
}

func newMetrics(t *testing.T) *metrics {
	t.Helper()
	r := sdkmetric.NewManualReader()
	m := &metrics{reader: r, mp: sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))}
	t.Cleanup(func() { _ = m.mp.Shutdown(context.Background()) })
	return m
}

// hardCancelled returns tz_job_hard_cancelled_total by kind.
func (m *metrics) hardCancelled(t *testing.T) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, m.reader.Collect(context.Background(), &rm))
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, md := range sm.Metrics {
			if md.Name != HardCancelledMetric {
				continue
			}
			sum, ok := md.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.True(t, sum.IsMonotonic)
			for _, dp := range sum.DataPoints {
				kind, _ := dp.Attributes.Value("kind")
				out[kind.AsString()] += dp.Value
			}
		}
	}
	return out
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func jsonLogger() (*slog.Logger, *syncBuffer) {
	var b syncBuffer
	return slog.New(slog.NewJSONHandler(&b, nil)), &b
}

func startSlow(t *testing.T, db *testkit.DB, ctx context.Context, w *slowWorker, m *metrics) (*Client, int64) {
	t.Helper()
	w.started, w.release = make(chan struct{}), make(chan struct{})
	reg := NewRegistry()
	Add[slowArgs](reg, w)
	c, err := NewWorkerClient(db.Pool, reg, Options{Log: quiet(), MeterProvider: m.mp})
	require.NoError(t, err)
	require.NoError(t, Start(ctx, c))
	res, err := c.Insert(context.WithoutCancel(ctx), slowArgs{}, nil)
	require.NoError(t, err)
	select {
	case <-w.started:
	case <-time.After(20 * time.Second):
		t.Fatal("the job did not start")
	}
	return c, res.Job.ID
}

func jobState(t *testing.T, db *testkit.DB, id int64) string {
	t.Helper()
	var s string
	require.NoError(t, db.Pool.QueryRow(context.Background(), `SELECT state FROM river_job WHERE id = $1`, id).Scan(&s))
	return s
}

// TestDrainLetsRunningJobFinish (D-98, graceful path): SIGTERM cancels the
// process context; a job running at that moment still finishes inside the
// grace and is completed. Nothing is hard-cancelled: the counter stays 0 and
// nothing is logged at error level.
func TestDrainLetsRunningJobFinish(t *testing.T) {
	db := testkit.NewDB(t)
	m := newMetrics(t)
	ctx, signal := context.WithCancel(context.Background())
	w := &slowWorker{}
	c, id := startSlow(t, db, ctx, w, m)

	signal() // what SIGTERM does to the worker's context
	log, logs := jsonLogger()
	drained := make(chan error, 1)
	go func() { drained <- Drain(c, 10*time.Second, 5*time.Second, log) }()
	time.Sleep(300 * time.Millisecond) // the job is still working when the drain starts
	require.False(t, w.cancelled.Load(), "the signal cancelled the running job")
	close(w.release)

	require.NoError(t, <-drained)
	require.True(t, w.finished.Load())
	require.False(t, w.cancelled.Load())
	require.Equal(t, "completed", jobState(t, db, id))
	require.Empty(t, m.hardCancelled(t), "tz_job_hard_cancelled_total stays 0")
	require.NotContains(t, logs.String(), `"level":"ERROR"`)
	_, tracked := trackers.Load(c)
	require.False(t, tracked, "Drain forgets the client")
}

// TestDrainHardCancelsAfterGrace (D-98, hard path): a job still running after
// the grace is hard-cancelled: logged at error level with its kind and ID,
// counted once in tz_job_hard_cancelled_total{kind}, its context cancelled,
// and Drain returns inside hardGrace. The attempt is left for River to retry.
func TestDrainHardCancelsAfterGrace(t *testing.T) {
	db := testkit.NewDB(t)
	m := newMetrics(t)
	w := &slowWorker{}
	c, id := startSlow(t, db, context.Background(), w, m)

	log, logs := jsonLogger()
	require.NoError(t, Drain(c, 300*time.Millisecond, 5*time.Second, log))
	require.True(t, w.cancelled.Load())
	require.False(t, w.finished.Load())
	// River puts a job cancelled by its own stop back for another attempt.
	require.Contains(t, []string{"available", "retryable"}, jobState(t, db, id), "the cancelled attempt is retried")

	require.Equal(t, map[string]int64{"test.slow": 1}, m.hardCancelled(t))
	out := logs.String()
	require.Contains(t, out, `"level":"ERROR","msg":"queue: job hard-cancelled at shutdown","kind":"test.slow"`)
	require.Contains(t, out, `"job_id":`)
	require.Contains(t, out, "hard-cancelling them")
}

// TestDrainGivesUpAfterHardGrace: a job that ignores cancellation does not
// hold the process past hardGrace; Drain returns an error, logged at error
// level.
func TestDrainGivesUpAfterHardGrace(t *testing.T) {
	db := testkit.NewDB(t)
	m := newMetrics(t)
	w := &slowWorker{stubborn: true}
	c, _ := startSlow(t, db, context.Background(), w, m)
	defer close(w.release) // let River's goroutine end after the test

	log, logs := jsonLogger()
	start := time.Now()
	err := Drain(c, 200*time.Millisecond, 300*time.Millisecond, log)
	require.ErrorContains(t, err, "stop and cancel")
	require.Less(t, time.Since(start), 5*time.Second)
	require.Equal(t, map[string]int64{"test.slow": 1}, m.hardCancelled(t))
	require.Contains(t, logs.String(), "jobs did not return after the hard cancel")
}
