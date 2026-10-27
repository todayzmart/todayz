package main

import (
	"net/http"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/ids"
)

// adminSession is one admin sign-in that adminToken keeps per phone.
type adminSession struct {
	cookie   *http.Cookie
	signedAt time.Time
	tok      string
	stepUpAt time.Time
}

// adminToken returns an access token of phone (a reserved test number with
// an admin_users row) that passes a † action now: an admin-app sign-in with
// the SMS OTP alone (D-264) and a fresh SMS step-up.
//
// The sign-in is kept and refreshed on later calls (a new one only when the
// old one has ended), so a call costs one SMS (the step-up), as the old
// TOTP sign-in did, and the number's five sends an hour last as long. Each
// send waits out the resend gap first, which moves the clock on 31 s.
func (a *bizAPI) adminToken(phone, userAgent string) string {
	a.t.Helper()
	if a.admins == nil {
		a.admins = map[string]*adminSession{}
	}
	s := a.admins[phone]
	if s != nil && s.stepUpAt.Equal(a.clk.Now()) {
		return s.tok
	}
	if s != nil && a.clk.Now().Before(s.signedAt.Add(authn.AdminRefreshTTL-authn.AccessTTL)) {
		w := a.call("", http.MethodPost, "/v1/auth/refresh", map[string]string{"app": "admin"}, map[string]string{
			"Origin": "https://admin.todayz.localhost", "X-TZ-Client": userAgent + "/1.0", "Cookie": s.cookie.Name + "=" + s.cookie.Value,
		})
		if w.code == http.StatusOK {
			s.tok, s.cookie = w.body["accessToken"].(string), cookieOf(a.t, w)
		} else {
			s = nil // disabled, signed out or revoked meanwhile: sign in again
		}
	} else {
		s = nil
	}
	if s == nil {
		a.clk.Advance(31 * time.Second)
		w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
			"phone": phone, "app": "admin", "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
		}, nil)
		require.Equal(a.t, http.StatusOK, w.code, "otp send: %d %s", w.code, w.raw)
		w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
			"app": "admin", "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": ids.New(a.clk.Now()).String(),
		}, map[string]string{"User-Agent": userAgent})
		require.Equal(a.t, http.StatusOK, w.code, "otp verify: %d", w.code)
		require.NotContains(a.t, w.body, "nextStep", "D-264: the SMS code alone signs an admin in")
		s = &adminSession{cookie: cookieOf(a.t, w), signedAt: a.clk.Now(), tok: w.body["accessToken"].(string)}
		a.admins[phone] = s
	}
	a.stepUp(s.tok)
	s.stepUpAt = a.clk.Now()
	return s.tok
}

// stepUp passes the admin SMS step-up (D-264) on tok: a fresh code to the
// admin's own number, then the reserved test code.
func (a *bizAPI) stepUp(tok string) {
	a.t.Helper()
	a.clk.Advance(31 * time.Second) // past the resend gap
	s := a.call(tok, http.MethodPost, "/v1/auth/step-up/send", nil, nil)
	require.Equal(a.t, http.StatusOK, s.code, "step-up send: %d %s", s.code, s.raw)
	v := a.call(tok, http.MethodPost, "/v1/auth/step-up/verify", map[string]string{
		"requestId": s.body["requestId"].(string), "code": testOTP,
	}, nil)
	require.Equal(a.t, http.StatusOK, v.code, "step-up verify: %d %s", v.code, v.raw)
}
