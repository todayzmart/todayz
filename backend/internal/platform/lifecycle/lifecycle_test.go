package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/egress"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/queue"
)

// TestDrainOutlivesWriteTimeout (SC-GS-03): SHUTDOWN_DRAIN must exceed
// WriteTimeout, so a response still inside its write budget is never cut by
// the drain (like TestLeaseOutlivesWriteTimeout in httpx), and the api's
// pre-stop plus drain must stay under its container stop timeout. The worker's
// queue.Drain (grace + hardGrace) must stay under the worker's stop timeout,
// and its grace must exceed the 60 s job cap (D-98).
func TestDrainOutlivesWriteTimeout(t *testing.T) {
	require.Greater(t, Drain, httpx.WriteTimeout)
	require.Less(t, PreStopDeployed+Drain, APIStopTimeout)
	require.Less(t, PreStopLocal+Drain, APIStopTimeout)
	require.Less(t, queue.DrainGrace+queue.HardGrace, WorkerStopTimeout)
	require.Greater(t, queue.DrainGrace, 60*time.Second, "the grace must outlast the 60 s job cap")
	require.Equal(t, 75*time.Second, Drain)
	require.Equal(t, 90*time.Second, queue.DrainGrace)
	require.Equal(t, 30*time.Second, queue.HardGrace)

	// The env values are held to the same rules: the api refuses to start.
	_, err := APITimings(true, 0, httpx.WriteTimeout)
	require.ErrorContains(t, err, "SHUTDOWN_DRAIN")
	_, err = APITimings(true, 30*time.Second, 90*time.Second)
	require.ErrorContains(t, err, "container stop timeout")
}

func TestAPITimingsDefaults(t *testing.T) {
	d, err := APITimings(true, 0, 0)
	require.NoError(t, err)
	require.Equal(t, Timings{PreStop: 15 * time.Second, Drain: 75 * time.Second}, d)
	l, err := APITimings(false, 0, 0)
	require.NoError(t, err)
	require.Equal(t, Timings{PreStop: time.Second, Drain: 75 * time.Second}, l)
	c, err := APITimings(true, 10*time.Second, 90*time.Second)
	require.NoError(t, err)
	require.Equal(t, Timings{PreStop: 10 * time.Second, Drain: 90 * time.Second}, c)
}

// TestBaseContextIsNotTheSignal (SC-GS-03): request contexts derive from a
// background context that nothing cancels, never from the signal context.
func TestBaseContextIsNotTheSignal(t *testing.T) {
	srv := httpx.NewServer("127.0.0.1:0", http.NotFoundHandler())
	require.NotNil(t, srv.BaseContext)
	ctx := srv.BaseContext(nil)
	require.Nil(t, ctx.Done(), "a request context must not be cancellable by the signal")
	require.NoError(t, ctx.Err())
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

// probe and slowClient reach loopback only (egress guard); fresh() makes every
// call a new connection, like a load-balancer probe.
var (
	probe      = egress.NewClient(egress.Options{Block: true, Timeout: 2 * time.Second})
	slowClient = egress.NewClient(egress.Options{Block: true, Timeout: 30 * time.Second})
)

func fresh(c *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Close = true
	return c.Do(req)
}

func call(base, path string) (int, string) {
	resp, err := fresh(probe, base+path)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

const slowBody = "slow handler finished"

// slowRoute registers the test's own slow handler (not /v1/test/slow, which
// is W1-07's): it sleeps d unless its context ends first.
type slowRoute struct {
	started  chan struct{}
	finished chan time.Time
	ctxErr   chan error
}

func newSlowRoute(r chi.Router, d time.Duration) *slowRoute {
	s := &slowRoute{started: make(chan struct{}), finished: make(chan time.Time, 1), ctxErr: make(chan error, 1)}
	r.Get("/test-slow", func(w http.ResponseWriter, req *http.Request) {
		close(s.started)
		select {
		case <-time.After(d):
		case <-req.Context().Done():
		}
		s.ctxErr <- req.Context().Err()
		_, _ = io.WriteString(w, slowBody)
		s.finished <- time.Now()
	})
	return s
}

type slowResult struct {
	code int
	body string
	err  error
}

func callSlow(base string) <-chan slowResult {
	out := make(chan slowResult, 1)
	go func() {
		resp, err := fresh(slowClient, base+"/test-slow")
		if err != nil {
			out <- slowResult{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		out <- slowResult{code: resp.StatusCode, body: string(b), err: err}
	}()
	return out
}

// TestSIGTERMDrainsInFlightRequest (SC-GS-01, SC-GS-02): a real SIGTERM
// reaches the process while the test's 5 s handler runs. /readyz answers 503
// {"status":"draining"} at once while /healthz stays 200; after the 1 s
// pre-stop new connections are refused; the slow request still gets 200 with
// its full body and a live context; Run returns only after it.
func TestSIGTERMDrainsInFlightRequest(t *testing.T) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()

	ready := &health.Readiness{CacheFor: -1}
	r := chi.NewRouter()
	health.Register(r, ready, health.Options{})
	slow := newSlowRoute(r, 5*time.Second)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	base := "http://" + addr
	var logs syncBuffer
	s := Server{
		Name: "api", Srv: httpx.NewServer(addr, r), Ready: ready, Listener: l,
		PreStop: PreStopLocal, Drain: Drain, Log: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	runDone := make(chan error, 1)
	go func() { runDone <- s.Run(ctx) }()

	require.Eventually(t, func() bool { c, _ := call(base, "/readyz"); return c == 200 }, 5*time.Second, 20*time.Millisecond)
	res := callSlow(base)
	<-slow.started

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))
	sigAt := time.Now()

	// SC-GS-02: /readyz is 503 at once, inside the pre-stop; /healthz is 200.
	var body string
	require.Eventually(t, func() bool {
		var c int
		c, body = call(base, "/readyz")
		return c == http.StatusServiceUnavailable
	}, 500*time.Millisecond, 5*time.Millisecond)
	var rb map[string]string
	require.NoError(t, json.Unmarshal([]byte(body), &rb))
	require.Equal(t, map[string]string{"status": "draining"}, rb)
	c, _ := call(base, "/healthz")
	require.Equal(t, 200, c, "/healthz stays 200 while draining")
	require.Less(t, time.Since(sigAt), PreStopLocal, "both answered inside the pre-stop")

	// After the pre-stop, new connections are refused (the slow one runs on).
	var refusedAt time.Time
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			refusedAt = time.Now()
			return true
		}
		_ = conn.Close()
		return false
	}, 3*time.Second, 20*time.Millisecond)
	require.GreaterOrEqual(t, refusedAt.Sub(sigAt), PreStopLocal-50*time.Millisecond, "refused only after the pre-stop")
	select {
	case <-slow.finished:
		t.Fatal("the slow handler ended before the listener closed; the test proves nothing")
	default:
	}

	// SC-GS-01: the in-flight request finishes with 200 and its full body.
	got := <-res
	require.NoError(t, got.err)
	require.Equal(t, 200, got.code)
	require.Equal(t, slowBody, got.body)
	require.NoError(t, <-slow.ctxErr, "SIGTERM must not cancel a request context")
	finished := <-slow.finished
	require.GreaterOrEqual(t, finished.Sub(sigAt), 4*time.Second, "the handler ran its full 5 s")

	// The server (and so the process) stops only after it.
	select {
	case err := <-runDone:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the drain")
	}
	require.Contains(t, logs.String(), "api draining: /readyz is 503")
	require.Contains(t, logs.String(), `"api drained"`)
	require.NotContains(t, logs.String(), `"level":"ERROR"`)
}

// TestDrainDeadlineCutsAndLogs: a request still running when the drain budget
// ends is cut, Run reports ErrDrainDeadline and logs at error level.
func TestDrainDeadlineCutsAndLogs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ready := &health.Readiness{CacheFor: -1}
	r := chi.NewRouter()
	health.Register(r, ready, health.Options{})
	slow := newSlowRoute(r, 10*time.Second)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + l.Addr().String()
	var logs syncBuffer
	before := make(chan bool, 1)
	s := Server{
		Name: "api", Srv: httpx.NewServer("", r), Ready: ready, Listener: l,
		Drain: 200 * time.Millisecond, Log: slog.New(slog.NewJSONHandler(&logs, nil)),
		Before: func() error { before <- ready.Draining(); return nil },
	}
	runDone := make(chan error, 1)
	go func() { runDone <- s.Run(ctx) }()
	res := callSlow(base)
	<-slow.started
	cancel()
	require.True(t, <-before, "Before runs with /readyz already draining")
	select {
	case err := <-runDone:
		require.ErrorIs(t, err, ErrDrainDeadline)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return at the drain deadline")
	}
	require.Error(t, (<-res).err, "the cut request gets no response")
	require.Contains(t, logs.String(), `"level":"ERROR"`)
	require.Contains(t, logs.String(), "requests still running at the drain deadline")
}

// TestListenErrorReturns: a listen failure before any signal is returned.
func TestListenErrorReturns(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	s := Server{Name: "api", Srv: httpx.NewServer(l.Addr().String(), http.NotFoundHandler()), Drain: time.Second,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	require.Error(t, s.Run(context.Background()))
}
