package jobs

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The jobs metrics (05 §8). Their only labels are kind, class and status
// (SC-JB-13); the values are bounded by the registry (Registry.LabelValues).
const (
	MetricRuns     = telemetry.MetricPrefix + "job_runs_total"
	MetricDuration = telemetry.MetricPrefix + "job_duration_seconds"
	MetricLateness = telemetry.MetricPrefix + "job_lateness_seconds"
	MetricMissed   = telemetry.MetricPrefix + "job_missed"
)

// The missed gauge's cache: every worker reports tz_job_missed from the
// database, read at most once per missedCacheTTL (and again at once after
// this process's watchdog run or a scheduled success), so all workers agree
// within that time whichever of them flagged the period or ran the success.
const (
	missedCacheTTL     = 30 * time.Second
	missedQueryTimeout = 5 * time.Second
)

// missedSQL is, per scheduled kind, whether a missed row is newer than the
// kind's latest success (D-95), and whether the kind is paused.
const missedSQL = `SELECT k.kind,
	COALESCE((SELECT max(created_at) FROM job_runs WHERE kind = k.kind AND status = 'missed'), '-infinity')
	> COALESCE((SELECT max(COALESCE(finished_at, created_at)) FROM job_runs WHERE kind = k.kind AND status = 'succeeded'), '-infinity'),
	COALESCE(c.paused, false)
	FROM unnest($1::text[]) AS k(kind) LEFT JOIN job_controls c ON c.kind = k.kind`

// monitor holds the jobs metrics and the missed gauge. The Runner creates
// it (only the worker emits job metrics) and attaches it to the registry,
// where the jobs_watchdog handler finds it.
type monitor struct {
	runs     metric.Int64Counter
	duration metric.Float64Histogram
	lateness metric.Float64Histogram
	reg      metric.Registration

	pool     *pgxpool.Pool
	now      func() time.Time
	kinds    []string        // scheduled kinds, for the gauge's zero series
	pausable map[string]bool // scheduled kinds a pause stops (not timers)

	mu      sync.Mutex
	missed  map[string]bool // kind → missed, as last read from the database
	fetched time.Time       // when missed was read; zero = never
	stale   bool            // set by invalidate: read again on next use
}

func newMonitor(mp metric.MeterProvider, r *Registry, pool *pgxpool.Pool, clk clock.Clock) (*monitor, error) {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	m := &monitor{missed: map[string]bool{}, pool: pool, pausable: map[string]bool{}, now: clk.Now}
	for _, k := range r.Kinds() {
		if k.Scheduled() {
			m.kinds = append(m.kinds, k.Name)
			m.pausable[k.Name] = k.Class != ClassTimer
		}
	}
	meter := mp.Meter("todayz/jobs")
	var err error
	if m.runs, err = meter.Int64Counter(MetricRuns,
		metric.WithDescription("Job attempts by outcome (D-95)")); err != nil {
		return nil, err
	}
	// Exponential (native) buckets come from the meter provider's view
	// (05 §2: histograms are OTel exponential histograms); the instrument
	// only names the unit.
	if m.duration, err = meter.Float64Histogram(MetricDuration, metric.WithUnit("s"),
		metric.WithDescription("Job attempt duration")); err != nil {
		return nil, err
	}
	if m.lateness, err = meter.Float64Histogram(MetricLateness, metric.WithUnit("s"),
		metric.WithDescription("Actual start minus scheduled time")); err != nil {
		return nil, err
	}
	gauge, err := meter.Int64ObservableGauge(MetricMissed,
		metric.WithDescription("1 while jobs_watchdog sees the scheduled kind as missed (D-95)"))
	if err != nil {
		return nil, err
	}
	m.reg, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		for kind, v := range m.current(ctx) {
			n := int64(0)
			if v {
				n = 1
			}
			o.ObserveInt64(gauge, n, metric.WithAttributes(attribute.String("kind", kind)))
		}
		return nil
	}, gauge)
	if err != nil {
		return nil, err
	}
	m.zero(r)
	return m, nil
}

// zero adds 0 to the runs counter for every kind and terminal status, so
// every series exists from the start and increase()-based alerts see the
// first failure.
func (m *monitor) zero(r *Registry) {
	ctx := context.Background()
	for _, k := range r.Kinds() {
		for _, s := range []Status{StatusSucceeded, StatusFailed, StatusDiscarded} {
			m.runs.Add(ctx, 0, metric.WithAttributes(runAttrs(k.Name, k.Class, s)...))
		}
	}
}

func runAttrs(kind string, class Class, s Status) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("kind", kind),
		attribute.String("class", string(class)),
		attribute.String("status", string(s)),
	}
}

// current is the missed gauge per scheduled kind, from the database through
// the cache. A failed read keeps the last values (and is tried again on the
// next collection); a paused kind other than a timer reads 0.
func (m *monitor) current(ctx context.Context) map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	due := m.stale || m.fetched.IsZero() || now.Sub(m.fetched) >= missedCacheTTL || now.Before(m.fetched)
	if due && m.pool != nil && len(m.kinds) > 0 {
		qctx, cancel := context.WithTimeout(ctx, missedQueryTimeout)
		got, err := m.query(qctx)
		cancel()
		if err == nil {
			m.missed, m.fetched, m.stale = got, now, false
		}
	}
	out := make(map[string]bool, len(m.kinds))
	for _, k := range m.kinds {
		out[k] = m.missed[k]
	}
	return out
}

func (m *monitor) query(ctx context.Context) (map[string]bool, error) {
	rows, err := m.pool.Query(ctx, missedSQL, m.kinds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool, len(m.kinds))
	for rows.Next() {
		var (
			kind           string
			missed, paused bool
		)
		if err := rows.Scan(&kind, &missed, &paused); err != nil {
			return nil, err
		}
		stopped := paused && m.pausable[kind] // a pause stops the kind: not missed
		out[kind] = missed && !stopped
	}
	return out, rows.Err()
}

// invalidate makes the next read go to the database (after this process's
// watchdog run commits, or a scheduled kind succeeds here).
func (m *monitor) invalidate() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.stale = true
	m.mu.Unlock()
}

// missedKinds lists the scheduled kinds tz_job_missed reads 1 for, sorted.
func (m *monitor) missedKinds(ctx context.Context) []string {
	if m == nil {
		return nil
	}
	var out []string
	for k, v := range m.current(ctx) {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// record emits one finished attempt.
func (m *monitor) record(ctx context.Context, k Kind, s Status, durS, lateS float64, lateKnown bool) {
	if m == nil {
		return
	}
	m.runs.Add(ctx, 1, metric.WithAttributes(runAttrs(k.Name, k.Class, s)...))
	if s == StatusSkipped {
		return
	}
	kindAttr := metric.WithAttributes(attribute.String("kind", k.Name))
	m.duration.Record(ctx, durS, kindAttr)
	if lateKnown {
		m.lateness.Record(ctx, lateS, kindAttr)
	}
}

// recordMissed counts one missed period (status missed, written by the
// watchdog).
func (m *monitor) recordMissed(ctx context.Context, k Kind) {
	if m == nil {
		return
	}
	m.runs.Add(ctx, 1, metric.WithAttributes(runAttrs(k.Name, k.Class, StatusMissed)...))
}

func (m *monitor) close() {
	if m != nil && m.reg != nil {
		_ = m.reg.Unregister()
	}
}
