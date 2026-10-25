package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// W3-05 A-03: the verification queue (support reads, admin moves), KYC masked
// with an audited reveal (no-store), document review; a verify puts the
// stores waiting for review live in the same change.
func TestAdminBusinessesHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	a := o.a
	get := func(path string) resp { t.Helper(); return a.call(tok, http.MethodGet, path, nil, nil) }
	post := func(path string, body any) resp { t.Helper(); return a.call(tok, http.MethodPost, path, body, nil) }

	bid, sid := a.newBusiness(o.owner, "Ravi Kirana")
	B := "/v1/biz/businesses/" + bid
	cur := a.call(o.owner, http.MethodGet, B+"/kyc", nil, nil)
	r := a.call(o.owner, http.MethodPut, B+"/kyc", map[string]any{
		"pan": "AAPFU0939F", "gstin": "27AAPFU0939F1ZV", "upiId": "ravikirana@okaxis",
		"bankAccount": "001234567890", "bankIfsc": "HDFC0001234", "legalName": "Ravi Kirana LLP",
	}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	a.exec(`UPDATE businesses SET status = 'submitted', submitted_at = $1 WHERE id = $2`, a.clk.Now(), uuidOf(t, bid))
	a.exec(`UPDATE stores SET status = 'pending_review' WHERE id = $1`, uuidOf(t, sid))
	doc := ids.New(a.clk.Now()).String()
	a.exec(`INSERT INTO business_documents (id, business_id, kind, object_key, content_type, size_bytes, status)
	  VALUES ($1, $2, 'pan', 'k', 'application/pdf', 1234, 'uploaded')`, uuidOf(t, doc), uuidOf(t, bid))

	// The queue, filtered by status in the URL.
	r = get("/v1/admin/businesses?status=submitted")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, bid, items[0].(map[string]any)["id"])
	require.EqualValues(t, 1, items[0].(map[string]any)["storeCount"])

	// Detail: KYC last 4 only, staff phones masked.
	base := "/v1/admin/businesses/" + bid
	r = get(base)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	kyc := r.body["kyc"].(map[string]any)
	require.Equal(t, "submitted", kyc["status"])
	require.Equal(t, "939F", kyc["panLast4"])
	require.NotContains(t, r.raw, "AAPFU0939F")
	require.NotContains(t, r.raw, "001234567890")
	require.NotContains(t, r.raw, ownerPhone[3:])
	staff := r.body["staff"].([]any)
	require.Equal(t, pii.MaskPhone(ownerPhone), staff[0].(map[string]any)["phoneMasked"])
	require.Len(t, r.body["documents"], 1)

	// Support reads but cannot move or reveal.
	verify := map[string]any{"note": "documents match"}
	r = post(base+"/verify", verify)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = post(base+"/kyc/reveal", map[string]any{"field": "pan"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)

	// Reveal: one field in full, never stored, audited by field name only.
	r = post(base+"/kyc/reveal", map[string]any{"field": "pan"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "AAPFU0939F", r.body["value"])
	require.Equal(t, "no-store", r.hdr.Get("Cache-Control"))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'business.kyc_reveal' AND subject_id = $1
	  AND after->>'field' = 'pan' AND after::text NOT LIKE '%AAPFU%'`, bid))

	// Document review: reject needs a reason; a correction may accept it.
	D := base + "/documents/" + doc + "/review"
	r = post(D, map[string]any{"decision": "rejected"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post(D, map[string]any{"decision": "rejected", "reason": "blurred"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "blurred", r.body["documents"].([]any)[0].(map[string]any)["rejectReason"])
	r = post(D, map[string]any{"decision": "rejected", "reason": "blurred"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = post(D, map[string]any{"decision": "accepted"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Nil(t, r.body["documents"].([]any)[0].(map[string]any)["rejectReason"])

	// KYC review: reject needs a note; verified after a fix.
	r = post(base+"/kyc/review", map[string]any{"decision": "rejected"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post(base+"/kyc/review", map[string]any{"decision": "rejected", "note": "PAN name differs"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "rejected", r.body["kyc"].(map[string]any)["status"])
	r = post(base+"/kyc/review", map[string]any{"decision": "verified"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "verified", r.body["kyc"].(map[string]any)["status"])
	r = post(base+"/kyc/review", map[string]any{"decision": "verified"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)

	// Request info, then (after the owner resubmits) verify: the waiting store goes live.
	r = post(base+"/request-info", map[string]any{"fields": []string{"pan", "hours"}, "note": "Upload a clear PAN"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "needs_info", r.body["status"])
	require.Equal(t, []any{"pan", "hours"}, r.body["needsInfoFields"])
	r = post(base+"/verify", verify)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	a.exec(`UPDATE businesses SET status = 'submitted' WHERE id = $1`, uuidOf(t, bid))
	r = post(base+"/verify", verify)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "verified", r.body["status"])
	require.Empty(t, r.body["needsInfoFields"])
	require.Equal(t, "live", r.body["stores"].([]any)[0].(map[string]any)["status"])

	// Suspend and reinstate, each with a reason.
	r = post(base+"/suspend", map[string]any{})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post(base+"/suspend", map[string]any{"reason": "fake listings"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "suspended", r.body["status"])
	r = post(base+"/reject", map[string]any{"reason": "x"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = post(base+"/reinstate", map[string]any{"reason": "owner fixed the listings"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "verified", r.body["status"])

	for _, act := range []string{"business.document_review", "business.kyc_review", "business.request_info", "business.verify", "business.suspend", "business.reinstate"} {
		require.Positive(t, a.count(`SELECT count(*) FROM audit_log WHERE action = $1 AND subject_type = 'business' AND subject_id = $2`, act, bid), act)
	}
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'business.verify' AND subject_id = $1 AND after->'storesLive' ? $2`, bid, sid))
	r = get("/v1/admin/audit-log/subject?subjectType=business&subjectId=" + bid + "&limit=100")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.NotEmpty(t, r.body["items"])
	r = get("/v1/admin/businesses/" + ids.New(a.clk.Now()).String())
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
}

// W3-05 A-05: list and detail (support), strike reset (support), review,
// pin and hours, force-close and reopen (admin), each audited.
func TestAdminStoresHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	a := o.a
	get := func(path string) resp { t.Helper(); return a.call(tok, http.MethodGet, path, nil, nil) }
	call := func(method, path string, body any) resp { t.Helper(); return a.call(tok, method, path, body, nil) }

	r := get("/v1/admin/stores?status=live")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 3)

	base := "/v1/admin/stores/" + o.sid
	r = get(base)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "live", r.body["status"])
	require.NotEmpty(t, r.body["hours"].(map[string]any)["weekly"])

	for range 2 {
		a.exec(`INSERT INTO strikes (id, subject_type, subject_id, reason, created_at) VALUES ($1, 'store', $2, 'dispatch_timeout', $3)`,
			uuidOf(t, ids.New(a.clk.Now()).String()), uuidOf(t, o.sid), a.clk.Now())
	}
	r = get(base)
	require.EqualValues(t, 2, r.body["activeStrikes"])
	r = call(http.MethodPost, base+"/strikes/reset", map[string]any{"reason": "dispatch outage on our side"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 0, r.body["activeStrikes"])
	r = call(http.MethodPost, base+"/strikes/reset", map[string]any{"reason": "again"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)

	until := a.clk.Now().Add(6 * time.Hour).UTC().Format(time.RFC3339)
	r = call(http.MethodPost, base+"/force-close", map[string]any{"reason": "flood", "until": until})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)

	r = call(http.MethodPost, base+"/force-close", map[string]any{"reason": "flood", "until": a.clk.Now().Add(-time.Hour).UTC().Format(time.RFC3339)})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = call(http.MethodPost, base+"/force-close", map[string]any{"reason": "flood", "until": until})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.NotNil(t, r.body["closedUntil"])
	r = call(http.MethodPost, base+"/reopen", map[string]any{"reason": "water cleared"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Nil(t, r.body["closedUntil"])
	r = call(http.MethodPost, base+"/reopen", map[string]any{"reason": "again"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)

	// Pin and hours: the hours rules are internal/business's.
	r = call(http.MethodPatch, base, map[string]any{"reason": "nothing"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	bad := map[string]any{"weekly": map[string]any{"mon": []map[string]string{{"open": "08:00", "close": "12:00"}, {"open": "11:00", "close": "14:00"}}}, "holidays": []any{}}
	r = call(http.MethodPatch, base, map[string]any{"reason": "owner asked", "hours": bad})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, r.body["fields"], "body.hours.weekly.mon[1]")
	hours := map[string]any{"weekly": map[string]any{"mon": []map[string]string{{"open": "08:00", "close": "20:00"}}}, "holidays": []any{}}
	r = call(http.MethodPatch, base, map[string]any{"reason": "owner asked on the phone", "hours": hours, "location": map[string]float64{"lat": 12.93, "lng": 77.58}})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, map[string]any{"lat": 12.93, "lng": 77.58}, r.body["location"])
	require.Equal(t, "08:00", r.body["hours"].(map[string]any)["weekly"].(map[string]any)["mon"].([]any)[0].(map[string]any)["open"])
	require.Empty(t, r.body["hours"].(map[string]any)["weekly"].(map[string]any)["tue"])

	// Review: send back with a note, approve only at a verified business.
	a.exec(`UPDATE stores SET status = 'pending_review' WHERE id = $1`, uuidOf(t, o.sid))
	r = call(http.MethodPost, base+"/review", map[string]any{"decision": "send_back"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = call(http.MethodPost, base+"/review", map[string]any{"decision": "send_back", "note": "Add a storefront photo"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "draft", r.body["status"])
	require.Equal(t, "Add a storefront photo", r.body["reviewNote"])
	r = call(http.MethodPost, base+"/review", map[string]any{"decision": "approve"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	a.exec(`UPDATE stores SET status = 'pending_review' WHERE id = $1`, uuidOf(t, o.sid))
	a.exec(`UPDATE businesses SET status = 'submitted' WHERE id = (SELECT business_id FROM stores WHERE id = $1)`, uuidOf(t, o.sid))
	r = call(http.MethodPost, base+"/review", map[string]any{"decision": "approve"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	a.exec(`UPDATE businesses SET status = 'verified' WHERE id = (SELECT business_id FROM stores WHERE id = $1)`, uuidOf(t, o.sid))
	r = call(http.MethodPost, base+"/review", map[string]any{"decision": "approve"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "live", r.body["status"])
	require.Nil(t, r.body["reviewNote"])

	for _, act := range []string{"store.strikes_reset", "store.force_close", "store.reopen", "store.update", "store.send_back", "store.approve"} {
		require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = $1 AND subject_type = 'store' AND subject_id = $2`, act, o.sid), act)
	}
}

// W3-05 A-03 Leads tab: field agents list leads (phone masked) and move them.
func TestAdminLeadsHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "field_agent")
	tok := desk.token()
	a := o.a
	const leadPhone = "+919000000041"
	r := a.call("", http.MethodPost, "/v1/public/leads", map[string]any{
		"name": "Meena", "phone": leadPhone, "shopName": "Meena Stores", "city": "Bengaluru",
		"source": "site:home:en", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(t, http.StatusAccepted, r.code, r.raw)

	r = a.call(tok, http.MethodGet, "/v1/admin/leads?status=new", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.Len(t, items, 1)
	lead := items[0].(map[string]any)
	require.Equal(t, pii.MaskPhone(leadPhone), lead["phoneMasked"])
	require.NotContains(t, r.raw, leadPhone[3:])
	id := lead["id"].(string)

	S := "/v1/admin/leads/" + id + "/status"
	r = a.call(tok, http.MethodPost, S, map[string]any{"status": "new"}, nil)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = a.call(tok, http.MethodPost, S, map[string]any{"status": "contacted", "note": "visit on Monday"}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "contacted", r.body["status"])
	require.NotNil(t, r.body["statusChangedAt"])
	r = a.call(tok, http.MethodGet, "/v1/admin/leads?status=new", nil, nil)
	require.Empty(t, r.body["items"])
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'lead.status' AND subject_type = 'lead' AND subject_id = $1
	  AND before->>'status' = 'new' AND after->>'note' = 'visit on Monday'`, id))

	// A role without leads (catalogue) is refused.
	a.exec(`UPDATE admin_users SET roles = ARRAY['catalogue']`)
	r = a.call(tok, http.MethodGet, "/v1/admin/leads", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
}
