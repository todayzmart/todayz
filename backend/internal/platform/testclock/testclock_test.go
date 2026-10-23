package testclock

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type memStore struct {
	mu     sync.Mutex
	offset time.Duration
	due    []int // successive DueJobs answers; the last one repeats
}

func (m *memStore) Offset(context.Context) (time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offset, nil
}

func (m *memStore) SetOffset(_ context.Context, d time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.offset = d
	return nil
}

func (m *memStore) AdvanceOffset(_ context.Context, d time.Duration) (time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.offset += d
	return m.offset, nil
}

func (m *memStore) DueJobs(context.Context, time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.due) == 0 {
		return 0, nil
	}
	n := m.due[0]
	if len(m.due) > 1 {
		m.due = m.due[1:]
	}
	return n, nil
}

func newService(base clock.Clock, st Store) *Service {
	return New(clock.NewOffset(base), st, quiet())
}

func router(env config.Env, s *Service) http.Handler {
	r := httpx.NewRouter(httpx.Options{Log: quiet()})
	Register(r, env, "127.0.0.1:8180", s)
	return r
}

func call(h http.Handler, method, body string) *httptest.ResponseRecorder {
	return callCT(h, method, body, "application/json")
}

func callCT(h http.Handler, method, body, contentType string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, Path, rd)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestRegisteredOnlyInLocalAndTest is the D-80 table test across all four envs.
func TestRegisteredOnlyInLocalAndTest(t *testing.T) {
	for _, c := range []struct {
		env  config.Env
		want int
	}{
		{config.EnvLocal, 200},
		{config.EnvTest, 200},
		{config.EnvStaging, 404},
		{config.EnvProd, 404},
	} {
		t.Run(string(c.env), func(t *testing.T) {
			h := router(c.env, newService(nil, &memStore{}))
			require.Equal(t, c.want, call(h, "GET", "").Code)
			w := call(h, "POST", `{"advance":"31m"}`)
			require.Equal(t, c.want, w.Code)
			if c.want == 404 {
				require.Equal(t, problem.ContentType, w.Header().Get("Content-Type"))
			}
		})
	}
	require.Len(t, config.Envs, 4, "a new env needs a row above")
}

func decode(t *testing.T, w *httptest.ResponseRecorder) State {
	t.Helper()
	require.Equal(t, 200, w.Code, w.Body.String())
	var s State
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))
	return s
}

func TestGetSetAdvanceReset(t *testing.T) {
	t0 := time.Date(2026, 10, 22, 4, 30, 0, 0, time.UTC)
	st := &memStore{}
	s := newService(clock.NewFake(t0), st)
	h := router(config.EnvTest, s)

	got := decode(t, call(h, "GET", ""))
	require.True(t, got.Now.Equal(t0))
	require.Zero(t, got.OffsetMs)

	got = decode(t, call(h, "POST", `{"advance":"31m"}`))
	require.True(t, got.Now.Equal(t0.Add(31*time.Minute)))
	require.Equal(t, int64(31*60*1000), got.OffsetMs)
	require.Equal(t, 31*time.Minute, st.offset, "the shared offset is stored")
	require.NotNil(t, got.DueJobs)

	got = decode(t, call(h, "POST", `{"advance":"1h"}`))
	require.True(t, got.Now.Equal(t0.Add(91*time.Minute)), "advance accumulates")

	got = decode(t, call(h, "POST", `{"set":"2027-01-01T00:00:00+05:30"}`))
	require.True(t, got.Now.Equal(time.Date(2026, 12, 31, 18, 30, 0, 0, time.UTC)))

	got = decode(t, call(h, "POST", `{"reset":true}`))
	require.True(t, got.Now.Equal(t0))
	require.Zero(t, st.offset)
}

func TestValidation(t *testing.T) {
	h := router(config.EnvLocal, newService(nil, &memStore{}))
	for _, body := range []string{
		``, `{}`, `{"advance":"31m","reset":true}`, `{"advance":"-5m"}`, `{"advance":"soon"}`,
		`{"set":"tomorrow"}`, `{"advance":"1m","wait":"2h"}`,
	} {
		w := call(h, "POST", body)
		require.Equal(t, 422, w.Code, body)
		require.Contains(t, w.Body.String(), `"validation_failed"`, body)
	}
	w := call(h, "POST", `{"advance":"1m","extra":1}`)
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), `"bad_request"`)
}

// TestPostNeedsJSONContentType (T9): a form or text POST (what a cross-site
// form can send without a preflight) is refused with 415.
func TestPostNeedsJSONContentType(t *testing.T) {
	st := &memStore{}
	h := router(config.EnvLocal, newService(nil, st))
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "application/jsonx"} {
		w := callCT(h, "POST", `{"advance":"31m","wait":"0s"}`, ct)
		require.Equal(t, http.StatusUnsupportedMediaType, w.Code, ct)
		require.Contains(t, w.Body.String(), `"unsupported_media_type"`, ct)
	}
	require.Zero(t, st.offset, "nothing moved")
	w := callCT(h, "POST", `{"advance":"31m","wait":"0s"}`, "application/json; charset=utf-8")
	require.Equal(t, 200, w.Code, w.Body.String())
}

// TestWaitIsCappedAt50s: the wait ends inside the server's 60 s write timeout.
func TestWaitIsCappedAt50s(t *testing.T) {
	require.Equal(t, 50*time.Second, maxWait)
	h := router(config.EnvLocal, newService(nil, &memStore{}))
	require.Equal(t, 422, call(h, "POST", `{"advance":"1m","wait":"51s"}`).Code)
	require.Equal(t, 200, call(h, "POST", `{"advance":"1m","wait":"0s"}`).Code)
}

func TestLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8180": true, "127.0.0.2:1": true, "localhost:8180": true, "[::1]:8180": true,
		":8180": false, "0.0.0.0:8180": false, "[::]:8180": false, "10.0.0.1:80": false,
		"example.com:80": false, "127.0.0.1": false, "": false,
	} {
		require.Equal(t, want, Loopback(addr), addr)
	}
}

func TestWaitsForDueJobs(t *testing.T) {
	st := &memStore{due: []int{3, 1, 0}}
	h := router(config.EnvLocal, newService(nil, st))
	got := decode(t, call(h, "POST", `{"advance":"1m","wait":"5s"}`))
	require.Zero(t, *got.DueJobs, "returned once the due jobs were worked")

	st.due = []int{2}
	got = decode(t, call(h, "POST", `{"advance":"1m","wait":"0s"}`))
	require.Equal(t, 2, *got.DueJobs, "wait 0s reports what is still due")
}

// TestSharedOffsetOverNotify: two processes (api and worker) share the offset
// through the test_clock row and NOTIFY, as todayz_app.
func TestSharedOffsetOverNotify(t *testing.T) {
	db := testkit.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	api := newService(nil, PGStore{Pool: db.Pool})
	worker := newService(nil, PGStore{Pool: db.Pool})
	l := pgnotify.New(db.Pool, quiet())
	worker.Attach(l)
	go func() { _ = l.Run(ctx) }()
	<-l.Ready()

	h := router(config.EnvTest, api)
	got := decode(t, call(h, "POST", `{"advance":"45m","wait":"0s"}`))
	require.Equal(t, int64(45*60*1000), got.OffsetMs)
	require.Equal(t, 0, *got.DueJobs)
	require.Eventually(t, func() bool { return worker.Clock.Offset() == 45*time.Minute }, 5*time.Second, 10*time.Millisecond)

	// A process that starts later loads the stored offset.
	late := newService(nil, PGStore{Pool: db.Pool})
	require.NoError(t, late.Load(ctx))
	require.Equal(t, 45*time.Minute, late.Clock.Offset())

	// A scheduled job becomes due once the clock passes it.
	_, err := db.Pool.Exec(ctx, `INSERT INTO river_job (args, kind, max_attempts, queue, state, scheduled_at, priority)
		VALUES ('{}', 'platform.heartbeat', 1, 'default', 'scheduled', now() + interval '2 hours', 1)`)
	require.NoError(t, err)
	n, err := PGStore{Pool: db.Pool}.DueJobs(ctx, api.Clock.Now())
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = PGStore{Pool: db.Pool}.DueJobs(ctx, api.Clock.Now().Add(3*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got = decode(t, call(h, "POST", `{"reset":true,"wait":"0s"}`))
	require.Zero(t, got.OffsetMs)
	require.Eventually(t, func() bool { return worker.Clock.Offset() == 0 }, 5*time.Second, 10*time.Millisecond)
}

// TestConcurrentAdvancesAddUp: two processes advancing at once both count;
// a read-then-write advance would lose one of them.
func TestConcurrentAdvancesAddUp(t *testing.T) {
	db := testkit.NewDB(t)
	ctx := context.Background()
	a := router(config.EnvTest, newService(nil, PGStore{Pool: db.Pool}))
	b := router(config.EnvTest, newService(nil, PGStore{Pool: db.Pool}))
	var wg sync.WaitGroup
	for i := range 20 {
		h := a
		if i%2 == 1 {
			h = b
		}
		wg.Go(func() {
			w := call(h, "POST", `{"advance":"1m","wait":"0s"}`)
			require.Equal(t, 200, w.Code, w.Body.String())
		})
	}
	wg.Wait()
	got, err := PGStore{Pool: db.Pool}.Offset(ctx)
	require.NoError(t, err)
	require.Equal(t, 20*time.Minute, got)
}
