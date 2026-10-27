package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/ids"
)

// signInApp signs testPhone in to app over HTTP and returns the verify body
// and the refresh cookie.
func (a *identityAPI) signInApp(app string) (map[string]any, *http.Cookie) {
	a.t.Helper()
	w := a.do(http.MethodPost, "/v1/auth/otp/send", `{"phone":"`+testPhone+`","app":"`+app+`","purpose":"login","turnstileToken":"XXXX.DUMMY.TOKEN.XXXX"}`, nil)
	require.Equal(a.t, http.StatusOK, w.Code, w.Body.String())
	sent := decode(a.t, w)
	body := `{"app":"` + app + `","requestId":"` + sent["requestId"].(string) + `","code":"` + testOTP + `","deviceId":"` + ids.New(a.clk.Now()).String() + `"}`
	w = a.do(http.MethodPost, "/v1/auth/otp/verify", body, nil)
	require.Equal(a.t, http.StatusOK, w.Code, w.Body.String())
	a.clk.Advance(30 * time.Second)
	return decode(a.t, w), refreshCookieOf(a.t, w)
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

// AU-11/AU-12 over HTTP, through the real pipeline (D-264): the SMS code
// alone signs an admin in and reaches /v1/admin, W1-01's gate behind W1-11's
// admin-jobs API reads the live roles of the session, and the SMS step-up
// pair sends to the admin's own number and stamps the sign-in.
func TestIdentityHTTPAdminSMS(t *testing.T) {
	a := newIdentityAPI(t)
	cust, _ := a.signIn()
	me := decode(t, a.do(http.MethodGet, "/v1/me", "", bearer(cust)))
	_, err := a.db.OwnerPool.Exec(context.Background(), `INSERT INTO admin_users (user_id, roles) SELECT id, ARRAY['support'] FROM users WHERE id = $1`, ids.UUID(mustID(t, me["id"].(string))))
	require.NoError(t, err)

	out, _ := a.signInApp("admin")
	require.NotContains(t, out, "nextStep", "no second factor to set up")
	adm := out["accessToken"].(string)
	w := a.do(http.MethodGet, "/v1/admin/jobs", "", bearer(adm))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, http.StatusForbidden, a.do(http.MethodGet, "/v1/admin/jobs", "", bearer(cust)).Code, "a customer token never reaches /v1/admin")
	rum := `{"app":"admin","release":"admin@3f2a9c1","platform":"web","samples":[{"type":"vital","name":"LCP","value":900,"route":"/","t":1792660000000}]}`
	w = a.do(http.MethodPost, "/v1/rum", rum, bearer(adm))
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	// support may read but not act; the role is read from the session, so a
	// grant applies to the token already held.
	pause := func() int {
		return a.do(http.MethodPost, "/v1/admin/jobs/identity_purge/pause", `{"reason":"holding the purge"}`, bearer(adm)).Code
	}
	require.Equal(t, http.StatusForbidden, pause())
	_, err = a.db.OwnerPool.Exec(context.Background(), `UPDATE admin_users SET roles = ARRAY['admin']`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, pause())
	var actor string
	require.NoError(t, a.db.OwnerPool.QueryRow(context.Background(),
		`SELECT actor_id::text FROM audit_log WHERE action = 'jobs.pause'`).Scan(&actor))
	require.Equal(t, ids.UUID(mustID(t, me["id"].(string))).String(), actor, "the audit actor is the admin's user id")

	// The SMS step-up: only an admin sign-in may ask, the body names no number.
	require.Equal(t, http.StatusUnauthorized, a.do(http.MethodPost, "/v1/auth/step-up/send", "", nil).Code, "anonymous")
	require.Equal(t, http.StatusForbidden, a.do(http.MethodPost, "/v1/auth/step-up/send", "", bearer(cust)).Code, "a customer sign-in")
	w = a.do(http.MethodPost, "/v1/auth/step-up/send", "", bearer(adm))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	sent := decode(t, w)
	require.EqualValues(t, 30, sent["resendAfter"])
	reqID := sent["requestId"].(string)
	verify := func(tok, code string) *httptest.ResponseRecorder {
		return a.do(http.MethodPost, "/v1/auth/step-up/verify", `{"requestId":"`+reqID+`","code":"`+code+`"}`, bearer(tok))
	}
	w = verify(adm, "12345")
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "the contract refuses a malformed code")
	w = verify(adm, "999999")
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	require.Equal(t, "code_invalid", decode(t, w)["code"])
	require.Equal(t, http.StatusForbidden, verify(cust, testOTP).Code, "a customer sign-in")
	w = verify(adm, testOTP)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Empty(t, w.Header().Values("Set-Cookie"), "a step-up leaves the refresh cookie")
	until, err := time.Parse(time.RFC3339, decode(t, w)["validUntil"].(string))
	require.NoError(t, err)
	require.Equal(t, a.clk.Now().Add(authn.StepUpWindow).UTC(), until.UTC())
	w = verify(adm, testOTP)
	require.Equal(t, http.StatusUnauthorized, w.Code, "a code works once")
	require.Equal(t, "otp_expired", decode(t, w)["code"])
}

// AU-13 over HTTP: logout-all needs the app's origin and a live cookie,
// clears the cookie and ends every sign-in of the app.
func TestIdentityHTTPLogoutAll(t *testing.T) {
	a := newIdentityAPI(t)
	first, c1 := a.signIn()
	second, c2 := a.signIn()
	const body = `{"app":"customer"}`

	w := a.do(http.MethodPost, "/v1/auth/logout-all", body, refreshHeaders(c1, "https://evil.example"))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Empty(t, w.Header().Values("Set-Cookie"), "a forged request clears nothing")
	require.Equal(t, http.StatusOK, a.do(http.MethodGet, "/v1/me", "", bearer(first)).Code)

	w = a.do(http.MethodPost, "/v1/auth/logout-all", body, refreshHeaders(nil, appOrigin))
	require.Equal(t, http.StatusUnauthorized, w.Code, "no cookie")

	w = a.do(http.MethodPost, "/v1/auth/logout-all", body, refreshHeaders(c1, appOrigin))
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Negative(t, refreshCookieOf(t, w).MaxAge)
	require.Equal(t, http.StatusUnauthorized, a.do(http.MethodGet, "/v1/me", "", bearer(first)).Code)
	require.Equal(t, http.StatusUnauthorized, a.do(http.MethodGet, "/v1/me", "", bearer(second)).Code, "the other device too")
	w = a.do(http.MethodPost, "/v1/auth/refresh", body, refreshHeaders(c2, appOrigin))
	require.Equal(t, http.StatusUnauthorized, w.Code)

	w = a.do(http.MethodPost, "/v1/auth/logout-all", body, refreshHeaders(c1, appOrigin))
	require.Equal(t, http.StatusUnauthorized, w.Code, "a revoked cookie")
	require.Negative(t, refreshCookieOf(t, w).MaxAge, "and the dead cookie is cleared")
}

// AU-18 over HTTP: DELETE /v1/me with a step_up code.
func TestIdentityHTTPDeleteMe(t *testing.T) {
	a := newIdentityAPI(t)
	access, _ := a.signIn()
	w := a.do(http.MethodPost, "/v1/auth/otp/send", `{"phone":"`+testPhone+`","app":"customer","purpose":"step_up","turnstileToken":"XXXX.DUMMY.TOKEN.XXXX"}`, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	reqID := decode(t, w)["requestId"].(string)

	require.Equal(t, http.StatusUnauthorized, a.do(http.MethodDelete, "/v1/me", `{"otpRequestId":"`+reqID+`","code":"`+testOTP+`"}`, nil).Code)
	w = a.do(http.MethodDelete, "/v1/me", `{"otpRequestId":"`+reqID+`","code":"111111"}`, bearer(access))
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	p := decode(t, w)
	require.Equal(t, "code_invalid", p["code"])
	ext, _ := p["extensions"].(map[string]any)
	require.InDelta(t, identity.OTPMaxAttempts-1, ext["attemptsLeft"], 0, w.Body.String())

	w = a.do(http.MethodDelete, "/v1/me", `{"otpRequestId":"`+reqID+`","code":"`+testOTP+`"}`, bearer(access))
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Negative(t, refreshCookieOf(t, w).MaxAge)
	require.NotEqual(t, http.StatusOK, a.do(http.MethodGet, "/v1/me", "", bearer(access)).Code)
}

func mustID(t *testing.T, s string) ids.ID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}
