package jobs

import (
	"context"
	"sort"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

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

// monitor holds the jobs metrics and the watchdog's missed state. The
// Runner creates it (only the worker emits job metrics) and attaches it to
// the registry, where the jobs_watchdog handler finds it.
type monitor struct {
	runs     metric.Int64Counter
	duration metric.Float64Histogram
	lateness metric.Float64Histogram
	reg      metric.Registration

	mu     sync.Mutex
	missed map[string]bool // kind → seen as missed by the watchdog
	kinds  []string        // scheduled kinds, for the gauge's zero series
}

func newMonitor(mp metric.MeterProvider, r *Registry) (*monitor, error) {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	m := &monitor{missed: map[string]bool{}}
	for _, k := range r.Kinds() {
		if k.Scheduled() {
			m.kinds = append(m.kinds, k.Name)
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
	m.reg, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		for kind, v := range m.snapshot() {
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

// snapshot is the missed gauge per scheduled kind (0 or 1).
func (m *monitor) snapshot() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]bool, len(m.kinds))
	for _, k := range m.kinds {
		out[k] = m.missed[k]
	}
	return out
}

func (m *monitor) setMissed(kind string, v bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.missed[kind] = v
	m.mu.Unlock()
}

// Missed lists the kinds the watchdog currently sees as missed, sorted
// (tests and the A-21 list read it through the registry).
func (m *monitor) missedKinds() []string {
	if m == nil {
		return nil
	}
	var out []string
	for k, v := range m.snapshot() {
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
