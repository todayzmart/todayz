package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/testclock"
)

type nopStore struct{}

func (nopStore) Offset(context.Context) (time.Duration, error)  { return 0, nil }
func (nopStore) SetOffset(context.Context, time.Duration) error { return nil }
func (nopStore) AdvanceOffset(_ context.Context, d time.Duration) (time.Duration, error) {
	return d, nil
}
func (nopStore) DueJobs(context.Context, time.Time) (int, error) { return 0, nil }

// appKeys returns fresh random D-18 keys (nothing secret is written here).
func appKeys() map[string]string {
	rnd := func(n int) []byte {
		b := make([]byte, n)
		_, _ = rand.Read(b)
		return b
	}
	seed := rnd(32)
	pub, _ := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	set, _ := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": "k1",
		"x": base64.RawURLEncoding.EncodeToString(pub),
		"d": base64.RawURLEncoding.EncodeToString(seed),
	}}})
	m := map[string]string{"JWT_SIGNING_KEYS": string(set), "JWT_CURRENT_KID": "k1"}
	for _, n := range config.AppKeyNames[2:] {
		m[n] = base64.StdEncoding.EncodeToString(rnd(32))
	}
	return m
}

func env(m map[string]string) config.Getenv { return func(k string) string { return m[k] } }

func runCmd(args []string, e map[string]string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(context.Background(), args, env(e), &out, &errb)
	return code, out.String(), errb.String()
}

func TestUsageAndVersion(t *testing.T) {
	code, _, stderr := runCmd(nil, nil)
	require.Equal(t, 2, code)
	require.Contains(t, stderr, "usage: todayz")

	code, _, _ = runCmd([]string{"bogus"}, nil)
	require.Equal(t, 2, code)
	code, _, _ = runCmd([]string{"migrate"}, nil)
	require.Equal(t, 2, code)
	code, _, _ = runCmd([]string{"migrate", "sideways"}, map[string]string{"APP_ENV": "local"})
	require.Equal(t, 2, code)
	code, _, _ = runCmd([]string{"seed", "everything"}, map[string]string{"APP_ENV": "local"})
	require.Equal(t, 2, code)

	code, out, _ := runCmd([]string{"version"}, nil)
	require.Equal(t, 0, code)
	require.Contains(t, out, `"version"`)
}

func TestSeedRefusedWhenDeployed(t *testing.T) {
	for _, e := range []string{"staging", "prod"} {
		code, _, stderr := runCmd([]string{"seed", "dev"}, deployedEnv(e))
		require.Equal(t, 1, code, e)
		require.Contains(t, stderr, "refused", e)
	}
}

func TestMigrateDownRefusedWhenDeployed(t *testing.T) {
	code, _, stderr := runCmd([]string{"migrate", "down"}, deployedEnv("prod"))
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "refused")
}

func TestConfigErrorsNameVariablesOnly(t *testing.T) {
	const secret = "postgres://u:hunter2-very-secret@db.example:5432/x"
	e := deployedEnv("prod")
	e["DATABASE_URL"] = secret
	e["API_ADDR"] = "nope"
	code, out, stderr := runCmd([]string{"migrate", "status"}, e)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "API_ADDR")
	require.NotContains(t, out+stderr, "hunter2")
}

// deployedEnv is a config that loads for staging/prod without a database.
func deployedEnv(appEnv string) map[string]string {
	m := map[string]string{
		"APP_ENV":        appEnv,
		"API_PUBLIC_URL": "https://api.todayz.in",
		"DATABASE_URL":   "postgres://todayz_app@127.0.0.1:1/todayz",
	}
	for k, v := range appKeys() {
		m[k] = v
	}
	return m
}

// TestRoutesPerEnv: health routes everywhere, /v1/test/clock only in local
// and test (D-80), through the real router and pipeline.
func TestRoutesPerEnv(t *testing.T) {
	for _, c := range []struct {
		env   config.Env
		clock int // /v1/test/clock and /version: local and test only
	}{
		{config.EnvLocal, 200}, {config.EnvTest, 200}, {config.EnvStaging, 404}, {config.EnvProd, 404},
	} {
		t.Run(string(c.env), func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			var tc *testclock.Service
			if c.env.AllowsTestRoutes() {
				tc = testclock.New(clock.NewOffset(nil), nopStore{}, log)
			}
			d := deps{Config: &config.Config{Env: c.env, APIAddr: config.DefaultLocalAPIAddr}, Log: log, Clock: clock.Real{}, Ready: &health.Readiness{}, TestClock: tc}
			h := newAPIRouter(d, idempotency.NewMemoryStore(), nil)
			for path, want := range map[string]int{"/healthz": 200, "/readyz": 200, "/version": c.clock, "/v1/test/clock": c.clock, "/v1/nope": 404} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, want, w.Code, path)
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/test/clock", strings.NewReader(`{"advance":"1m","wait":"0s"}`))
			req.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(w, req)
			require.Equal(t, c.clock, w.Code)
		})
	}
}

// TestTestClockNeedsLoopbackAPIAddr (W4): a local or test API listening on
// anything but loopback serves no test clock.
func TestTestClockNeedsLoopbackAPIAddr(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for addr, want := range map[string]int{
		"127.0.0.1:8180": 200, "localhost:8180": 200, "[::1]:8180": 200,
		":8180": 404, "0.0.0.0:8180": 404, "10.0.0.5:8180": 404, "api.todayz.in:8180": 404,
	} {
		tc := testclock.New(clock.NewOffset(nil), nopStore{}, log)
		d := deps{Config: &config.Config{Env: config.EnvLocal, APIAddr: addr}, Log: log, Clock: clock.Real{}, Ready: &health.Readiness{}, TestClock: tc}
		h := newAPIRouter(d, idempotency.NewMemoryStore(), nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/test/clock", nil))
		require.Equal(t, want, w.Code, addr)
	}
}
