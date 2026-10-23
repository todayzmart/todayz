package main

import (
	"context"
	"net/http"
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

// AU-11/AU-12 over HTTP, through the real pipeline: slot 11 keeps an
// OTP-only admin token on /v1/auth, enrolment and verify turn it into
// otp+totp, and W1-01's gate behind W1-11's admin-jobs API reads the live
// roles of the session.
func TestIdentityHTTPAdminTOTP(t *testing.T) {
	a := newIdentityAPI(t)
	cust, _ := a.signIn()
	me := decode(t, a.do(http.MethodGet, "/v1/me", "", bearer(cust)))
	_, err := a.db.OwnerPool.Exec(context.Background(), `INSERT INTO admin_users (user_id, roles) SELECT id, ARRAY['support'] FROM users WHERE id = $1`, ids.UUID(mustID(t, me["id"].(string))))
	require.NoError(t, err)

	out, otpCookie := a.signInApp("admin")
	require.Equal(t, "totp_enrol", out["nextStep"])
	otpOnly := out["accessToken"].(string)
	w := a.do(http.MethodGet, "/v1/admin/jobs", "", bearer(otpOnly))
	require.Equal(t, http.StatusForbidden, w.Code, "an OTP-only admin token never reaches /v1/admin")
	require.Equal(t, http.StatusForbidden, a.do(http.MethodGet, "/v1/admin/jobs", "", bearer(cust)).Code, "nor a customer token")

	w = a.do(http.MethodPost, "/v1/auth/totp/enrol", "", bearer(otpOnly))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	enr := decode(t, w)
	require.Len(t, enr["recoveryCodes"], 10)
	secret, err := identity.DecodeTOTPSecret(enr["secret"].(string))
	require.NoError(t, err)

	w = a.do(http.MethodPost, "/v1/auth/totp/verify", `{"code":"`+identity.TOTPCode(secret, a.clk.Now())+`","purpose":"login"}`, bearer(otpOnly))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	// L9: the login verify rotates the sign-in and sets the new cookie.
	rotated := refreshCookieOf(t, w)
	require.Equal(t, otpCookie.Name, rotated.Name)
	require.Equal(t, "/v1/auth", rotated.Path)
	require.True(t, rotated.HttpOnly)
	require.Positive(t, rotated.MaxAge)
	require.NotEqual(t, otpCookie.Value, rotated.Value, "a new refresh token")
	full := decode(t, w)["accessToken"].(string)

	w = a.do(http.MethodGet, "/v1/admin/jobs", "", bearer(full))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// support may read but not act; the role is read from the session, so a
	// grant applies to the token already held.
	pause := func() int {
		return a.do(http.MethodPost, "/v1/admin/jobs/identity_purge/pause", `{"reason":"holding the purge"}`, bearer(full)).Code
	}
	require.Equal(t, http.StatusForbidden, pause())
	_, err = a.db.OwnerPool.Exec(context.Background(), `UPDATE admin_users SET roles = ARRAY['admin']`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, pause())
	var actor string
	require.NoError(t, a.db.OwnerPool.QueryRow(context.Background(),
		`SELECT actor_id::text FROM audit_log WHERE action = 'jobs.pause'`).Scan(&actor))
	require.Equal(t, ids.UUID(mustID(t, me["id"].(string))).String(), actor, "the audit actor is the admin's user id")

	// Step-up: a later verify with purpose step_up re-confirms the session.
	a.clk.Advance(authn.StepUpWindow + time.Second)
	w = a.do(http.MethodPost, "/v1/auth/totp/verify", `{"code":"`+identity.TOTPCode(secret, a.clk.Now())+`","purpose":"step_up"}`, bearer(full))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Empty(t, w.Header().Values("Set-Cookie"), "step_up leaves the cookie")
	w = a.do(http.MethodPost, "/v1/auth/totp/verify", `{"code":"12345","purpose":"login"}`, bearer(full))
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "the contract refuses a malformed code")
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
