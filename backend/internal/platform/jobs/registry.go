package jobs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/todayz/backend/internal/platform/telemetry"
)

// Registry holds the declared kinds. The worker builds one, every domain
// stub adds its kinds with Define, and the River client is built from it.
// The API builds one too (with the same Register call) to list kinds on
// A-21; declaring a kind has no side effects.
type Registry struct {
	mu    sync.Mutex
	kinds map[string]*entry
	order []string
	mon   *monitor // set by NewRunner in the worker; nil elsewhere
}

type entry struct {
	kind  Kind
	sched *Schedule // nil for event kinds
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{kinds: map[string]*entry{}} }

// Define declares kind k in r. It panics if k is invalid (Validate) or its
// name is taken: a bad declaration is a programming error caught at start-up
// and by the registry tests, never at run time.
func Define(r *Registry, k Kind) {
	if err := r.define(k); err != nil {
		panic(err)
	}
}

func (r *Registry) define(k Kind) error {
	if err := Validate(k); err != nil {
		return err
	}
	var sched *Schedule
	if k.Scheduled() {
		sched, _ = ParseSchedule(k.Schedule) // Validate parsed it
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.kinds[k.Name]; dup {
		return fmt.Errorf("jobs: kind %q is defined twice", k.Name)
	}
	r.kinds[k.Name] = &entry{kind: k, sched: sched}
	r.order = append(r.order, k.Name)
	// The kind and its queue become allowed values of the kind and queue
	// labels (05 §2.1), so W1-07's metrics (tz_queue_* and any other series
	// labelled by kind or queue through telemetry.LabelValue) show them
	// instead of "other". Values are only ever added: a registry built in a
	// test leaves its names behind, which is harmless.
	q := k.Queue
	if q == "" {
		q = DefaultQueue
	}
	telemetry.RegisterLabelValues("kind", k.Name)
	telemetry.RegisterLabelValues("queue", q)
	return nil
}

var (
	reKindName = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	reOwner    = regexp.MustCompile(`^W[0-9]-[0-9]{2}[a-z]?$`)
	reQueue    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)
	reRunbook  = regexp.MustCompile(`^docs/runbooks/[a-z0-9][a-z0-9-]*\.md$`)
)

// Limits on a declaration.
const (
	maxNameLen        = 63
	maxDescriptionLen = 200
	maxAttemptsCap    = 25
	// maxConcurrency caps Kind.Concurrency. Each run of a capped kind pins one
	// pool connection for its whole run (acquireSlot holds the session
	// advisory lock on it), on top of the connections the handler uses. The
	// worker pool is small (pgxpool's default max(4, NumCPU) unless
	// DATABASE_URL sets pool_max_conns; cmd/todayz/app.go), so a large cap
	// would let one kind pin the pool and starve its own handlers, River and
	// the scheduler. A kind that needs more parallelism leaves Concurrency at
	// 0 and is bounded by its queue's MaxWorkers instead.
	maxConcurrency = 10
)

// Validate checks a declaration against the registry rules (SC-JB-01,
// SC-JB-15) and returns every problem found, joined.
func Validate(k Kind) error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("jobs: kind %q: "+format, append([]any{k.Name}, a...)...))
	}
	if !reKindName.MatchString(k.Name) || len(k.Name) > maxNameLen {
		bad("Name must be snake_case, at most %d characters", maxNameLen)
	}
	if !reOwner.MatchString(k.Owner) {
		bad("Owner must be the owning work package id, like W1-11")
	}
	if k.Description == "" || len(k.Description) > maxDescriptionLen {
		bad("Description is required, at most %d characters", maxDescriptionLen)
	}
	if !k.Class.Valid() {
		bad("Class %q is not one of %v", k.Class, Classes())
	}
	if k.Class.NeedsRunbook() && k.Runbook == "" {
		bad("a %s kind needs a Runbook", k.Class)
	}
	if k.Runbook != "" && !reRunbook.MatchString(k.Runbook) {
		bad("Runbook must be a path like docs/runbooks/<name>.md")
	}
	if k.Scheduled() {
		if _, err := ParseSchedule(k.Schedule); err != nil {
			bad("%v", err)
		}
		if k.UniqueBy == PeriodNone || k.UniqueBy > PerMonth {
			bad("a scheduled kind needs UniqueBy (PerTick, PerHour, PerDay or PerMonth)")
		}
	} else if k.UniqueBy != PeriodNone {
		bad("an event-triggered kind has no UniqueBy (pass EnqueueOpts.UniqueKey instead)")
	}
	switch {
	case k.Timeout <= 0:
		bad("Timeout is required")
	case k.Timeout > MaxTimeout && !k.Batched:
		bad("Timeout %s is above %s; only a Batched kind may run longer (D-98)", k.Timeout, MaxTimeout)
	case k.Timeout%time.Second != 0:
		bad("Timeout must be whole seconds")
	}
	if k.MaxAttempts < 1 || k.MaxAttempts > maxAttemptsCap {
		bad("MaxAttempts must be 1–%d", maxAttemptsCap)
	}
	if k.Queue != "" && !reQueue.MatchString(k.Queue) {
		bad("Queue must be empty (the default queue) or a snake_case name")
	}
	if k.Concurrency < 0 || k.Concurrency > maxConcurrency {
		bad("Concurrency must be 0 (no cap) to %d", maxConcurrency)
	}
	if k.Handler == nil {
		bad("Handler is required")
	}
	return errors.Join(errs...)
}

// Kinds returns the declared kinds, sorted by name.
func (r *Registry) Kinds() []Kind {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Kind, 0, len(r.kinds))
	for _, e := range r.kinds {
		out = append(out, e.kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns the declared kind names, sorted.
func (r *Registry) Names() []string {
	ks := r.Kinds()
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = k.Name
	}
	return out
}

// Lookup returns the kind named name.
func (r *Registry) Lookup(name string) (Kind, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.kinds[name]
	if !ok {
		return Kind{}, false
	}
	return e.kind, true
}

// ScheduleOf returns the parsed schedule of a scheduled kind.
func (r *Registry) ScheduleOf(name string) (*Schedule, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.kinds[name]
	if !ok || e.sched == nil {
		return nil, false
	}
	return e.sched, true
}

func (r *Registry) entry(name string) (*entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.kinds[name]
	return e, ok
}

func (r *Registry) setMonitor(m *monitor) {
	r.mu.Lock()
	r.mon = m
	r.mu.Unlock()
}

func (r *Registry) monitor() *monitor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mon
}

// MissedKinds lists the scheduled kinds tz_job_missed reads 1 for: read
// from the database (shared by every worker) through the monitor's cache;
// empty outside the worker.
func (r *Registry) MissedKinds(ctx context.Context) []string { return r.monitor().missedKinds(ctx) }

// DefaultQueue is the River queue a kind with no Queue uses.
const DefaultQueue = "default"

// Labels are the bounded label values the jobs metrics use (05 §2.1).
type Labels struct {
	Kinds   []string // registered kind names
	Classes []string // the seven classes
	Queues  []string // River queue names in use, including "default"
}

// LabelValues returns the `kind`, `class` and `queue` label values for the
// metrics allow-list (W1-07): every value a tz_job_* series or River metric
// can carry. Build the registry with the same Register call the worker uses.
func (r *Registry) LabelValues() Labels {
	l := Labels{}
	queues := map[string]bool{DefaultQueue: true}
	for _, k := range r.Kinds() {
		l.Kinds = append(l.Kinds, k.Name)
		if k.Queue != "" {
			queues[k.Queue] = true
		}
	}
	for _, c := range Classes() {
		l.Classes = append(l.Classes, string(c))
	}
	for q := range queues {
		l.Queues = append(l.Queues, q)
	}
	sort.Strings(l.Queues)
	return l
}
