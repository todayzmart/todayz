package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/lifecycle"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/testkit"
)

type longArgs struct{}

func (longArgs) Kind() string { return "test.long" }

// longWorker is the test's own 20 s job: it runs until its clock has moved
// 20 s from the start (a fake clock, so the test controls when that is).
type longWorker struct {
	river.WorkerDefaults[longArgs]
	clk       *clock.Fake
	starts    atomic.Int32
	started   chan struct{}
	cancelled atomic.Bool
}

func (w *longWorker) Work(ctx context.Context, _ *river.Job[longArgs]) error {
	if w.starts.Add(1) == 1 {
		close(w.started)
	}
	begin := w.clk.Now()
	for w.clk.Now().Sub(begin) < 20*time.Second {
		select {
		case <-ctx.Done():
			w.cancelled.Store(true)
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return nil
}

func riverState(t *testing.T, db *testkit.DB, id int64) string {
	t.Helper()
	var s string
	require.NoError(t, db.Pool.QueryRow(context.Background(), `SELECT state FROM river_job WHERE id = $1`, id).Scan(&s))
	return s
}

func probeGet(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Close = true // a new connection every call, like a fresh load-balancer probe
	resp, err := testHTTP.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestWorkerSIGTERMLetsJobFinish (SC-GS-04): a 20 s job (20 s of the test's
// fake clock) is running when the worker gets a real SIGTERM. /readyz turns
// 503 {"status":"draining"} while /healthz stays 200; a job inserted during
// the drain is not started; the running job ends completed (River's state for
// a run that succeeded); nothing is hard-cancelled (tz_job_hard_cancelled_total
// stays 0, no error log); serveWorker returns only after the drain.
func TestWorkerSIGTERMLetsJobFinish(t *testing.T) {
	db := testkit.NewDB(t)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(context.Background()) }()

	w := &longWorker{clk: clock.NewFake(time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)), started: make(chan struct{})}
	reg := queue.NewRegistry()
	queue.Add[longArgs](reg, w)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := queue.NewWorkerClient(db.Pool, reg, queue.Options{Log: quiet, MeterProvider: mp})
	require.NoError(t, err)

	var qh queue.Health
	ready := &health.Readiness{CacheFor: -1, Checks: []health.Check{
		{Name: "river", Fn: func(ctx context.Context) error { return queue.Check(ctx, db.Pool, &qh) }},
	}}
	r := chi.NewRouter()
	health.Register(r, ready, health.Options{})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + l.Addr().String()

	require.NoError(t, queue.Start(ctx, c))
	qh.Watch(c)
	var logs syncBuffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	done := make(chan error, 1)
	go func() {
		done <- serveWorker(ctx, log, lifecycle.Server{Srv: httpx.NewServer("", r), Ready: ready, Listener: l},
			c, queue.DrainGrace, queue.HardGrace)
	}()
	waitOK(t, base+"/readyz")

	first, err := c.Insert(ctx, longArgs{}, nil)
	require.NoError(t, err)
	select {
	case <-w.started:
	case <-time.After(20 * time.Second):
		t.Fatal("the job did not start")
	}

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))

	var body string
	require.Eventually(t, func() bool {
		var code int
		code, body = probeGet(t, base+"/readyz")
		return code == http.StatusServiceUnavailable
	}, 2*time.Second, 5*time.Millisecond)
	var rb map[string]string
	require.NoError(t, json.Unmarshal([]byte(body), &rb))
	require.Equal(t, "draining", rb["status"])
	code, _ := probeGet(t, base+"/healthz")
	require.Equal(t, 200, code)
	require.Eventually(t, func() bool { return strings.Contains(logs.String(), "worker draining") }, 2*time.Second, 5*time.Millisecond)

	// River stops fetching: a job inserted now is not started, even after
	// River's fetch interval has passed several times.
	second, err := c.Insert(context.WithoutCancel(ctx), longArgs{}, nil)
	require.NoError(t, err)
	time.Sleep(2 * time.Second)
	require.Equal(t, int32(1), w.starts.Load(), "a new job started during the drain")
	require.Equal(t, "running", riverState(t, db, first.Job.ID))
	code, _ = probeGet(t, base+"/readyz")
	require.Equal(t, http.StatusServiceUnavailable, code, "still draining while the job runs")
	select {
	case err := <-done:
		t.Fatalf("serveWorker returned while the job was running: %v", err)
	default:
	}

	// The job reaches its 20 s and ends; the drain completes.
	w.clk.Advance(20 * time.Second)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("the worker did not finish draining")
	}
	require.Equal(t, "completed", riverState(t, db, first.Job.ID))
	require.Equal(t, "available", riverState(t, db, second.Job.ID), "left for the next worker")
	require.False(t, w.cancelled.Load())
	require.Equal(t, int32(1), w.starts.Load())

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, md := range sm.Metrics {
			require.NotEqual(t, queue.HardCancelledMetric, md.Name, "tz_job_hard_cancelled_total stays 0")
		}
	}
	out := logs.String()
	require.NotContains(t, out, `"level":"ERROR"`)
	require.Contains(t, out, "worker drained: river stopped")
	require.Contains(t, out, `"worker health drained"`)
}
