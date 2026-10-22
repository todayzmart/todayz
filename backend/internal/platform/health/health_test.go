package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/buildinfo"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/migrate"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, Report) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var rep Report
	if path == "/readyz" {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rep))
	}
	return w, rep
}

func mount(rd *Readiness) http.Handler {
	r := chi.NewRouter()
	Register(r, rd, Options{Version: true})
	return r
}

func TestHealthzAndVersion(t *testing.T) {
	h := mount(&Readiness{})
	w, _ := get(t, h, "/healthz")
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `{"status":"ok"}`, w.Body.String())

	w, _ = get(t, h, "/version")
	require.Equal(t, 200, w.Code)
	var bi buildinfo.Info
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bi))
	require.NotEmpty(t, bi.Version)
	require.NotContains(t, w.Body.String(), `"go"`, "no toolchain version (T6)")

	r := chi.NewRouter()
	Register(r, &Readiness{}, Options{})
	w, _ = get(t, r, "/version")
	require.Equal(t, 404, w.Code, "not mounted without Options.Version")
}

// TestReadyzIsCached (T6): one run serves every call for CacheFor, and
// concurrent calls share the run in flight.
func TestReadyzIsCached(t *testing.T) {
	var runs atomic.Int32
	release := make(chan struct{})
	clk := clock.NewFake(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	rd := &Readiness{Clock: clk, Checks: []Check{{Name: "db", Fn: func(context.Context) error {
		runs.Add(1)
		<-release
		return nil
	}}}}
	h := mount(rd)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			w, _ := get(t, h, "/readyz")
			require.Equal(t, 200, w.Code)
		})
	}
	time.Sleep(50 * time.Millisecond) // let the 20 callers queue behind the run
	close(release)
	wg.Wait()
	require.Equal(t, int32(1), runs.Load(), "one run for 20 concurrent calls")

	clk.Advance(DefaultCacheFor - time.Millisecond)
	get(t, h, "/readyz")
	require.Equal(t, int32(1), runs.Load(), "still cached")
	clk.Advance(time.Millisecond)
	get(t, h, "/readyz")
	require.Equal(t, int32(2), runs.Load(), "expired")
}

func TestReadyzFailureHidesErrorText(t *testing.T) {
	var logs bytes.Buffer
	rd := &Readiness{
		Log:     slog.New(slog.NewJSONHandler(&logs, nil)),
		Timeout: 200 * time.Millisecond,
		Checks: []Check{
			{Name: "ok", Fn: func(context.Context) error { return nil }},
			{Name: "db", Fn: func(context.Context) error { return errors.New("dial tcp db.internal.example:5432: refused") }},
			{Name: "slow", Fn: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }},
		},
	}
	w, rep := get(t, mount(rd), "/readyz")
	require.Equal(t, 503, w.Code)
	require.Equal(t, Report{Status: "unavailable", Checks: map[string]string{"ok": "ok", "db": "fail", "slow": "fail"}}, rep)
	require.NotContains(t, w.Body.String(), "db.internal.example")
	require.Contains(t, logs.String(), "db.internal.example", "the cause goes to the log")
}

// TestReadyzAgainstDatabase: as todayz_app, readyz is OK on a migrated
// database and 503 on an empty one.
func TestReadyzAgainstDatabase(t *testing.T) {
	ctx := context.Background()
	checks := func(db *testkit.DB) *Readiness {
		m, err := migrate.New(db.Pool, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = m.Close() })
		return &Readiness{Checks: []Check{
			DBCheck(db.Pool),
			MigrationsCheck(m),
			{Name: "river", Fn: func(ctx context.Context) error { return queue.Check(ctx, db.Pool, nil) }},
		}}
	}

	ready := testkit.NewDB(t)
	w, rep := get(t, mount(checks(ready)), "/readyz")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, map[string]string{"db": "ok", "migrations": "ok", "river": "ok"}, rep.Checks)

	empty := testkit.NewEmptyDB(t)
	w, rep = get(t, mount(checks(empty)), "/readyz")
	require.Equal(t, 503, w.Code)
	require.Equal(t, map[string]string{"db": "ok", "migrations": "fail", "river": "fail"}, rep.Checks)

	// One migration behind is not ready either.
	om, err := migrate.New(ready.OwnerPool, nil)
	require.NoError(t, err)
	defer func() { _ = om.Close() }()
	require.NoError(t, om.Down(ctx))
	w, rep = get(t, mount(checks(ready)), "/readyz")
	require.Equal(t, 503, w.Code)
	require.Equal(t, "fail", rep.Checks["migrations"])
}
