package main

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/ids"
)

// D-264 review (founder, 27 Oct: "Lock after 10 wrong"): over HTTP, the
// tenth wrong admin sign-in code answers 429 signin_locked with Retry-After,
// A-18 shows the lock, and another super admin lifts it with a step-up; the
// unlock is audited, never of oneself and never of an admin who is not locked.
func TestAdminSignInLockHTTP(t *testing.T) {
	const meeraPhone = "+919000000031"
	o := newOrderShop(t)
	desk := newAdminDesk(o, "super_admin")
	tok := desk.token()
	a := o.a
	send := func() resp {
		t.Helper()
		a.clk.Advance(31 * time.Second)
		return a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
			"phone": meeraPhone, "app": "admin", "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
		}, nil)
	}
	verify := func(reqID, code string) resp {
		t.Helper()
		return a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
			"app": "admin", "requestId": reqID, "code": code, "deviceId": ids.New(a.clk.Now()).String(),
		}, nil)
	}
	unlock := func(tok, id string, body any) resp {
		t.Helper()
		return a.call(tok, http.MethodPost, "/v1/admin/users/"+id+"/unlock-sign-in", body, nil)
	}

	r := a.call(tok, http.MethodPost, "/v1/admin/users", map[string]any{"phone": meeraPhone, "name": "Meera", "roles": []string{"support"}, "reason": "joins support"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	meera := r.body["id"].(string)
	require.Contains(t, r.body, "signInLockedUntil")
	require.Nil(t, r.body["signInLockedUntil"])
	self := a.call(tok, http.MethodGet, "/v1/admin/users", nil, nil).body["items"].([]any)
	var selfID string
	for _, it := range self {
		if m := it.(map[string]any); m["id"] != meera {
			selfID = m["id"].(string)
		}
	}
	require.NotEmpty(t, selfID)

	r = unlock(tok, meera, map[string]any{"reason": "nothing to unlock"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r), "an admin who is not locked")

	// Ten wrong codes over two requests (five tries each).
	var last resp
	for range 2 {
		s := send()
		require.Equal(t, http.StatusOK, s.code, s.raw)
		for range 5 {
			last = verify(s.body["requestId"].(string), "111111")
		}
	}
	require.Equal(t, http.StatusTooManyRequests, last.code, last.raw)
	require.Equal(t, "signin_locked", problemCode(last))
	require.Equal(t, strconv.Itoa(int((24 * time.Hour).Seconds())), last.hdr.Get("Retry-After"))
	lockedAt := a.clk.Now().UTC()
	s := send()
	require.Equal(t, http.StatusTooManyRequests, s.code, s.raw)
	require.Equal(t, "signin_locked", problemCode(s), "no code is sent while locked")

	// A-18 shows the lock on Meera only.
	r = a.call(tok, http.MethodGet, "/v1/admin/users", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		if m["id"] == meera {
			until, err := time.Parse(time.RFC3339, m["signInLockedUntil"].(string))
			require.NoError(t, err)
			require.Equal(t, lockedAt.Add(24*time.Hour), until.UTC())
		} else {
			require.Nil(t, m["signInLockedUntil"])
		}
	}

	// The unlock: a † action with a reason; never one's own.
	r = unlock(tok, selfID, map[string]any{"reason": "me"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	r = unlock(tok, meera, map[string]any{})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = unlock(tok, ids.New(a.clk.Now()).String(), map[string]any{"reason": "who"})
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	a.clk.Advance(authn.StepUpWindow + time.Second)
	r = unlock(tok, meera, map[string]any{"reason": "called from the office line"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "step_up_required", problemCode(r))
	tok = desk.token()
	r = unlock(tok, meera, map[string]any{"reason": "called from the office line"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Nil(t, r.body["signInLockedUntil"])
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.unlock_sign_in' AND subject_id = $1
		AND after ->> 'reason' = 'called from the office line' AND before ->> 'signInLockedUntil' IS NOT NULL`, meera))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM audit_log WHERE subject_type = 'admin_user' AND (before::text LIKE '%90000000%' OR after::text LIKE '%90000000%')`))
	r = unlock(tok, meera, map[string]any{"reason": "again"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)

	// Meera signs in straight away.
	s = send()
	require.Equal(t, http.StatusOK, s.code, s.raw)
	v := verify(s.body["requestId"].(string), testOTP)
	require.Equal(t, http.StatusOK, v.code, v.raw)
}

// The SMS step-up routes take the bearer token only: the refresh cookie of an
// admin sign-in, with the admin app's origin, is not a caller (CSRF), and no
// code is sent.
func TestAdminStepUpIgnoresCookieHTTP(t *testing.T) {
	a := newIdentityAPI(t)
	cust, _ := a.signIn()
	me := decode(t, a.do(http.MethodGet, "/v1/me", "", bearer(cust)))
	_, err := a.db.OwnerPool.Exec(context.Background(), `INSERT INTO admin_users (user_id, roles) SELECT id, ARRAY['super_admin'] FROM users WHERE id = $1`, ids.UUID(mustID(t, me["id"].(string))))
	require.NoError(t, err)
	out, cookie := a.signInApp("admin")
	require.NotEmpty(t, out["accessToken"])
	require.NotNil(t, cookie)

	hdr := map[string]string{
		"Origin": "https://admin.todayz.localhost", "X-TZ-Client": "admin-pwa/1.0",
		"Cookie": cookie.Name + "=" + cookie.Value,
	}
	w := a.do(http.MethodPost, "/v1/auth/step-up/send", "", hdr)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	w = a.do(http.MethodPost, "/v1/auth/step-up/verify", `{"requestId":"`+ids.New(a.clk.Now()).String()+`","code":"`+testOTP+`"}`, hdr)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	var n int
	require.NoError(t, a.db.OwnerPool.QueryRow(context.Background(), `SELECT count(*) FROM otp_requests WHERE purpose = 'step_up'`).Scan(&n))
	require.Zero(t, n, "no step-up code was sent")
	var stamped int
	require.NoError(t, a.db.OwnerPool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE step_up_at IS NOT NULL`).Scan(&stamped))
	require.Zero(t, stamped)

	// The same call with the bearer works.
	w = a.do(http.MethodPost, "/v1/auth/step-up/send", "", bearer(out["accessToken"].(string)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// D-264 second review: the unlock is super_admin only (a plain admin gets 403
// and the lock stays), and unlocking a disabled admin is allowed: it lifts
// the lock and clears the count, but lets nobody in, since sign-in still
// refuses a disabled admin; enabled again later, they find no stale lock.
func TestAdminSignInUnlockEdgesHTTP(t *testing.T) {
	const meeraPhone = "+919000000032"
	o := newOrderShop(t)
	desk := newAdminDesk(o, "super_admin")
	tok := desk.token()
	a := o.a
	send := func() resp {
		t.Helper()
		a.clk.Advance(31 * time.Second)
		return a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
			"phone": meeraPhone, "app": "admin", "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
		}, nil)
	}
	verify := func(reqID, code string) resp {
		t.Helper()
		return a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
			"app": "admin", "requestId": reqID, "code": code, "deviceId": ids.New(a.clk.Now()).String(),
		}, nil)
	}
	unlock := func(id string) resp {
		t.Helper()
		return a.call(tok, http.MethodPost, "/v1/admin/users/"+id+"/unlock-sign-in", map[string]any{"reason": "called from the office line"}, nil)
	}
	locks := func() int {
		t.Helper()
		return a.count(`SELECT count(*) FROM admin_signin_locks`)
	}

	r := a.call(tok, http.MethodPost, "/v1/admin/users", map[string]any{"phone": meeraPhone, "name": "Meera", "roles": []string{"support"}, "reason": "joins support"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	meera := r.body["id"].(string)
	r = a.call(tok, http.MethodPatch, "/v1/admin/users/"+meera, map[string]any{"status": "disabled", "version": 1, "reason": "left the team"}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// A disabled admin's number locks like any other (the lock tells no one
	// whether the number is an admin's, AU-06).
	var last resp
	for range 2 {
		s := send()
		require.Equal(t, http.StatusOK, s.code, s.raw)
		for range 5 {
			last = verify(s.body["requestId"].(string), "111111")
		}
	}
	require.Equal(t, "signin_locked", problemCode(last), last.raw)
	require.Equal(t, 1, locks())
	r = a.call(tok, http.MethodGet, "/v1/admin/users?status=disabled", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.NotNil(t, r.body["items"].([]any)[0].(map[string]any)["signInLockedUntil"], "A-18 shows the lock of a disabled admin")

	// A plain admin may not unlock: 403, and the lock stays.
	a.exec(`UPDATE admin_users SET roles = ARRAY['admin'] WHERE user_id <> $1`, uuidOf(t, meera))
	r = unlock(meera)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "forbidden", problemCode(r))
	require.Equal(t, 1, locks())
	require.Zero(t, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.unlock_sign_in'`))

	// A super_admin unlocks the disabled admin; sign-in still refuses them.
	a.exec(`UPDATE admin_users SET roles = ARRAY['super_admin'] WHERE user_id <> $1`, uuidOf(t, meera))
	r = unlock(meera)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "disabled", r.body["status"])
	require.Nil(t, r.body["signInLockedUntil"])
	require.Zero(t, locks())
	require.Zero(t, a.count(`SELECT count(*) FROM admin_signin_failures`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.unlock_sign_in' AND subject_id = $1`, meera))
	s := send()
	require.Equal(t, http.StatusOK, s.code, s.raw)
	v := verify(s.body["requestId"].(string), testOTP)
	require.Equal(t, http.StatusForbidden, v.code, v.raw)
	require.Equal(t, "forbidden", problemCode(v), "a disabled admin still cannot sign in")
}

// D-264 second review: the four access settings (the admin sign-in and
// step-up limits) are super_admin only, like the money settings, and each
// key's own min and max hold on the PATCH path, not only in the table.
func TestAdminAccessSettingsHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "admin")
	tok := desk.token()
	a := o.a
	keys := []string{"admin_signin_max_failures", "admin_signin_failure_window_hours", "admin_signin_lock_hours", "admin_step_up_sends_per_hour"}
	change := func(v any) map[string]any { return map[string]any{"value": v, "reason": "D-264 test"} }
	patch := func(key string, v any) resp {
		t.Helper()
		return a.call(tok, http.MethodPatch, "/v1/admin/settings/"+key, change(v), nil)
	}
	list := func() map[string]map[string]any {
		t.Helper()
		r := a.call(tok, http.MethodGet, "/v1/admin/settings", nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		by := map[string]map[string]any{}
		for _, it := range r.body["items"].([]any) {
			m := it.(map[string]any)
			by[m["key"].(string)] = m
		}
		return by
	}

	by := list()
	for _, k := range keys {
		require.Equal(t, "access", by[k]["group"], k)
		require.Equal(t, false, by[k]["money"], k)
		require.Equal(t, true, by[k]["superAdminOnly"], k)
		require.Equal(t, false, by[k]["editable"], "%s: an admin may not change it", k)
		r := patch(k, 12)
		require.Equal(t, http.StatusForbidden, r.code, "%s: %s", k, r.raw)
		require.Equal(t, "forbidden", problemCode(r), k)
	}
	require.Equal(t, false, by["ask_max_stores"]["superAdminOnly"])
	require.Equal(t, true, by["credit_max_per_order"]["superAdminOnly"])
	require.Equal(t, true, by["ask_max_stores"]["editable"], "other keys stay with admin")
	require.Zero(t, a.count(`SELECT count(*) FROM audit_log WHERE action = 'settings.update' AND subject_id = ANY($1)`, keys))

	a.exec(`UPDATE admin_users SET roles = ARRAY['super_admin']`)
	by = list()
	for _, k := range keys {
		require.Equal(t, true, by[k]["editable"], k)
	}
	for _, tc := range []struct {
		key  string
		bad  []any
		good int
	}{
		{"admin_signin_max_failures", []any{2, 51, 0, 10.5, "10", true}, 12},
		{"admin_signin_failure_window_hours", []any{0, 169, -1}, 48},
		{"admin_signin_lock_hours", []any{0, 169}, 12},
		{"admin_step_up_sends_per_hour", []any{0, 61}, 20},
	} {
		for _, v := range tc.bad {
			r := patch(tc.key, v)
			require.Equal(t, http.StatusUnprocessableEntity, r.code, "%s=%v: %s", tc.key, v, r.raw)
			require.Contains(t, r.raw, "body.value")
		}
		r := patch(tc.key, tc.good)
		require.Equal(t, http.StatusOK, r.code, "%s: %s", tc.key, r.raw)
		require.EqualValues(t, tc.good, r.body["setting"].(map[string]any)["value"])
		require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'settings.update' AND subject_id = $1`, tc.key))
	}
}
