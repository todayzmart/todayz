package main

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/ids"
)

// W3-05 A-18: admin users. super_admin only; invite by phone, change roles,
// disable and re-enable; every change needs a fresh SMS step-up (D-264) and
// is audited. Nobody changes themselves.
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
	require.NotContains(t, me, "totpEnrolled", "D-264: there is no authenticator")
	require.NotNil(t, me["lastActiveAt"])
	require.Nil(t, me["createdBy"])

	// Invite a number that has never signed in: the account is made, roles sorted.
	r = post("/v1/admin/users", map[string]any{"phone": "90000 00031", "name": " Meera ", "roles": []string{"support", "catalogue"}, "reason": "joins support"})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	meera := r.body["id"].(string)
	require.Equal(t, "Meera", r.body["name"])
	require.Equal(t, []any{"catalogue", "support"}, r.body["roles"])
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

	// The invited person signs in with the SMS code alone (D-264).
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": "+919000000031", "app": "admin", "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(t, http.StatusOK, w.code, w.raw)
	w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"app": "admin", "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": ids.New(a.clk.Now()).String(),
	}, nil)
	require.Equal(t, http.StatusOK, w.code, w.raw)
	require.NotContains(t, w.body, "nextStep")
	require.NotEmpty(t, w.body["accessToken"])
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

	// Nobody changes themselves.
	r = patch("/v1/admin/users/"+self, map[string]any{"status": "disabled", "version": 1, "reason": "me"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	r = post("/v1/admin/users/"+self+"/reset-totp", map[string]any{"reason": "me"})
	require.Equal(t, http.StatusNotFound, r.code, "D-264: the authenticator reset is gone: %s", r.raw)

	// With two active super admins, one may be demoted. The last-one guard
	// cannot be reached over HTTP (the caller is always a second active
	// super admin); it stays as a backstop under the advisory lock.
	r = patch("/v1/admin/users/"+ravi, map[string]any{"roles": []string{"admin"}, "version": 1, "reason": "one is enough"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, []any{"admin"}, r.body["roles"])

	// Every change is audited with its reason; no audit row holds a phone.
	require.Equal(t, 2, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.invite' AND subject_type = 'admin_user'`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.disable' AND subject_id = $1 AND after ->> 'reason' = 'left the team'`, meera))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.enable' AND subject_id = $1`, meera))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM audit_log WHERE subject_type = 'admin_user' AND (before::text LIKE '%90000000%' OR after::text LIKE '%90000000%')`))

	// A stale step-up is asked again; reading needs none.
	a.clk.Advance(authn.StepUpWindow + time.Second)
	r = patch("/v1/admin/users/"+meera, map[string]any{"roles": []string{"analyst"}, "version": 4, "reason": "moves"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "step_up_required", problemCode(r))
	r = get("/v1/admin/users")
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

// A disabled admin's access token stops working on its next request, not when
// it expires; re-enabling does not bring the old session back.
func TestAdminUsersDisabledTokenHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "super_admin")
	tok := desk.token()
	a := o.a

	r := a.call(tok, http.MethodPost, "/v1/admin/users", map[string]any{"phone": "9000000041", "name": "Asha", "roles": []string{"support"}, "reason": "joins support"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	asha := r.body["id"].(string)
	// The invite is audited with the number masked, never in full.
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.invite' AND subject_id = $1 AND after ->> 'phone' = '90•••••041' AND (after ->> 'newAccount')::bool`, asha))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM audit_log WHERE after::text LIKE '%9000000041%' OR before::text LIKE '%9000000041%'`))

	// Asha signs in with the SMS code and can read orders.
	ashaTok, ashaCookie := adminSignIn(o, "+919000000041")
	r = a.call(ashaTok, http.MethodGet, "/v1/admin/orders", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// The super admin disables her; her very next request is refused.
	tok = desk.token() // a fresh step-up after the clock moved
	r = a.call(tok, http.MethodGet, "/v1/admin/users?status=active", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	var version any
	for _, it := range r.body["items"].([]any) {
		if m := it.(map[string]any); m["id"] == asha {
			version = m["version"]
		}
	}
	require.NotNil(t, version)
	r = a.call(tok, http.MethodPatch, "/v1/admin/users/"+asha, map[string]any{"status": "disabled", "version": version, "reason": "left the team"}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	version = r.body["version"]
	r = a.call(ashaTok, http.MethodGet, "/v1/admin/orders", nil, nil)
	require.Equal(t, http.StatusUnauthorized, r.code, r.raw)

	// Re-enabled, she has to sign in again: the old token stays dead.
	r = a.call(tok, http.MethodPatch, "/v1/admin/users/"+asha, map[string]any{"status": "active", "version": version, "reason": "back"}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = a.call(ashaTok, http.MethodGet, "/v1/admin/orders", nil, nil)
	require.Equal(t, http.StatusUnauthorized, r.code, r.raw)
	// Nor does her refresh token: every admin session stays revoked.
	r = adminRefresh(o, ashaCookie)
	require.Equal(t, http.StatusUnauthorized, r.code, r.raw)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, uuidOf(t, asha)))
}

// adminSignIn signs an invited number into the admin app with the SMS code
// alone (D-264), passes the SMS step-up, and returns the access token and the
// refresh cookie.
func adminSignIn(o *orderShop, phone string) (string, *http.Cookie) {
	t, a := o.t, o.a
	t.Helper()
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": phone, "app": "admin", "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(t, http.StatusOK, w.code, w.raw)
	w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"app": "admin", "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": ids.New(a.clk.Now()).String(),
	}, nil)
	require.Equal(t, http.StatusOK, w.code, w.raw)
	require.NotContains(t, w.body, "nextStep")
	tok := w.body["accessToken"].(string)
	a.stepUp(tok)
	return tok, cookieOf(t, w)
}

func adminRefresh(o *orderShop, c *http.Cookie) resp {
	o.t.Helper()
	return o.a.call("", http.MethodPost, "/v1/auth/refresh", map[string]string{"app": "admin"}, map[string]string{
		"Origin": "https://admin.todayz.localhost", "X-TZ-Client": "admin-users-test/1.0", "Cookie": c.Name + "=" + c.Value,
	})
}

// adminVersions reads every admin's version from A-18.
func adminVersions(o *orderShop, tok string) map[string]any {
	o.t.Helper()
	r := o.a.call(tok, http.MethodGet, "/v1/admin/users", nil, nil)
	require.Equal(o.t, http.StatusOK, r.code, r.raw)
	out := map[string]any{}
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		out[m["id"].(string)] = m["version"]
	}
	return out
}

// Two super_admins disable each other at the same moment: the advisory lock
// orders them, the second finds itself disabled (403, or 401 if its request
// had not yet passed the session check), and one active super_admin remains.
func TestAdminUsersMutualDisableHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "super_admin")
	a := o.a
	tokA := desk.token()
	r := a.call(tokA, http.MethodPost, "/v1/admin/users", map[string]any{"phone": "9000000051", "name": "Kiran", "roles": []string{"super_admin"}, "reason": "second super admin"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	idB := r.body["id"].(string)
	tokB, _ := adminSignIn(o, "+919000000051")
	tokA = desk.token()
	vs := adminVersions(o, tokA)
	var idA string
	for id := range vs {
		if id != idB {
			idA = id
		}
	}

	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i, c := range []struct{ tok, target string }{{tokA, idB}, {tokB, idA}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = a.call(c.tok, http.MethodPatch, "/v1/admin/users/"+c.target, map[string]any{"status": "disabled", "version": vs[c.target], "reason": "race"}, nil).code
		}()
	}
	wg.Wait()
	ok, refused := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusForbidden, http.StatusUnauthorized:
			refused++
		}
	}
	require.Equal(t, 1, ok, "codes %v", codes)
	require.Equal(t, 1, refused, "codes %v", codes)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_users au JOIN users u ON u.id = au.user_id
		WHERE au.disabled_at IS NULL AND 'super_admin' = ANY (au.roles) AND u.status = 'active'`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'admin_user.disable'`))
}

// A super_admin who is disabled, demoted or blocked while their request is
// already past authz makes no change: the actor is read again under the
// advisory lock. The test holds that lock, changes the actor, then lets the
// request through.
func TestAdminUsersActorRecheckHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "super_admin")
	a := o.a
	tok := desk.token()
	r := a.call(tok, http.MethodPost, "/v1/admin/users", map[string]any{"phone": "9000000061", "name": "Dev", "roles": []string{"support"}, "reason": "joins"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	dev := r.body["id"].(string)
	var self string
	for id := range adminVersions(o, tok) {
		if id != dev {
			self = id
		}
	}
	ctx := context.Background()
	waiting := func() int {
		return a.count(`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`)
	}
	restore := func() {
		a.exec(`UPDATE admin_users SET disabled_at = NULL, roles = ARRAY['super_admin'] WHERE user_id = $1`, uuidOf(t, self))
		a.exec(`UPDATE users SET status = 'active' WHERE id = $1`, uuidOf(t, self))
	}
	before := a.count(`SELECT count(*) FROM audit_log WHERE subject_type = 'admin_user'`)
	for _, c := range []struct {
		name, change string
		send         func() resp
	}{
		{"invite, actor disabled", `UPDATE admin_users SET disabled_at = now() WHERE user_id = $1`, func() resp {
			return a.call(tok, http.MethodPost, "/v1/admin/users", map[string]any{"phone": "9000000062", "name": "Late", "roles": []string{"support"}, "reason": "late"}, nil)
		}},
		{"update, actor demoted", `UPDATE admin_users SET roles = ARRAY['admin'] WHERE user_id = $1`, func() resp {
			return a.call(tok, http.MethodPatch, "/v1/admin/users/"+dev, map[string]any{"status": "disabled", "version": 1, "reason": "late"}, nil)
		}},
		{"roles, actor blocked", `UPDATE users SET status = 'blocked' WHERE id = $1`, func() resp {
			return a.call(tok, http.MethodPatch, "/v1/admin/users/"+dev, map[string]any{"roles": []string{"catalogue"}, "version": 1, "reason": "late"}, nil)
		}},
	} {
		tx, err := a.tdb.OwnerPool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('admin:admin_users', 0))`)
		require.NoError(t, err)
		done := make(chan resp, 1)
		go func() { done <- c.send() }()
		deadline := time.Now().Add(10 * time.Second)
		for waiting() == 0 {
			require.True(t, time.Now().Before(deadline), "%s: the request never reached the lock", c.name)
			time.Sleep(10 * time.Millisecond)
		}
		_, err = tx.Exec(ctx, c.change, uuidOf(t, self))
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		got := <-done
		require.Equal(t, http.StatusForbidden, got.code, "%s: %s", c.name, got.raw)
		restore()
	}
	require.Equal(t, before, a.count(`SELECT count(*) FROM audit_log WHERE subject_type = 'admin_user'`))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM phone_identities pi JOIN users u ON u.id = pi.user_id WHERE u.name = 'Late'`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_users WHERE user_id = $1 AND disabled_at IS NULL`, uuidOf(t, dev)))
}
