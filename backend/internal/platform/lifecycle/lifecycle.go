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
// The caller then closes the rest in order: in-process loops (LISTEN/NOTIFY,
// caches), the DB pool, and the telemetry flush (TelemetryFlush).
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
)

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
// must stay under APIStopTimeout, so the container runtime never kills the
// process mid-drain. The api refuses to start otherwise.
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
	if t.PreStop+t.Drain >= APIStopTimeout {
		return t, fmt.Errorf("lifecycle: SHUTDOWN_PRESTOP + SHUTDOWN_DRAIN (%s) must stay under the %s container stop timeout",
			t.PreStop+t.Drain, APIStopTimeout)
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
// package comment says. It returns after the server has stopped. A listen
// error before the signal is returned as is.
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
		return err
	case <-ctx.Done():
	}

	// 1. /readyz turns 503 at once.
	if s.Ready != nil {
		s.Ready.SetDraining()
	}
	log.InfoContext(bg, s.Name+" draining: /readyz is 503",
		slog.Duration("prestop", s.PreStop), slog.Duration("drain", s.Drain))

	// 2. Keep serving while the load balancer takes the instance out.
	if s.PreStop > 0 {
		t := time.NewTimer(s.PreStop)
		select {
		case <-t.C:
		case err := <-errc: // the server died during the pre-stop
			t.Stop()
			if s.Before != nil {
				err = errors.Join(err, s.Before())
			}
			return err
		}
	}

	// 3. The worker's queue drain, with /readyz still answering 503.
	var beforeErr error
	if s.Before != nil {
		beforeErr = s.Before()
	}

	// 4. Stop accepting, wait for in-flight requests.
	log.InfoContext(bg, s.Name+" shutting down: waiting for in-flight requests", slog.Duration("drain", s.Drain))
	sctx, cancel := context.WithTimeout(context.Background(), s.Drain)
	defer cancel()
	err := s.Srv.Shutdown(sctx)
	if err != nil {
		log.ErrorContext(bg, s.Name+": requests still running at the drain deadline; closing them",
			slog.Duration("drain", s.Drain), slog.String("err", err.Error()))
		_ = s.Srv.Close()
		err = fmt.Errorf("%w: %w", ErrDrainDeadline, err)
	}
	if serr := <-errc; serr != nil && !errors.Is(serr, http.ErrServerClosed) {
		err = errors.Join(err, serr)
	}
	if err == nil {
		log.InfoContext(bg, s.Name+" drained")
	}
	return errors.Join(beforeErr, err)
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
