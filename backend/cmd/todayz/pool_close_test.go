package main

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/lifecycle"
	"github.com/todayz/backend/internal/testkit"
)

// TestStuckPoolStillFlushesTelemetry (D-98): after a failed drain a request or
// job can still hold a pool connection, and pgxpool.Close waits for every
// connection. The pool close gives up after lifecycle.PoolClose (logged at
// error level) so the telemetry flush, which carries the hard-cancel metric
// and the error logs of exactly that failure, still runs. The defers are in
// runAPI's and runWorker's order: a.close first, a.closePool last.
func TestStuckPoolStillFlushesTelemetry(t *testing.T) {
	db := testkit.NewDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db.URL) // its own pool: the test leaves it closing
	require.NoError(t, err)
	conn, err := pool.Acquire(ctx) // a handler or job that never returns its connection
	require.NoError(t, err)

	var logs syncBuffer
	var flushed atomic.Bool
	a := &app{
		log:      slog.New(slog.NewJSONHandler(&logs, nil)),
		shutdown: func(context.Context) error { flushed.Store(true); return nil },
	}
	start := time.Now()
	func() {
		defer a.close()
		defer a.closePool(ctx, pool)
	}()
	took := time.Since(start)

	require.True(t, flushed.Load(), "telemetry flushed despite the stuck pool")
	require.GreaterOrEqual(t, took, lifecycle.PoolClose)
	require.Less(t, took, lifecycle.PoolClose+2*time.Second)
	out := logs.String()
	require.Contains(t, out, `"level":"ERROR","msg":"shutdown: db pool did not close in time; moving on","limit":"5s"`)
	require.Contains(t, out, "shutdown: telemetry flushed")

	// Returning the connection lets the abandoned Close finish, so the test
	// database can be dropped.
	conn.Release()
	require.Eventually(t, func() bool { return pool.Stat().TotalConns() == 0 }, 5*time.Second, 20*time.Millisecond)
}
