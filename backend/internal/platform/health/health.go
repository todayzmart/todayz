// Package health serves GET /healthz, /readyz and /version (02 §2.4, 05).
//
//   - /healthz: the process is up. Always 200; never touches a dependency.
//   - /readyz: the process can serve. Runs every check concurrently with a
//     short timeout; 200 when all pass, 503 otherwise. The body names each
//     check as "ok" or "fail" and never carries error text (which may hold a
//     host name); the failure is logged instead. A result is reused for
//     CacheFor (2 s), and only one run is in flight at a time, so a flood of
//     /readyz calls cannot become a flood of database pings. Once the
//     process starts draining (SIGTERM, D-98) it answers 503
//     {"status":"draining"} at once, without running a check, until it
//     exits; /healthz stays 200.
//   - /version: buildinfo.Get() (no Go version). In staging and prod it is
//     served only on the worker's internal health port (WORKER_ADDR), never
//     on the public API (Options.Version).
//
// The api and the worker both serve /healthz and /readyz.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/todayz/backend/internal/platform/buildinfo"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Check is one readiness probe.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

// DefaultTimeout bounds one /readyz run; DefaultCacheFor is how long a
// result is reused.
const (
	DefaultTimeout  = 2 * time.Second
	DefaultCacheFor = 2 * time.Second
)

// Readiness runs checks.
type Readiness struct {
	Checks  []Check
	Timeout time.Duration
	Log     *slog.Logger
	// CacheFor reuses a result this long (DefaultCacheFor when zero; a
	// negative value turns the cache off).
	CacheFor time.Duration
	// Clock times the cache (clock.Real when nil).
	Clock clock.Clock

	mu   sync.Mutex // one run at a time; later callers get its result
	last Report
	at   time.Time
	have bool

	draining atomic.Bool
}

// StatusDraining is the /readyz status while the process drains.
const StatusDraining = "draining"

// SetDraining turns /readyz to 503 {"status":"draining"} for the rest of the
// process's life (D-98, 02 §2.8 step 1). It is the first thing SIGTERM does,
// so the load balancer stops sending traffic while in-flight work finishes.
// There is no way back: a draining process only exits.
func (rd *Readiness) SetDraining() { rd.draining.Store(true) }

// Draining reports whether SetDraining was called.
func (rd *Readiness) Draining() bool { return rd.draining.Load() }

// Cached returns a result no older than CacheFor, running the checks when
// there is none. Concurrent callers wait for the one run in flight.
func (rd *Readiness) Cached(ctx context.Context) Report {
	ttl := rd.CacheFor
	if ttl == 0 {
		ttl = DefaultCacheFor
	}
	var clk clock.Clock = clock.Real{}
	if rd.Clock != nil {
		clk = rd.Clock
	}
	rd.mu.Lock()
	defer rd.mu.Unlock()
	if rd.have && ttl > 0 && clk.Now().Sub(rd.at) < ttl {
		return rd.last
	}
	rep := rd.Run(ctx)
	rd.last, rd.at, rd.have = rep, clk.Now(), true
	return rep
}

// Report is the /readyz body.
type Report struct {
	Status string            `json:"status"` // "ok" | "unavailable"
	Checks map[string]string `json:"checks"` // name → "ok" | "fail"
}

// Run executes every check and returns the report.
func (rd *Readiness) Run(ctx context.Context) Report {
	timeout := rd.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	log := rd.Log
	if log == nil {
		log = telemetry.Default()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rep := Report{Status: "ok", Checks: make(map[string]string, len(rd.Checks))}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range rd.Checks {
		wg.Go(func() {
			err := c.Fn(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				rep.Checks[c.Name] = "fail"
				rep.Status = "unavailable"
				log.WarnContext(ctx, "readyz: check failed", slog.String("check", c.Name), slog.String("err", err.Error()))
				return
			}
			rep.Checks[c.Name] = "ok"
		})
	}
	wg.Wait()
	return rep
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ServeHTTP implements /readyz.
func (rd *Readiness) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rd.Draining() { // before the cache: a cached "ok" must not outlive the signal
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": StatusDraining})
		return
	}
	rep := rd.Cached(r.Context())
	status := http.StatusOK
	if rep.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, rep)
}

// Healthz answers 200 {"status":"ok"}.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Version answers the build info.
func Version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildinfo.Get())
}

// Options choose what Register mounts.
type Options struct {
	// Version mounts /version. The API sets it only in local and test; the
	// worker's internal health port always does.
	Version bool
}

// Register mounts /healthz and /readyz, and /version when o.Version.
func Register(r chi.Router, rd *Readiness, o Options) {
	r.Get("/healthz", Healthz)
	r.Get("/readyz", rd.ServeHTTP)
	if o.Version {
		r.Get("/version", Version)
	}
}

// DBCheck pings the pool.
func DBCheck(pool *pgxpool.Pool) Check {
	return Check{Name: "db", Fn: pool.Ping}
}

// AtHeader reports whether the schema is at the binary's newest migration.
type AtHeader interface {
	AtHead(ctx context.Context) (bool, error)
}

// ErrBehind is returned by MigrationsCheck when migrations are pending.
var ErrBehind = errorString("migrations pending")

type errorString string

func (e errorString) Error() string { return string(e) }

// MigrationsCheck fails while migrations are pending.
func MigrationsCheck(m AtHeader) Check {
	return Check{Name: "migrations", Fn: func(ctx context.Context) error {
		ok, err := m.AtHead(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return ErrBehind
		}
		return nil
	}}
}
