package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/ids"
)

// W3-05 A-18: admin users. super_admin only; invite by phone, change roles,
// disable and re-enable, reset the authenticator; every change needs a fresh
// step-up and is audited. Nobody changes themselves.
func TestAdminUsersHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "admin")
	tok := desk.token()
	a := o.a
	get := func(path string) resp { t.Helper(); return a.call(tok, http.MethodGet, path, nil, nil) }
	post := func(path string, body any) resp { t.Helper(); return a.call(tok, http.MethodPost, path, body, nil) }
	patch := func(path string, body any) resp { t.Helper(); return a.call(tok, http.MethodPatch, path, body, nil) }
	adminSessions := func(id string) int {
		return a.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND app = 'admin' AND revoked_at IS NULL`, uuidOf(t, id))
	}

	// admin is not enough: A-18 is super_admin only.
	r := get("/v1/admin/users")
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	a.exec(`UPDATE admin_users SET roles = ARRAY['super_admin']`)
	r = get("/v1/admin/users")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	me := r.body["items"].([]any)[0].(map[string]any)
	self := me["id"].(string)
	require.Equal(t, "90•••••021", me["phoneMasked"])
	require.Equal(t, true, me["totpEnrolled"])
	require.NotNil(t, me["lastActiveAt"])
	require.Nil(t, me["createdBy"])

	// Invite a number that has never signed in: the account is made, roles sorted.
	r = post("/v1/admin/users", map[string]any{"phone": "90000 00031", "name": " Meera ", "roles": []string{"support", "catalogue"}, "reason": "joins support"})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	meera := r.body["id"].(string)
	require.Equal(t, "Meera", r.body["name"])
	require.Equal(t, []any{"catalogue", "support"}, r.body["roles"])
	require.Equal(t, false, r.body["totpEnrolled"])
	require.Equal(t, self, r.body["createdBy"])
	require.Equal(t, "90•••••031", r.body["phoneMasked"])
	require.Nil(t, r.body["lastActiveAt"])
	r = post("/v1/admin/users", map[string]any{"phone": "+919000000031", "name": "Meera", "roles": []string{"support"}, "reason": "again"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "conflict", problemCode(r))

	// A number that is already a customer keeps its account and its name.
	var cust string
	o.login(&cust, "+919000000032", "customer")
	cm := a.call(cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, cm.code, cm.raw)
	a.exec(`UPDATE users SET name = 'Ravi K' WHERE id = $1`, uuidOf(t, cm.body["id"].(string)))
	r = post("/v1/admin/users", map[string]any{"phone": "9000000032", "name": "Ravi", "roles": []string{"super_admin"}, "reason": "second super admin"})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	ravi := r.body["id"].(string)
	require.Equal(t, cm.body["id"], ravi)
	require.Equal(t, "Ravi K", r.body["name"])

	// Bad input names the field.
	r = post("/v1/admin/users", map[string]any{"phone": "12345", "name": "X", "roles": []string{"support"}, "reason": "x"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post("/v1/admin/users", map[string]any{"phone": "9000000033", "name": "X", "roles": []string{}, "reason": "x"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post("/v1/admin/users", map[string]any{"phone": "9000000033", "name": "X", "roles": []string{"owner"}, "reason": "x"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM phone_identities pi JOIN users u ON u.id = pi.user_id WHERE u.name = 'X'`))

	// The invited person signs in and is asked to set up the authenticator.
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": "+919000000031", "app": "admin", "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(t, http.StatusOK, w.code, w.raw)
	w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"app": "admin", "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": ids.New(a.clk.Now()).String(),
	}, nil)
	require.Equal(t, http.StatusOK, w.code, w.raw)
	require.Equal(t, identity.NextStepTOTPEnrol, w.body["nextStep"])
	require.Equal(t, 1, adminSessions(meera))

	// Roles change with the version read; a stale one is a conflict.
	r = patch("/v1/admin/users/"+meera, map[string]any{"roles": []string{"support"}, "version": 1, "reason": "catalogue moved"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, []any{"support"}, r.body["roles"])
	require.EqualValues(t, 2, r.body["version"])
	r = patch("/v1/admin/users/"+meera, map[string]any{"status": "disabled", "version": 1, "reason": "left"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = patch("/v1/admin/users/"+meera, map[string]any{"roles": []string{"support"}, "version": 2, "reason": "same"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))

	// Disable ends the person's admin sessions; the list moves it to disabled.
	r = patch("/v1/admin/users/"+meera, map[string]any{"status": "disabled", "version": 2, "reason": "left the team"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "disabled", r.body["status"])
	require.NotNil(t, r.body["disabledAt"])
	require.Equal(t, 0, adminSessions(meera))
	r = get("/v1/admin/users?status=disabled")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	r = get("/v1/admin/users?limit=1")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.NotEmpty(t, r.body["nextCursor"])
	first := r.body["items"].([]any)[0].(map[string]any)["id"]
	r = get("/v1/admin/users?limit=1&cursor=" + r.body["nextCursor"].(string))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.ElementsMatch(t, []any{self, ravi}, []any{first, r.body["items"].([]any)[0].(map[string]any)["id"]})
	r = patch("/v1/admin/users/"+meera, map[string]any{"status": "active", "version": 3, "reason": "back"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "active", r.body["status"])

	// Nobody changes or resets themselves.
	r = patch("/v1/admin/users/"+self, map[string]any{"status": "disabled", "version": 1, "reason": "me"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	r = post("/v1/admin/users/"+self+"/reset-totp", map[string]any{"reason": "me"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)

	// With two active super admins, one may be demoted. The last-one guard
	// cannot be reached over HTTP (the caller is always a second active
	// super admin); it stays as a backstop under the advisory lock.
	r = patch("/v1/admin/users/"+ravi, map[string]any{"roles": []string{"admin"}, "version": 1, "reason": "one is enough"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, []any{"admin"}, r.body["roles"])

	// Reset clears the authenticator and recovery codes and ends the sessions.
	a.exec(`UPDATE admin_users SET totp_secret_enc = '\x00', totp_enabled_at = now(), totp_failures = 2 WHERE user_id = $1`, uuidOf(t, meera))
	a.exec(`INSERT INTO admin_recovery_codes (id, admin_id, code_hash) VALUES ($1, $2, decode(repeat('ab', 32), 'hex'))`, uuidOf(t, ids.New(a.clk.Now()).String()), uuidOf(t, meera))
	a.exec(`INSERT INTO sessions (id, user_id, app, family_id, refresh_hash, expires_at, last_used_at, created_at)
		VALUES ($1, $2, 'admin', $1, decode(repeat('cd', 32), 'hex'), now() + interval '1 day', now(), now())`, uuidOf(t, ids.New(a.clk.Now()).String()), uuidOf(t, meera))
	r = post("/v1/admin/users/"+meera+"/reset-totp", map[string]any{"reason": "lost her phone"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, false, r.body["totpEnrolled"])
	require.Equal(t, 0, a.count(`SELECT count(*) FROM admin_recovery_codes WHERE admin_id = $1`, uuidOf(t, meera)))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM admin_users WHERE user_id = $1 AND (totp_secret_enc IS NOT NULL OR totp_failures <> 0)`, uuidOf(t, meera)))
	require.Equal(t, 0, adminSessions(meera))

	// Every change is audited with its reason; no audit row holds a phone.
	require.Equal(t, 2, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.invite' AND subject_type = 'admin_user'`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.disable' AND subject_id = $1 AND after ->> 'reason' = 'left the team'`, meera))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.enable' AND subject_id = $1`, meera))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.totp_reset' AND subject_id = $1 AND (after ->> 'sessionsRevoked')::int = 1`, meera))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM audit_log WHERE subject_type = 'admin_user' AND (before::text LIKE '%90000000%' OR after::text LIKE '%90000000%')`))

	// A stale step-up is asked again; reading needs none.
	a.clk.Advance(authn.StepUpWindow + time.Second)
	r = patch("/v1/admin/users/"+meera, map[string]any{"roles": []string{"analyst"}, "version": 5, "reason": "moves"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "step_up_required", problemCode(r))
	r = get("/v1/admin/users")
	require.Equal(t, http.StatusOK, r.code, r.raw)
}
