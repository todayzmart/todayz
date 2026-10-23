// Package lifecycle runs the SIGTERM drain of the api and the worker (02 §2.8,
// D-98): no deploy, scale-in or restart cuts off an API request or a running
// job.
//
// On SIGTERM, Server.Run:
//
//  1. turns /readyz to 503 {"status":"draining"} at once (/healthz stays 200);
//  2. keeps serving for PreStop, so the load balancer's health checks and
//     deregistration stop new traffic (api: SHUTDOWN_PRESTOP, 15 s deployed,
//     1 s locally; the worker has no load balancer and uses 0);
//  3. runs Before (the worker's queue.Drain, while /readyz keeps answering
//     503);
//  4. calls srv.Shutdown with Drain (api: SHUTDOWN_DRAIN, 75 s), which closes
//     the listener and waits for in-flight requests. Request contexts come
//     from httpx.NewServer's BaseContext, never from the signal, so they stay
//     live. A request still running at the deadline is cut (srv.Close) and
//     logged at error level.
//
// If the server fails before the signal or during PreStop, it still gets
// srv.Shutdown with Drain, so requests already running are not cut.
//
// The caller then closes the rest in order: in-process loops (LISTEN/NOTIFY,
// caches), the DB pool (bounded by PoolClose, see StepWithin), and the
// telemetry flush (TelemetryFlush).
//
// The deploy timings W5 sets (ALB deregistration delay, container stop
// timeouts) are named here so a test holds them against the drain.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// API drain defaults (02 §2.8, 09 §3.7). They are deploy settings from the
// environment (SHUTDOWN_PRESTOP, SHUTDOWN_DRAIN), not platform_settings keys.
const (
	PreStopDeployed = 15 * time.Second
	PreStopLocal    = 1 * time.Second
	Drain           = 75 * time.Second
	// TelemetryFlush bounds the last export of traces and metrics.
	TelemetryFlush = 5 * time.Second
	// HealthShutdown bounds the worker's health server shutdown, after the
	// queue drain: its requests are short.
	HealthShutdown = 5 * time.Second
	// PoolClose bounds the DB pool close. pgxpool.Close waits for every
	// connection to come back, which a request cut at the drain deadline or a
	// job that ignored its hard cancel may never do (StepWithin).
	PoolClose = 5 * time.Second
	// ShutdownMargin is what the stop-timeout checks leave for the rest of the
	// exit (loops, the migrator, scheduling).
	ShutdownMargin = 5 * time.Second
)

// APIExit is what the api's SIGTERM path may take after the drain: the
// bounded pool close, the telemetry flush and the margin.
const APIExit = PoolClose + TelemetryFlush + ShutdownMargin

// WorkerExit is the same for the worker, whose health server shuts down after
// the queue drain.
const WorkerExit = HealthShutdown + PoolClose + TelemetryFlush + ShutdownMargin

// Deploy timings (D-98; set by W5 in the Beanstalk and Docker config). The
// process must finish its drain inside them, or it is killed mid-drain.
const (
	APIStopTimeout    = 120 * time.Second
	WorkerStopTimeout = 180 * time.Second
)

// Timings is the API's resolved SIGTERM timing.
type Timings struct {
	PreStop time.Duration
	Drain   time.Duration
}

// APITimings applies the defaults to the configured values (zero = unset) and
// checks them: Drain must exceed httpx.WriteTimeout, so a response that is
// still within its write budget is never cut by the drain, and PreStop + Drain
// + APIExit (pool close, telemetry flush, margin) must stay under
// APIStopTimeout, so the container runtime never kills the process before the
// flush. The api refuses to start otherwise.
func APITimings(deployed bool, preStop, drain time.Duration) (Timings, error) {
	t := Timings{PreStop: preStop, Drain: drain}
	if t.PreStop <= 0 {
		t.PreStop = PreStopLocal
		if deployed {
			t.PreStop = PreStopDeployed
		}
	}
	if t.Drain <= 0 {
		t.Drain = Drain
	}
	if t.Drain <= httpx.WriteTimeout {
		return t, fmt.Errorf("lifecycle: SHUTDOWN_DRAIN (%s) must exceed the %s WriteTimeout", t.Drain, httpx.WriteTimeout)
	}
	if t.PreStop+t.Drain+APIExit >= APIStopTimeout {
		return t, fmt.Errorf("lifecycle: SHUTDOWN_PRESTOP + SHUTDOWN_DRAIN (%s) plus %s for the pool close, telemetry flush and margin must stay under the %s container stop timeout",
			t.PreStop+t.Drain, APIExit, APIStopTimeout)
	}
	return t, nil
}

// Drainer is told when the process starts draining (health.Readiness).
type Drainer interface{ SetDraining() }

// Server serves an http.Server until its context ends, then drains it.
type Server struct {
	Name  string // for logs: "api", "worker health"
	Srv   *http.Server
	Ready Drainer
	// Listener, when set, is served instead of listening on Srv.Addr (tests).
	Listener net.Listener
	// PreStop is how long the server keeps serving after Ready turns 503.
	PreStop time.Duration
	// Drain is srv.Shutdown's budget for in-flight requests.
	Drain time.Duration
	// Before runs after PreStop and before srv.Shutdown (the worker's queue
	// drain); its error is returned after the shutdown.
	Before func() error
	Log    *slog.Logger
}

// ErrDrainDeadline is returned when requests were still running when the
// drain budget ran out (they were cut).
var ErrDrainDeadline = errors.New("lifecycle: requests still running at the drain deadline")

// Run serves until ctx is done (the signal context), then drains as the
// package comment says. It returns after the server has stopped. A serve
// error before the signal is returned, after a shutdown with Drain so that
// requests already running are not cut.
func (s *Server) Run(ctx context.Context) error {
	log := s.Log
	if log == nil {
		log = telemetry.Default()
	}
	bg := context.WithoutCancel(ctx) // logs after the signal keep the context's values
	errc := make(chan error, 1)
	go func() {
		addr := s.Srv.Addr
		if s.Listener != nil {
			addr = s.Listener.Addr().String()
		}
		log.InfoContext(bg, s.Name+" listening", slog.String("addr", addr))
		if s.Listener != nil {
			errc <- s.Srv.Serve(s.Listener)
			return
		}
		errc <- s.Srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		// The server failed before any signal: the process is going down, so
		// /readyz says so and requests already running get the drain budget.
		s.setDraining()
		log.ErrorContext(bg, s.Name+" failed before any signal", slog.String("err", err.Error()))
		return errors.Join(err, s.shutdown(bg, log))
	case <-ctx.Done():
	}

	// 1. /readyz turns 503 at once.
	s.setDraining()
	log.InfoContext(bg, s.Name+" draining: /readyz is 503",
		slog.String("prestop", s.PreStop.String()), slog.String("drain", s.Drain.String()))

	// 2. Keep serving while the load balancer takes the instance out.
	if s.PreStop > 0 {
		t := time.NewTimer(s.PreStop)
		select {
		case <-t.C:
		case err := <-errc: // the server died during the pre-stop
			t.Stop()
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			if s.Before != nil {
				err = errors.Join(err, s.Before())
			}
			return errors.Join(err, s.shutdown(bg, log))
		}
	}

	// 3. The worker's queue drain, with /readyz still answering 503.
	var beforeErr error
	if s.Before != nil {
		beforeErr = s.Before()
	}

	// 4. Stop accepting, wait for in-flight requests.
	err := s.shutdown(bg, log)
	if serr := <-errc; serr != nil && !errors.Is(serr, http.ErrServerClosed) {
		err = errors.Join(err, serr)
	}
	if err == nil {
		log.InfoContext(bg, s.Name+" drained")
	}
	return errors.Join(beforeErr, err)
}

func (s *Server) setDraining() {
	if s.Ready != nil {
		s.Ready.SetDraining()
	}
}

// shutdown stops accepting and waits up to Drain for in-flight requests. A
// request still running at the deadline is cut (srv.Close) and logged at
// error level, and ErrDrainDeadline is returned.
func (s *Server) shutdown(bg context.Context, log *slog.Logger) error {
	log.InfoContext(bg, s.Name+" shutting down: waiting for in-flight requests", slog.String("drain", s.Drain.String()))
	sctx, cancel := context.WithTimeout(context.Background(), s.Drain)
	defer cancel()
	if err := s.Srv.Shutdown(sctx); err != nil {
		log.ErrorContext(bg, s.Name+": requests still running at the drain deadline; closing them",
			slog.String("drain", s.Drain.String()), slog.String("err", err.Error()))
		_ = s.Srv.Close()
		return fmt.Errorf("%w: %w", ErrDrainDeadline, err)
	}
	return nil
}

// Step runs one close step of the shutdown sequence and logs it, so the order
// is visible in the logs (loops, then the DB pool, then telemetry).
func Step(ctx context.Context, log *slog.Logger, name string, fn func()) {
	if log == nil {
		log = telemetry.Default()
	}
	fn()
	log.InfoContext(context.WithoutCancel(ctx), "shutdown: "+name+" closed")
}

// StepWithin runs one close step like Step, but waits at most limit for it
// and reports whether it finished. pgxpool.Close blocks until every
// connection is back; after a drain that cut a request, or hard-cancelled a
// job that ignores its context, that may never happen, and the telemetry
// flush after it (which carries the error logs and
// tz_job_hard_cancelled_total) would never run. Past limit the step is logged
// at error level and left running; the process exits soon after.
func StepWithin(ctx context.Context, log *slog.Logger, name string, limit time.Duration, fn func()) bool {
	if log == nil {
		log = telemetry.Default()
	}
	bg := context.WithoutCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	t := time.NewTimer(limit)
	defer t.Stop()
	select {
	case <-done:
		log.InfoContext(bg, "shutdown: "+name+" closed")
		return true
	case <-t.C:
		log.ErrorContext(bg, "shutdown: "+name+" did not close in time; moving on",
			slog.String("limit", limit.String()))
		return false
	}
}
