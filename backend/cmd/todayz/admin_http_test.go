package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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

	me := get(o.cust, "/v1/me")
	require.Equal(t, http.StatusOK, me.code, me.raw)
	cid := me.body["id"].(string)
	base := "/v1/admin/customers/" + cid

	// Search: exact phone in the forms an operator types; never the whole number back.
	for _, q := range []string{custPhone, strings.TrimPrefix(custPhone, "+91"), "+91 " + custPhone[3:8] + " " + custPhone[8:]} {
		r := get(tok, "/v1/admin/customers?phone="+url.QueryEscape(q))
		require.Equal(t, http.StatusOK, r.code, r.raw)
		items := r.body["items"].([]any)
		require.Len(t, items, 1, q)
		it := items[0].(map[string]any)
		require.Equal(t, cid, it["id"])
		require.Equal(t, pii.MaskPhone(custPhone), it["phoneMasked"])
		require.NotContains(t, r.raw, custPhone[3:])
	}
	r := get(tok, "/v1/admin/customers?phone=9000000099")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])
	r = get(tok, "/v1/admin/customers?phone=12345")
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)

	r = get(tok, base)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "active", r.body["status"])
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

	// An admin account is not blocked here (A-18 disables it).
	r = get(tok, "/v1/admin/customers?phone="+url.QueryEscape(adminPhone))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	adminID := r.body["items"].([]any)[0].(map[string]any)["id"].(string)
	r = post(tok, "/v1/admin/customers/"+adminID+"/block", block)
	require.Equal(t, http.StatusConflict, r.code, r.raw)

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
	d := a.call(tok, http.MethodDelete, base+"/sessions/"+sid, nil, nil)
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
