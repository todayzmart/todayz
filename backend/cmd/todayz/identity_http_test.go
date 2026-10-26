package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/testclock"
	"github.com/todayz/backend/internal/testkit"
)

// identityAPI is the real router and pipeline (slots 1–15, the contract
// validator) on a fresh database. SMS is the drop provider, so sign-in uses a
// reserved test number and the test code, as the e2e journeys do.
type identityAPI struct {
	t   *testing.T
	h   http.Handler
	clk *clock.Fake
	db  *testkit.DB
}

const (
	appOrigin  = "https://app.todayz.localhost"
	testPhone  = "+919000000007"
	testOTP    = "000000"
	cookieName = "__Secure-tz_rt_c"
)

func newIdentityAPI(t *testing.T) *identityAPI {
	t.Helper()
	tdb := testkit.NewDB(t)
	cfg, err := config.Load(env(localEnv(tdb)), nil)
	require.NoError(t, err)
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// The test clock is mounted as in a local api, which is what lets the
	// reserved test number sign in with 000000 (identity.Deps.TestRoutes).
	d := deps{
		Config: cfg, Log: log, Clock: clk,
		DB: db.New(tdb.Pool), Ready: &health.Readiness{},
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
	}
	h, err := buildRouter(d, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	return &identityAPI{t: t, h: h, clk: clk, db: tdb}
}

func (a *identityAPI) do(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	a.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "https://api.todayz.localhost"+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "127.0.0.1:5555"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m), w.Body.String())
	return m
}

// signIn sends and verifies over HTTP; it returns the access token and the
// refresh cookie.
func (a *identityAPI) signIn() (string, *http.Cookie) {
	a.t.Helper()
	w := a.do(http.MethodPost, "/v1/auth/otp/send", `{"phone":"`+testPhone+`","app":"customer","purpose":"login","turnstileToken":"XXXX.DUMMY.TOKEN.XXXX"}`, nil)
	require.Equal(a.t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(a.t, "no-store", w.Header().Get("Cache-Control"))
	sent := decode(a.t, w)
	require.InDelta(a.t, 30, sent["resendAfter"], 0)

	body := `{"app":"customer","requestId":"` + sent["requestId"].(string) + `","code":"` + testOTP + `","deviceId":"` + ids.New(a.clk.Now()).String() + `"}`
	w = a.do(http.MethodPost, "/v1/auth/otp/verify", body, map[string]string{"User-Agent": "identity-http-test"})
	require.Equal(a.t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(a.t, "no-store", w.Header().Get("Cache-Control"))
	out := decode(a.t, w)
	require.Equal(a.t, "Bearer", out["tokenType"])
	require.NotContains(a.t, w.Body.String(), "refresh", "the refresh token travels only in the cookie")
	c := refreshCookieOf(a.t, w)
	a.clk.Advance(30 * time.Second) // past the resend gap for the next sign-in
	return out["accessToken"].(string), c
}

func refreshCookieOf(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cs := w.Result().Cookies()
	require.Len(t, cs, 1, w.Header().Values("Set-Cookie"))
	return cs[0]
}

// D-14: the refresh cookie is host-only, Path=/v1/auth, HttpOnly, Secure,
// SameSite=Strict, and __Secure- prefixed.
func TestIdentityHTTPCookie(t *testing.T) {
	a := newIdentityAPI(t)
	_, c := a.signIn()
	require.Equal(t, cookieName, c.Name)
	require.Equal(t, "/v1/auth", c.Path)
	require.Empty(t, c.Domain, "host-only")
	require.True(t, c.HttpOnly)
	require.True(t, c.Secure)
	require.Equal(t, http.SameSiteStrictMode, c.SameSite)
	require.Equal(t, int((30 * 24 * time.Hour).Seconds()), c.MaxAge)
}

func refreshHeaders(c *http.Cookie, origin string) map[string]string {
	h := map[string]string{"X-TZ-Client": "customer-pwa/1.0"}
	if origin != "" {
		h["Origin"] = origin
	}
	if c != nil {
		h["Cookie"] = c.Name + "=" + c.Value
	}
	return h
}

// AU-16: refresh and logout need the app's own origin and X-TZ-Client; a
// refused request does not clear the cookie (a forged request must not sign
// anyone out). AU-07 over HTTP: rotation, the lost-response grace window
// (identity.RotationGrace) and reuse.
func TestIdentityHTTPRefresh(t *testing.T) {
	a := newIdentityAPI(t)
	_, c := a.signIn()
	const body = `{"app":"customer"}`

	for name, hdr := range map[string]map[string]string{
		"no origin":        refreshHeaders(c, ""),
		"foreign origin":   refreshHeaders(c, "https://evil.example"),
		"another app":      refreshHeaders(c, "https://business.todayz.localhost"),
		"no X-TZ-Client":   {"Origin": appOrigin, "Cookie": c.Name + "=" + c.Value},
		"public site only": refreshHeaders(c, "https://todayz.localhost"),
	} {
		w := a.do(http.MethodPost, "/v1/auth/refresh", body, hdr)
		require.Equal(t, http.StatusForbidden, w.Code, name)
		require.Empty(t, w.Header().Values("Set-Cookie"), "%s: a refused origin leaves the cookie", name)
	}

	w := a.do(http.MethodPost, "/v1/auth/refresh", body, refreshHeaders(c, appOrigin))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotEmpty(t, decode(t, w)["accessToken"])
	rotated := refreshCookieOf(t, w)
	require.NotEqual(t, c.Value, rotated.Value)

	// Replaying the first cookie at once, before the rotated one was used
	// (the response was lost): the lost response again, the same cookie
	// (D-239), and nothing rotates.
	w = a.do(http.MethodPost, "/v1/auth/refresh", body, refreshHeaders(c, appOrigin))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotEmpty(t, decode(t, w)["accessToken"])
	require.Equal(t, rotated.Value, refreshCookieOf(t, w).Value, "a lost response is replayed")
	w = a.do(http.MethodPost, "/v1/auth/refresh", body, refreshHeaders(rotated, appOrigin))
	require.Equal(t, http.StatusOK, w.Code, "a replay inside the grace window revokes nothing")
	latest := refreshCookieOf(t, w)

	// Once the rotated cookie is in use, the first one inside the window is
	// a 401 that leaves the cookie alone (it may already hold the newer
	// token), and the family lives on.
	w = a.do(http.MethodPost, "/v1/auth/refresh", body, refreshHeaders(c, appOrigin))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Empty(t, w.Header().Values("Set-Cookie"), "a grace retry keeps the cookie")

	// Past the window a replay is reuse: the family is revoked.
	a.clk.Advance(identity.RotationGrace + time.Second)
	w = a.do(http.MethodPost, "/v1/auth/refresh", body, refreshHeaders(c, appOrigin))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Negative(t, refreshCookieOf(t, w).MaxAge)
	w = a.do(http.MethodPost, "/v1/auth/refresh", body, refreshHeaders(latest, appOrigin))
	require.Equal(t, http.StatusUnauthorized, w.Code, "the newest cookie died with the family")

	// No cookie at all.
	w = a.do(http.MethodPost, "/v1/auth/refresh", body, refreshHeaders(nil, appOrigin))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// A client that still sends its expired access token on the cookie routes is
// treated as anonymous there (slot 11), so refresh works; elsewhere the
// expired bearer stays a 401.
func TestIdentityHTTPStaleBearer(t *testing.T) {
	a := newIdentityAPI(t)
	access, c := a.signIn()
	a.clk.Advance(authn.AccessTTL + time.Minute)
	stale := map[string]string{"Authorization": "Bearer " + access}
	require.Equal(t, http.StatusUnauthorized, a.do(http.MethodGet, "/v1/me", "", stale).Code, "expired")

	hdr := refreshHeaders(c, appOrigin)
	hdr["Authorization"] = "Bearer " + access
	w := a.do(http.MethodPost, "/v1/auth/refresh", `{"app":"customer"}`, hdr)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	fresh := decode(t, w)["accessToken"].(string)
	require.Equal(t, http.StatusOK, a.do(http.MethodGet, "/v1/me", "", map[string]string{"Authorization": "Bearer " + fresh}).Code)

	hdr = refreshHeaders(refreshCookieOf(t, w), appOrigin)
	hdr["Authorization"] = "Bearer junk"
	w = a.do(http.MethodPost, "/v1/auth/logout", `{"app":"customer"}`, hdr)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
}

func TestIdentityHTTPLogout(t *testing.T) {
	a := newIdentityAPI(t)
	access, c := a.signIn()
	auth := map[string]string{"Authorization": "Bearer " + access}
	require.Equal(t, http.StatusOK, a.do(http.MethodGet, "/v1/me", "", auth).Code)

	w := a.do(http.MethodPost, "/v1/auth/logout", `{"app":"customer"}`, refreshHeaders(c, "https://evil.example"))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, http.StatusOK, a.do(http.MethodGet, "/v1/me", "", auth).Code, "a forged logout changes nothing")

	w = a.do(http.MethodPost, "/v1/auth/logout", `{"app":"customer"}`, refreshHeaders(c, appOrigin))
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Negative(t, refreshCookieOf(t, w).MaxAge)
	require.Equal(t, http.StatusUnauthorized, a.do(http.MethodGet, "/v1/me", "", auth).Code, "the access token stops with its session")

	w = a.do(http.MethodPost, "/v1/auth/logout", `{"app":"customer"}`, refreshHeaders(nil, appOrigin))
	require.Equal(t, http.StatusUnauthorized, w.Code, "no cookie")
}

// /v1/me: 401 without a token, ETag, If-Match 412 and the missing-If-Match answer.
func TestIdentityHTTPMe(t *testing.T) {
	a := newIdentityAPI(t)
	w := a.do(http.MethodGet, "/v1/me", "", nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "unauthorized", decode(t, w)["code"])
	require.Equal(t, http.StatusUnauthorized, a.do(http.MethodGet, "/v1/me", "", map[string]string{"Authorization": "Bearer junk"}).Code)

	access, _ := a.signIn()
	auth := map[string]string{"Authorization": "Bearer " + access}
	w = a.do(http.MethodGet, "/v1/me", "", auth)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	me := decode(t, w)
	require.Equal(t, `"v1"`, w.Header().Get("ETag"))
	require.Equal(t, "90•••••007", me["phoneMasked"])
	require.NotContains(t, w.Body.String(), testPhone)
	require.Equal(t, "customer", me["app"])

	patch := func(ifMatch, body string) *httptest.ResponseRecorder {
		h := map[string]string{"Authorization": "Bearer " + access}
		if ifMatch != "" {
			h["If-Match"] = ifMatch
		}
		return a.do(http.MethodPatch, "/v1/me", body, h)
	}
	w = patch(`"v1"`, `{"name":"Asha","preferredLang":"kn"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, `"v2"`, w.Header().Get("ETag"))
	require.Equal(t, "Asha", decode(t, w)["name"])

	w = patch(`"v1"`, `{"preferredLang":"hi"}`)
	require.Equal(t, http.StatusPreconditionFailed, w.Code, "a stale If-Match")
	require.Equal(t, "precondition_failed", decode(t, w)["code"])

	// If-Match is optional in the contract, so a missing one reaches the
	// handler: 428 precondition_required.
	w = patch("", `{"preferredLang":"hi"}`)
	require.Equal(t, http.StatusPreconditionRequired, w.Code, w.Body.String())
	require.Equal(t, "precondition_required", decode(t, w)["code"])

	// A malformed one is still the validator's (the pattern).
	w = patch("v2", `{"preferredLang":"hi"}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	fields, _ := decode(t, w)["fields"].(map[string]any)
	require.Contains(t, fields, "header.If-Match", w.Body.String())

	w = patch(`"v2"`, `{"name":null}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Nil(t, decode(t, w)["name"], `"name": null clears the name`)

	w = patch(`"v3"`, `{"name":"   "}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())

	w = patch(`"v3"`, `{}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "nothing to change")
}

// The OTP limits answer 429 with Retry-After through the full pipeline, and a
// verify with the wrong code says how many attempts are left.
func TestIdentityHTTPLimits(t *testing.T) {
	a := newIdentityAPI(t)
	send := `{"phone":"` + testPhone + `","app":"customer","purpose":"login","turnstileToken":"XXXX.DUMMY.TOKEN.XXXX"}`
	w := a.do(http.MethodPost, "/v1/auth/otp/send", send, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	reqID := decode(t, w)["requestId"].(string)

	w = a.do(http.MethodPost, "/v1/auth/otp/send", send, nil)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Equal(t, "30", w.Header().Get("Retry-After"))
	require.Equal(t, "rate_limited", decode(t, w)["code"])

	verify := `{"app":"customer","requestId":"` + reqID + `","code":"123456","deviceId":"` + ids.New(a.clk.Now()).String() + `"}`
	w = a.do(http.MethodPost, "/v1/auth/otp/verify", verify, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	ext, _ := decode(t, w)["extensions"].(map[string]any)
	require.InDelta(t, 4, ext["attemptsLeft"], 0, w.Body.String())
	require.Empty(t, w.Header().Values("Set-Cookie"))

	w = a.do(http.MethodPost, "/v1/auth/otp/send", `{"phone":"9876543210","app":"customer","purpose":"login","turnstileToken":"XXXX.DUMMY.TOKEN.XXXX"}`, nil)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	require.NotContains(t, w.Body.String(), "9876543210", "a refusal never repeats the number")
}
