package telemetry

import (
	"context"
	"runtime"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func init() {
	// The queue River runs today; W1-11 registers the rest.
	RegisterLabelValues("queue", "default")
}

func meterOrGlobal(mp metric.MeterProvider) metric.Meter {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	return mp.Meter(MeterName)
}

// RegisterRuntime registers Go runtime and process metrics for service:
// goroutines, heap, GC pause total and CPU seconds. The returned function
// unregisters them.
func RegisterRuntime(mp metric.MeterProvider, service string) (func() error, error) {
	m := meterOrGlobal(mp)
	svc := metric.WithAttributes(attribute.String("service", LabelValue("service", service)))
	gor, err := m.Int64ObservableGauge("tz_go_goroutines")
	if err != nil {
		return nil, err
	}
	heap, err := m.Int64ObservableGauge("tz_go_heap_alloc_bytes", metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}
	sys, err := m.Int64ObservableGauge("tz_go_memory_sys_bytes", metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}
	gc, err := m.Float64ObservableCounter("tz_go_gc_pause_seconds_total", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	gcn, err := m.Int64ObservableCounter("tz_go_gc_cycles_total")
	if err != nil {
		return nil, err
	}
	cpu, err := m.Float64ObservableCounter("tz_process_cpu_seconds_total", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	reg, err := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		o.ObserveInt64(gor, int64(runtime.NumGoroutine()), svc)
		o.ObserveInt64(heap, int64(ms.HeapAlloc), svc)
		o.ObserveInt64(sys, int64(ms.Sys), svc)
		o.ObserveFloat64(gc, float64(ms.PauseTotalNs)/float64(time.Second), svc)
		o.ObserveInt64(gcn, int64(ms.NumGC), svc)
		var ru syscall.Rusage
		if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
			o.ObserveFloat64(cpu, tv(ru.Utime)+tv(ru.Stime), svc)
		}
		return nil
	}, gor, heap, sys, gc, gcn, cpu)
	if err != nil {
		return nil, err
	}
	return reg.Unregister, nil
}

func tv(t syscall.Timeval) float64 { return float64(t.Sec) + float64(t.Usec)/1e6 }

// RegisterPool registers the pgx pool gauges (05 §3): acquired, idle, total
// and max connections, plus cumulative acquire counts and wait time. pgxpool
// only exposes cumulative wait, so a p95 wait is not available; the mean is
// rate(wait_seconds_total)/rate(acquire_total).
func RegisterPool(mp metric.MeterProvider, service string, pool *pgxpool.Pool) (func() error, error) {
	m := meterOrGlobal(mp)
	svc := metric.WithAttributes(attribute.String("service", LabelValue("service", service)))
	mk := func(name string) (metric.Int64ObservableGauge, error) { return m.Int64ObservableGauge(name) }
	acq, err := mk("tz_db_pool_acquired_connections")
	if err != nil {
		return nil, err
	}
	idle, err := mk("tz_db_pool_idle_connections")
	if err != nil {
		return nil, err
	}
	total, err := mk("tz_db_pool_total_connections")
	if err != nil {
		return nil, err
	}
	maxc, err := mk("tz_db_pool_max_connections")
	if err != nil {
		return nil, err
	}
	acqN, err := m.Int64ObservableCounter("tz_db_pool_acquire_total")
	if err != nil {
		return nil, err
	}
	empty, err := m.Int64ObservableCounter("tz_db_pool_empty_acquire_total", metric.WithDescription("Acquires that had to wait for a connection"))
	if err != nil {
		return nil, err
	}
	wait, err := m.Float64ObservableCounter("tz_db_pool_acquire_wait_seconds_total", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	reg, err := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := pool.Stat()
		o.ObserveInt64(acq, int64(s.AcquiredConns()), svc)
		o.ObserveInt64(idle, int64(s.IdleConns()), svc)
		o.ObserveInt64(total, int64(s.TotalConns()), svc)
		o.ObserveInt64(maxc, int64(s.MaxConns()), svc)
		o.ObserveInt64(acqN, s.AcquireCount(), svc)
		o.ObserveInt64(empty, s.EmptyAcquireCount(), svc)
		o.ObserveFloat64(wait, s.AcquireDuration().Seconds(), svc)
		return nil
	}, acq, idle, total, maxc, acqN, empty, wait)
	if err != nil {
		return nil, err
	}
	return reg.Unregister, nil
}

// Querier is the slice of pgx the queue stats need (a pool or a tx).
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// QueueStat is one queue's backlog at a moment.
type QueueStat struct {
	Queue string
	// Depth is the number of jobs ready to run now: state available, or
	// retryable with scheduled_at in the past.
	Depth int64
	// OldestAge is how long the oldest of them has been waiting past its
	// scheduled time; zero when the queue is empty.
	OldestAge time.Duration
}

// QueueStats reads the backlog of every queue from river_job at now. now
// comes from the injected clock, so the test clock moves it (D-80). It is the
// function the worker's CloudWatch publisher (W5-04) calls for
// OldestAvailableAge.
func QueueStats(ctx context.Context, q Querier, now time.Time) ([]QueueStat, error) {
	rows, err := q.Query(ctx, `SELECT queue, count(*), min(scheduled_at)
FROM river_job
WHERE state IN ('available', 'retryable') AND scheduled_at <= $1
GROUP BY queue`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueStat
	for rows.Next() {
		var s QueueStat
		var oldest time.Time
		if err := rows.Scan(&s.Queue, &s.Depth, &oldest); err != nil {
			return nil, err
		}
		if age := now.Sub(oldest); age > 0 {
			s.OldestAge = age
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// OldestAvailableAge is the age of the oldest ready job in any queue, zero
// when none waits. The worker publishes it to CloudWatch for the queue-stuck
// alarm (W5-04) and the dashboards read it from tz_queue_oldest_available_age_seconds.
func OldestAvailableAge(ctx context.Context, q Querier, now time.Time) (time.Duration, error) {
	stats, err := QueueStats(ctx, q, now)
	if err != nil {
		return 0, err
	}
	var oldest time.Duration
	for _, s := range stats {
		if s.OldestAge > oldest {
			oldest = s.OldestAge
		}
	}
	return oldest, nil
}

// RegisterQueue registers tz_queue_depth{queue} and
// tz_queue_oldest_available_age_seconds{queue}. Every scrape runs one small
// query (bounded by a 2 s timeout); a failed query is skipped, never fatal.
// Queues that have drained are reported as zero until the process restarts.
func RegisterQueue(mp metric.MeterProvider, q Querier, now func() time.Time) (func() error, error) {
	m := meterOrGlobal(mp)
	depth, err := m.Int64ObservableGauge("tz_queue_depth", metric.WithDescription("Jobs ready to run"))
	if err != nil {
		return nil, err
	}
	age, err := m.Float64ObservableGauge("tz_queue_oldest_available_age_seconds", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{"default": {}}
	reg, err := m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		stats, err := QueueStats(ctx, q, now())
		if err != nil {
			return nil
		}
		cur := map[string]QueueStat{}
		for _, s := range stats {
			name := LabelValue("queue", s.Queue)
			c := cur[name]
			c.Depth += s.Depth
			if s.OldestAge > c.OldestAge {
				c.OldestAge = s.OldestAge
			}
			cur[name] = c
			seen[name] = struct{}{}
		}
		for name := range seen {
			c := cur[name]
			a := metric.WithAttributes(attribute.String("queue", name))
			o.ObserveInt64(depth, c.Depth, a)
			o.ObserveFloat64(age, c.OldestAge.Seconds(), a)
		}
		return nil
	}, depth, age)
	if err != nil {
		return nil, err
	}
	return reg.Unregister, nil
}
