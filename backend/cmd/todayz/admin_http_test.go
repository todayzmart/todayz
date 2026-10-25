package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// W3-05 A-06: exact phone search, detail, strike reset (support), block,
// unblock and session revoke (admin), each audited; the phone only masked.
func TestAdminCustomersHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	a := o.a
	get := func(tk, path string) resp { t.Helper(); return a.call(tk, http.MethodGet, path, nil, nil) }
	post := func(tk, path string, body any) resp { t.Helper(); return a.call(tk, http.MethodPost, path, body, nil) }
	// The phone travels in the body, never in a URL (W3-05 review).
	search := func(tk, phone string) resp {
		t.Helper()
		return post(tk, "/v1/admin/customer-search", map[string]any{"phone": phone})
	}

	me := get(o.cust, "/v1/me")
	require.Equal(t, http.StatusOK, me.code, me.raw)
	cid := me.body["id"].(string)
	base := "/v1/admin/customers/" + cid

	// Search: exact phone in the forms an operator types; never the whole number back.
	for _, q := range []string{custPhone, strings.TrimPrefix(custPhone, "+91"), "+91 " + custPhone[3:8] + " " + custPhone[8:]} {
		r := search(tok, q)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		items := r.body["items"].([]any)
		require.Len(t, items, 1, q)
		it := items[0].(map[string]any)
		require.Equal(t, cid, it["id"])
		require.Equal(t, pii.MaskPhone(custPhone), it["phoneMasked"])
		require.NotContains(t, r.raw, custPhone[3:])
	}
	r := search(tok, "9000000099")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])
	r = search(tok, "12345")
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = get(tok, "/v1/admin/customers?phone="+url.QueryEscape(custPhone))
	require.Equal(t, http.StatusNotFound, r.code, r.raw, "the old GET search is gone")

	r = get(tok, base)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "active", r.body["status"])
	require.Equal(t, false, r.body["isAdmin"])
	require.Equal(t, true, r.body["isCustomer"])
	require.NotEmpty(t, r.body["sessions"])
	require.NotContains(t, r.raw, custPhone[3:])
	r = get(tok, "/v1/admin/customers/"+ids.New(a.clk.Now()).String())
	require.Equal(t, http.StatusNotFound, r.code, r.raw)

	// Strike reset (support may): every active strike, with a reason, recounted.
	for range 2 {
		a.exec(`INSERT INTO strikes (id, subject_type, subject_id, reason, created_at) VALUES ($1, 'customer', $2, 'no_show', $3)`,
			uuidOf(t, ids.New(a.clk.Now()).String()), uuidOf(t, cid), a.clk.Now())
	}
	a.exec(`UPDATE customer_profiles SET strikes_count_30d = 2 WHERE user_id = $1`, uuidOf(t, cid))
	reason := map[string]any{"reason": "no-shows were a shop mix-up"}
	r = post(tok, base+"/strikes/reset", map[string]any{})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	// A phone in a reason would sit in the audit log, shown to other roles.
	for _, bad := range []string{"customer called from 9876543210", "ring +91 98765-43210 back", "her number is 098765 43210"} {
		r = post(tok, base+"/strikes/reset", map[string]any{"reason": bad})
		require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
		require.Contains(t, fieldsOf(r), "body.reason")
		require.NotContains(t, r.raw, "43210")
	}
	r = post(tok, base+"/strikes/reset", reason)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 0, r.body["activeStrikes"])
	require.Equal(t, "no-shows were a shop mix-up", r.body["strikes"].([]any)[0].(map[string]any)["resetReason"])
	require.Equal(t, 0, a.count(`SELECT strikes_count_30d FROM customer_profiles WHERE user_id = $1`, uuidOf(t, cid)))
	r = post(tok, base+"/strikes/reset", reason)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))

	// Support may not block.
	block := map[string]any{"reason": "fake orders from this number"}
	r = post(tok, base+"/block", block)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	// Admin blocks: every session revoked, the customer refused at once.
	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	r = post(tok, base+"/block", block)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "blocked", r.body["status"])
	require.Empty(t, r.body["sessions"])
	r = get(o.cust, "/v1/me")
	require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, r.code, r.raw)
	r = post(tok, base+"/block", block)
	require.Equal(t, http.StatusConflict, r.code, r.raw)

	// An admin account is not changed here (A-18 disables it): not blocked, its
	// strikes not reset, its sessions neither listed nor revoked.
	r = search(tok, adminPhone)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	adminID := r.body["items"].([]any)[0].(map[string]any)["id"].(string)
	adminBase := "/v1/admin/customers/" + adminID
	r = post(tok, adminBase+"/block", block)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = get(tok, adminBase)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, true, r.body["isAdmin"])
	require.Empty(t, r.body["sessions"])
	var adminSession pgtype.UUID
	require.NoError(t, a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT id FROM sessions WHERE user_id = $1 AND revoked_at IS NULL LIMIT 1`, uuidOf(t, adminID)).Scan(&adminSession))
	asid, _ := ids.FromUUID(adminSession)
	d := a.call(tok, http.MethodDelete, adminBase+"/sessions/"+asid.String(), nil, nil)
	require.Equal(t, http.StatusConflict, d.code, d.raw)
	a.exec(`INSERT INTO strikes (id, subject_type, subject_id, reason, created_at) VALUES ($1, 'customer', $2, 'no_show', $3)`,
		uuidOf(t, ids.New(a.clk.Now()).String()), uuidOf(t, adminID), a.clk.Now())
	r = post(tok, adminBase+"/strikes/reset", reason)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, 0, a.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_reason = 'admin'`, uuidOf(t, adminID)))

	// Block is for customers only: a shop owner with no customer profile is 409.
	r = search(tok, ownerPhone)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	ownerID := r.body["items"].([]any)[0].(map[string]any)["id"].(string)
	a.exec(`DELETE FROM customer_profiles WHERE user_id = $1`, uuidOf(t, ownerID))
	r = get(tok, "/v1/admin/customers/"+ownerID)
	require.Equal(t, false, r.body["isCustomer"])
	r = post(tok, "/v1/admin/customers/"+ownerID+"/block", block)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM users WHERE id = $1 AND status = 'blocked'`, uuidOf(t, ownerID)))

	r = post(tok, base+"/unblock", map[string]any{"reason": "spoke to the customer"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "active", r.body["status"])

	// Session revoke: one device, then 404.
	var again string
	o.login(&again, custPhone, "customer")
	r = get(tok, base)
	sessions := r.body["sessions"].([]any)
	require.Len(t, sessions, 1)
	sid := sessions[0].(map[string]any)["id"].(string)
	d = a.call(tok, http.MethodDelete, base+"/sessions/"+sid, nil, nil)
	require.Equal(t, http.StatusNoContent, d.code, d.raw)
	d = a.call(tok, http.MethodDelete, base+"/sessions/"+sid, nil, nil)
	require.Equal(t, http.StatusNotFound, d.code, d.raw)

	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'customer.strikes_reset' AND subject_type = 'user' AND subject_id = $1`, cid))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'customer.block' AND subject_id = $1
	  AND before->>'status' = 'active' AND after->>'reason' = 'fake orders from this number'`, cid))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'customer.unblock' AND subject_id = $1`, cid))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'customer.session_revoke' AND subject_id = $1`, cid))

	// The audit log and the customer's side panel (identity's sign-in rows
	// are on the same subject), paged newest first.
	total := a.count(`SELECT count(*) FROM audit_log WHERE subject_type = 'user' AND subject_id = $1`, cid)
	require.Greater(t, total, 4)
	r = get(tok, "/v1/admin/audit-log?subjectType=user&subjectId="+cid+"&limit=2")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 2)
	first := r.body["items"].([]any)[0].(map[string]any)
	require.Equal(t, "customer.session_revoke", first["action"], "newest first")
	seen := 2
	for cur, _ := r.body["nextCursor"].(string); cur != ""; cur, _ = r.body["nextCursor"].(string) {
		r = get(tok, "/v1/admin/audit-log?subjectType=user&subjectId="+cid+"&limit=2&cursor="+url.QueryEscape(cur))
		require.Equal(t, http.StatusOK, r.code, r.raw)
		seen += len(r.body["items"].([]any))
	}
	require.Equal(t, total, seen)
	r = get(tok, "/v1/admin/audit-log/subject?subjectType=user&subjectId="+cid+"&limit=100")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], total)

	// Support sees the panel, not the whole log; finance sees neither for a customer.
	a.exec(`UPDATE admin_users SET roles = ARRAY['support']`)
	r = get(tok, "/v1/admin/audit-log/subject?subjectType=user&subjectId="+cid)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = get(tok, "/v1/admin/audit-log")
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = get(tok, "/v1/admin/audit-log/subject?subjectType=platform_setting&subjectId=no_show_strikes")
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	a.exec(`UPDATE admin_users SET roles = ARRAY['finance']`)
	r = get(tok, "/v1/admin/audit-log/subject?subjectType=user&subjectId="+cid)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = get(tok, base)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	// Not an admin at all.
	for _, tk := range []string{o.cust, o.owner} {
		r = get(tk, base)
		require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, r.code, r.raw)
	}
}

// W3-05 A-07: order search and detail with the timeline; never the code.
func TestAdminOrdersReadHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	a := o.a
	id := o.order()
	r := o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	other := o.order()

	r = a.call(tok, http.MethodGet, "/v1/admin/orders?storeId="+o.sid, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 2)
	require.Equal(t, other, r.body["items"].([]any)[0].(map[string]any)["id"], "newest first")

	r = a.call(tok, http.MethodGet, "/v1/admin/orders?status=accepted", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	num := r.body["items"].([]any)[0].(map[string]any)["number"].(string)
	r = a.call(tok, http.MethodGet, "/v1/admin/orders?number="+url.QueryEscape(num), nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.Equal(t, id, r.body["items"].([]any)[0].(map[string]any)["id"])

	r = a.call(tok, http.MethodGet, "/v1/admin/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "accepted", r.body["status"])
	require.NotEmpty(t, r.body["items"])
	require.GreaterOrEqual(t, len(r.body["events"].([]any)), 2, "placed and accepted")
	require.NotContains(t, r.raw, `"code"`)
	r = a.call(tok, http.MethodGet, "/v1/admin/orders/"+ids.New(a.clk.Now()).String(), nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)

	a.exec(`UPDATE admin_users SET roles = ARRAY['catalogue']`)
	r = a.call(tok, http.MethodGet, "/v1/admin/orders/"+id, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
}

// W3-05 A-17: settings list with groups and provider key states, a change
// needs a fresh step-up and a reason, money keys are super_admin only, the
// value is checked against its unit and range, the current value is a no-op,
// each change is audited and listed in the key's history, and the settings
// cache reads the new value.
func TestAdminSettingsHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	a := o.a
	get := func(path string) resp { t.Helper(); return a.call(tok, http.MethodGet, path, nil, nil) }
	patch := func(key string, body any) resp {
		t.Helper()
		return a.call(tok, http.MethodPatch, "/v1/admin/settings/"+key, body, nil)
	}
	change := func(v any) map[string]any { return map[string]any{"value": v, "reason": "W3-05 test"} }

	// Support sees no settings.
	r := get("/v1/admin/settings")
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	r = get("/v1/admin/settings")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	byKey := map[string]map[string]any{}
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		byKey[m["key"].(string)] = m
	}
	require.Equal(t, a.count(`SELECT count(*) FROM platform_settings`), len(byKey))
	cm := byKey["credit_max_per_order"]
	require.Equal(t, "credits", cm["group"])
	require.Equal(t, true, cm["money"])
	require.Equal(t, false, cm["editable"], "an admin may not change a money setting")
	require.Equal(t, true, byKey["ask_max_stores"]["editable"])
	require.Equal(t, "asks", byKey["ask_max_stores"]["group"])
	require.Equal(t, "clients", byKey["min_client_version_admin"]["group"])
	require.Equal(t, "orders", r.body["items"].([]any)[0].(map[string]any)["group"], "01 §17 order of groups")
	names := []string{}
	for _, p := range r.body["providers"].([]any) {
		names = append(names, p.(map[string]any)["name"].(string))
	}
	require.Contains(t, names, "ANTHROPIC_API_KEY")

	// Admin: a money key is refused; a plain key within range is changed.
	r = patch("credit_max_per_order", change(40))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "forbidden", problemCode(r))
	r = patch("ask_max_stores", map[string]any{"value": 9})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "a reason is required")
	for _, bad := range []any{0, 21, 8.5, "9", true} {
		r = patch("ask_max_stores", change(bad))
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "%v: %s", bad, r.raw)
		require.Contains(t, r.raw, "body.value")
	}
	r = patch("feature_voice_enabled", change(1))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = patch("min_client_version_admin", change("latest"))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = patch("no_such_setting", change(1))
	require.Equal(t, http.StatusNotFound, r.code, r.raw)

	r = patch("ask_max_stores", change(9))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	s := r.body["setting"].(map[string]any)
	require.EqualValues(t, 9, s["value"])
	require.EqualValues(t, 2, s["version"])
	require.NotNil(t, s["updatedBy"])
	o.settings.Invalidate()
	n, err := o.settings.Int(t.Context(), "ask_max_stores")
	require.NoError(t, err)
	require.EqualValues(t, 9, n, "the cache reads the new value")

	// The current value again changes nothing and is not audited.
	r = patch("ask_max_stores", change(9))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 2, r.body["setting"].(map[string]any)["version"])
	r = patch("min_client_version_admin", change(" 1.4.0 "))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "1.4.0", r.body["setting"].(map[string]any)["value"])
	r = patch("feature_voice_enabled", change(false))
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// A stale step-up is asked again; a step_up verify re-confirms it.
	a.clk.Advance(authn.StepUpWindow + time.Second)
	r = patch("ask_max_stores", change(10))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "step_up_required", problemCode(r))
	r = get("/v1/admin/settings")
	require.Equal(t, http.StatusOK, r.code, "reading needs no step-up")
	a.clk.Advance(31 * time.Second)
	v := a.call(tok, http.MethodPost, "/v1/auth/totp/verify", map[string]string{"code": identity.TOTPCode(desk.secret, a.clk.Now()), "purpose": "step_up"}, nil)
	require.Equal(t, http.StatusOK, v.code, v.raw)
	r = patch("ask_max_stores", change(10))
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// super_admin changes the money key.
	a.exec(`UPDATE admin_users SET roles = ARRAY['super_admin']`)
	r = get("/v1/admin/settings")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	for _, it := range r.body["items"].([]any) {
		require.Equal(t, true, it.(map[string]any)["editable"])
	}
	r = patch("credit_max_per_order", change(40))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 40, r.body["setting"].(map[string]any)["value"])

	// Every number has a real range (30503), and values outside it, or past what the unit can
	// carry, are refused even for a super_admin (W3-05 review).
	require.Zero(t, a.count(`SELECT count(*) FROM platform_settings WHERE unit NOT IN ('bool','text') AND (min IS NULL OR max IS NULL)`),
		"every numeric key has a min and a max")
	require.Zero(t, a.count(`SELECT count(*) FROM platform_settings WHERE unit IN ('minutes','hours','days') AND min < 1`),
		"no timer can be 0")
	for key, bad := range map[string][]any{
		"admin_adjust_dual_approval_above_paise": {float64(1 << 53), 500001, -1},
		"ref_customer_customer_referee_credits":  {9e15, 1001},
		"order_accept_timeout_min":               {0, 61, 200000000},
		"ai_daily_cost_cap_paise":                {float64(1<<31) + 1},
		"ai_model_fast":                          {"claude-haiku-5", "gpt-5"},
	} {
		for _, v := range bad {
			r = patch(key, change(v))
			require.Equal(t, http.StatusUnprocessableEntity, r.code, "%s=%v: %s", key, v, r.raw)
			require.Contains(t, r.raw, "body.value")
		}
	}
	r = patch("ai_model_fast", change("claude-sonnet-5-5"))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 0, a.count(`SELECT count(*) FROM audit_log WHERE subject_id = 'order_accept_timeout_min'`), "a refused change is not audited")

	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'settings.update' AND subject_type = 'platform_setting'
	  AND subject_id = 'credit_max_per_order' AND before->>'value' = '30' AND after->>'value' = '40' AND after->>'reason' = 'W3-05 test'`))
	require.Equal(t, 2, a.count(`SELECT count(*) FROM audit_log WHERE action = 'settings.update' AND subject_id = 'ask_max_stores'`))

	// History: newest first, paged, with the actor's name and the reason.
	r = get("/v1/admin/settings/ask_max_stores/history?limit=1")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.Len(t, items, 1)
	h := items[0].(map[string]any)
	require.EqualValues(t, 9, h["from"])
	require.EqualValues(t, 10, h["to"])
	require.Equal(t, "W3-05 test", h["reason"])
	cur := r.body["nextCursor"].(string)
	r = get("/v1/admin/settings/ask_max_stores/history?limit=1&cursor=" + url.QueryEscape(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 8, r.body["items"].([]any)[0].(map[string]any)["from"])
	require.Nil(t, r.body["nextCursor"])
	r = get("/v1/admin/settings/no_such_setting/history")
	require.Equal(t, http.StatusNotFound, r.code, r.raw)

	// The audit side panel shows the same rows to an admin.
	r = get("/v1/admin/audit-log/subject?subjectType=platform_setting&subjectId=ask_max_stores")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 2)

	// A stale step-up is asked again for a money key too, even of a super_admin (a fresh sign-in,
	// so the access token outlives the step-up window).
	tok = desk.token()
	a.clk.Advance(authn.StepUpWindow + time.Second)
	r = patch("credit_max_per_order", change(41))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "step_up_required", problemCode(r))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM platform_settings WHERE key = 'credit_max_per_order' AND value = '40'`))
}
