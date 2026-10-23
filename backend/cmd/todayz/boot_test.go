package main

import (
	"errors"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/egress"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/idempotency"
)

// keepProcessDefaults restores slog's default, the standard log package's
// output and http.DefaultTransport after a test that boots, and points the
// standard log package at w (it stands in for stderr).
func keepProcessDefaults(t *testing.T, w *syncBuffer) {
	t.Helper()
	oldSlog, oldOut, oldFlags, oldTransport := slog.Default(), log.Writer(), log.Flags(), http.DefaultTransport //nolint:forbidigo // saved to restore after the test
	log.SetOutput(w)
	t.Cleanup(func() {
		slog.SetDefault(oldSlog)
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
		http.DefaultTransport = oldTransport
	})
}

// TestBootRedactsEveryLogPath (B2): a 5xx through the full router whose error
// carries a phone number and an OTP, from a handler that also writes through
// the standard log package and slog.Default (the paths that bypass the
// injected logger). After boot, nothing reaches stderr or the log
// unredacted, and the response body never carries the error.
func TestBootRedactsEveryLogPath(t *testing.T) {
	var stderr syncBuffer
	keepProcessDefaults(t, &stderr)

	e := deployedEnv("test")
	cfg, lg, err := loadAndLog("api", env(e), &stderr)
	require.NoError(t, err)

	d := deps{Config: cfg, Log: lg, Clock: clock.Real{}, Ready: &health.Readiness{}}
	r, err := buildRouter(d, idempotency.NewMemoryStore(), nil)
	require.NoError(t, err)
	r.Get("/v1/boom", func(w http.ResponseWriter, req *http.Request) {
		log.Printf("stray std log: sms to 9876543210 otp 482913")        //nolint:forbidigo // the bypass under test
		slog.Default().Error("stray slog: sms to 9876543210 otp 482913") //nolint:forbidigo // the bypass under test
		problem.Write(w, req, problem.Wrap("internal", errors.New("sms to 9876543210 otp 482913 failed")))
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/boom", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)

	out := stderr.String()
	require.Contains(t, out, "stray std log", "the standard log package went through the logger")
	require.Contains(t, out, "stray slog")
	require.Contains(t, out, `"err_code":"internal"`, "the 5xx was logged")
	for _, s := range []string{"9876543210", "482913"} {
		require.NotContains(t, out, s)
		require.NotContains(t, w.Body.String(), s)
	}
}

// TestBootInstallsEgressGuard (T3): under no-egress (APP_ENV=test) boot
// replaces http.DefaultTransport, so even a plain &http.Client{} cannot
// leave the host.
func TestBootInstallsEgressGuard(t *testing.T) {
	var stderr syncBuffer
	keepProcessDefaults(t, &stderr)
	cfg, _, err := loadAndLog("api", env(deployedEnv("test")), &stderr)
	require.NoError(t, err)
	require.True(t, cfg.NoEgress)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://192.0.2.10/", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req) //nolint:forbidigo // the test checks that the default client is guarded
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, egress.ErrBlocked)
}
