package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	ptel "github.com/todayz/backend/internal/platform/telemetry"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var now = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

type rig struct {
	h    http.Handler
	db   *testkit.DB
	logs *bytes.Buffer
	mr   *sdkmetric.ManualReader
	mp   *sdkmetric.MeterProvider
}

func newRig(t *testing.T, env config.Env) *rig {
	t.Helper()
	d := testkit.NewDB(t)
	mr := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(mr), sdkmetric.WithView(ptel.ExponentialView()))
	met, err := ptel.NewMetrics(mp, "api")
	require.NoError(t, err)
	r := chi.NewRouter()
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(ptel.WithLogger(req.Context(), log)))
		})
	})
	_, _, err = Register(r, Deps{
		DB: db.New(d.Pool), Clock: clock.NewFake(now), Metrics: met,
		Env: env, APIAddr: "127.0.0.1:8180", TestRoutes: true,
	})
	require.NoError(t, err)
	return &rig{h: r, db: d, logs: logs, mr: mr, mp: mp}
}

func (g *rig) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, req)
	return w
}

const goodEvents = `{"app":"customer","appVersion":"3f2a9c1","platform":"web",
"anonId":"01JB0S1W6W8ZV7T1C3A6M9N4PQ","sessionId":"01JB0S1W6W8ZV7T1C3A6M9N4PR",
"events":[{"id":"01JB0S1W6W8ZV7T1C3A6M9N4PS","name":"app_opened","t":"2026-10-23T09:59:00Z","props":{"launch":"cold"},"geoCell":"88618a1b3dfffff"}]}`

func TestEventsAreStoredAndRetriesAreSkipped(t *testing.T) {
	g := newRig(t, config.EnvTest)
	w := g.do("POST", "/v1/events", goodEvents)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.JSONEq(t, `{"accepted":1}`, w.Body.String())

	var name, app, platform, geo, version string
	var props []byte
	var anon *string
	require.NoError(t, g.db.Pool.QueryRow(context.Background(),
		`SELECT name, app, platform, geo_cell, app_version, props, anon_id::text FROM events`).
		Scan(&name, &app, &platform, &geo, &version, &props, &anon))
	require.Equal(t, "app_opened", name)
	require.Equal(t, "customer", app)
	require.Equal(t, "web", platform)
	require.Equal(t, "88618a1b3dfffff", geo)
	require.Equal(t, "3f2a9c1", version)
	require.JSONEq(t, `{"launch":"cold"}`, string(props))
	require.NotNil(t, anon)

	// The same batch again stores nothing new.
	w = g.do("POST", "/v1/events", goodEvents)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.JSONEq(t, `{"accepted":0}`, w.Body.String())
}

func TestEventsRejectedByTheCatalogue(t *testing.T) {
	g := newRig(t, config.EnvTest)
	ev := func(name, props, t string) string {
		return `{"app":"customer","events":[{"id":"01JB0S1W6W8ZV7T1C3A6M9N4PS","name":"` + name + `","t":"` + t + `","props":` + props + `}]}`
	}
	cases := map[string]string{
		"unknown event":        ev("made_up", `{}`, "2026-10-23T09:59:00Z"),
		"bad enum value":       ev("app_opened", `{"launch":"lukewarm"}`, "2026-10-23T09:59:00Z"),
		"missing prop":         ev("app_opened", `{}`, "2026-10-23T09:59:00Z"),
		"free text prop":       ev("app_opened", `{"launch":"cold","query":"milk near me"}`, "2026-10-23T09:59:00Z"),
		"too old":              ev("app_opened", `{"launch":"cold"}`, "2026-01-01T00:00:00Z"),
		"in the future":        ev("app_opened", `{"launch":"cold"}`, "2026-10-24T10:00:00Z"),
		"server-emitted event": ev("order_placed", `{}`, "2026-10-23T09:59:00Z"),
	}
	for name, body := range cases {
		w := g.do("POST", "/v1/events", body)
		require.Equal(t, http.StatusUnprocessableEntity, w.Code, name+": "+w.Body.String())
		var p struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
		require.Equal(t, "validation_failed", p.Code, name)
		require.NotEmpty(t, p.Fields, name)
		require.NotContains(t, w.Body.String(), "milk near me", name)
		require.NotContains(t, w.Body.String(), "lukewarm", name)
	}
	var n int
	require.NoError(t, g.db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM events`).Scan(&n))
	require.Zero(t, n, "a rejected batch stores nothing")
}

func TestEventsPartitionExists(t *testing.T) {
	g := newRig(t, config.EnvTest)
	var parent string
	var policies int
	require.NoError(t, g.db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM partition_policies WHERE parent = 'events'`).Scan(&policies))
	require.Equal(t, 1, policies)
	require.NoError(t, g.db.Pool.QueryRow(context.Background(),
		`SELECT count(*)::text FROM pg_inherits WHERE inhparent = 'events'::regclass`).Scan(&parent))
	require.NotEqual(t, "0", parent)
}

func TestRumRecordsVitalsAndLogsScrubbedErrors(t *testing.T) {
	g := newRig(t, config.EnvTest)
	body := `{"app":"customer","release":"customer@3f2a9c1","platform":"web","conn":"4g","samples":[
{"type":"vital","name":"LCP","value":1820.5,"route":"/search","detail":{"rating":"good"},"t":1792660000000},
{"type":"vital","name":"NOPE","value":1,"route":"/search","t":1792660000000},
{"type":"error","name":"TypeError","route":"/r/$token","detail":{"message":"OTP 482913 for 9876543210 failed"},"t":1792660001000}]}`
	w := g.do("POST", "/v1/rum", body)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	var rm metricdata.ResourceMetrics
	require.NoError(t, g.mr.Collect(context.Background(), &rm))
	seen := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			seen[m.Name] = true
		}
	}
	require.True(t, seen["tz_rum_lcp_seconds"], "the LCP histogram")
	require.True(t, seen["tz_rum_errors_total"], "the error counter")
	require.Empty(t, ptel.CheckResourceMetrics(&rm), "only allowed labels")

	out := g.logs.String()
	require.Contains(t, out, `"fingerprint"`)
	require.Contains(t, out, `"app_version":"3f2a9c1"`)
	require.NotContains(t, out, "482913")
	require.NotContains(t, out, "9876543210")
}

func TestRumFingerprintIgnoresNumbers(t *testing.T) {
	require.Equal(t, errorFingerprint("customer", "Err", "item 12 missing"), errorFingerprint("customer", "Err", "item 7 missing"))
	require.NotEqual(t, errorFingerprint("customer", "Err", "item 12 missing"), errorFingerprint("business", "Err", "item 12 missing"))
}

func TestSlowRoutesShareTheTestClockGate(t *testing.T) {
	for _, env := range []config.Env{config.EnvStaging, config.EnvProd} {
		g := newRig(t, env)
		require.Equal(t, http.StatusNotFound, g.do("GET", "/v1/test/slow?ms=1", "").Code, string(env))
		require.Equal(t, http.StatusNotFound, g.do("GET", "/v1/test/slow-sql?ms=1", "").Code, string(env))
	}
	g := newRig(t, config.EnvLocal)
	w := g.do("GET", "/v1/test/slow?ms=20", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"sleptMs":20}`, w.Body.String())
	w = g.do("GET", "/v1/test/slow-sql?ms=20", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"sleptMs":20}`, w.Body.String())
	for _, q := range []string{"", "?ms=-1", "?ms=30001", "?ms=abc"} {
		require.Equal(t, http.StatusUnprocessableEntity, g.do("GET", "/v1/test/slow"+q, "").Code, q)
	}
}

func TestSlowRoutesNeedALoopbackAddress(t *testing.T) {
	r := chi.NewRouter()
	_, gated, err := Register(r, Deps{Env: config.EnvLocal, APIAddr: ":8180", TestRoutes: true, Clock: clock.NewFake(now)})
	require.NoError(t, err)
	require.False(t, gated)
	_, gated, err = Register(chi.NewRouter(), Deps{Env: config.EnvLocal, APIAddr: "127.0.0.1:8180", TestRoutes: false, Clock: clock.NewFake(now)})
	require.NoError(t, err)
	require.False(t, gated)
}

// TestQueueStats reads the backlog the queue gauges and the W5-04 publisher report.
func TestQueueStats(t *testing.T) {
	g := newRig(t, config.EnvTest)
	ctx := context.Background()
	ins := func(state, queue string, scheduled time.Time) {
		_, err := g.db.OwnerPool.Exec(ctx, `INSERT INTO river_job (state, queue, kind, args, scheduled_at, max_attempts, priority)
VALUES ($1::river_job_state, $2, 'k', '{}', $3, 3, 1)`, state, queue, scheduled)
		require.NoError(t, err)
	}
	ins("available", "default", now.Add(-5*time.Minute))
	ins("available", "default", now.Add(-time.Minute))
	ins("retryable", "default", now.Add(-2*time.Minute))
	ins("available", "default", now.Add(time.Hour)) // not due yet
	ins("running", "default", now.Add(-time.Hour))  // not waiting

	stats, err := ptel.QueueStats(ctx, g.db.Pool, now)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	require.Equal(t, int64(3), stats[0].Depth)
	require.Equal(t, 5*time.Minute, stats[0].OldestAge)
	age, err := ptel.OldestAvailableAge(ctx, g.db.Pool, now)
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, age)
}
