package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// W3-05 A-20: banner moderation. support and up clear a held upload or take a
// banner down with a reason the shop sees; both are audited.
func TestAdminBannersHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "catalogue")
	tok := desk.token()
	a := o.a
	get := func(path string) resp { t.Helper(); return a.call(tok, http.MethodGet, path, nil, nil) }
	post := func(path string, body any) resp { t.Helper(); return a.call(tok, http.MethodPost, path, body, nil) }
	now := a.clk.Now()
	banner := func(headline, status, moderation string, starts time.Time, ends *time.Time) string {
		id := ids.New(a.clk.Now()).String()
		a.exec(`INSERT INTO store_banners (id, store_id, template, headline, target_type, starts_at, ends_at, status, moderation)
			VALUES ($1, $2, '{"tint":"green","subline":"Fresh today"}', $3, 'store', $4, $5, $6, $7)`,
			uuidOf(t, id), uuidOf(t, o.sid), headline, starts, ends, status, moderation)
		a.clk.Advance(time.Second)
		return id
	}
	past := now.Add(-time.Hour)
	held := banner("Diwali sweets", "pending_review", "needs_review", past, nil)
	later := banner("Weekend atta sale", "pending_review", "needs_review", now.Add(48*time.Hour), nil)
	lapsed := banner("Yesterday only", "pending_review", "needs_review", now.Add(-3*time.Hour), &past)
	live := banner("Fresh milk daily", "live", "auto_ok", past, nil)

	// The catalogue role does not moderate banners; support does.
	r := get("/v1/admin/banners")
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	a.exec(`UPDATE admin_users SET roles = ARRAY['support']`)

	r = get("/v1/admin/banners?limit=2")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.Len(t, items, 2)
	first := items[0].(map[string]any)
	require.Equal(t, held, first["id"], "the review queue is oldest first")
	require.Equal(t, "Lakshmi Traders", first["storeName"])
	require.Equal(t, "green", first["template"].(map[string]any)["tint"])
	require.Nil(t, first["imageKey"])
	require.Nil(t, first["targetLabel"])
	r = get("/v1/admin/banners?limit=2&cursor=" + r.body["nextCursor"].(string))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.Equal(t, lapsed, r.body["items"].([]any)[0].(map[string]any)["id"])
	r = get("/v1/admin/banners?state=live")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	r = get("/v1/admin/banners?storeId=" + ids.New(a.clk.Now()).String())
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])

	// Approve: live from its start, scheduled before it, ended once past its end.
	r = post("/v1/admin/banners/"+held+"/approve", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "live", r.body["status"])
	require.Equal(t, "auto_ok", r.body["moderation"])
	r = post("/v1/admin/banners/"+later+"/approve", map[string]any{"reason": "the photo is the shop's own"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "scheduled", r.body["status"])
	r = post("/v1/admin/banners/"+lapsed+"/approve", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "ended", r.body["status"])
	r = post("/v1/admin/banners/"+held+"/approve", map[string]any{})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	r = post("/v1/admin/banners/"+ids.New(a.clk.Now()).String()+"/approve", map[string]any{})
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = get("/v1/admin/banners?state=live")
	require.Len(t, r.body["items"], 3, "the two cleared and the one that passed on its own")
	r = get("/v1/admin/banners")
	require.Empty(t, r.body["items"])

	// Takedown: a reason is required; it is shown to the shop; once is enough.
	r = post("/v1/admin/banners/"+live+"/takedown", map[string]any{})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post("/v1/admin/banners/"+live+"/takedown", map[string]any{"reason": "   "})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post("/v1/admin/banners/"+live+"/takedown", map[string]any{"reason": "Uses another brand's logo"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "removed", r.body["status"])
	require.Equal(t, "Uses another brand's logo", r.body["removeReason"])
	require.NotNil(t, r.body["removedAt"])
	r = post("/v1/admin/banners/"+live+"/takedown", map[string]any{"reason": "again"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	r = post("/v1/admin/banners/"+lapsed+"/takedown", map[string]any{"reason": "already over"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = get("/v1/admin/banners?state=removed")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.Equal(t, live, r.body["items"].([]any)[0].(map[string]any)["id"])

	require.Equal(t, 3, a.count(`SELECT count(*) FROM audit_log WHERE action = 'banners.approve' AND subject_type = 'banner'`))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'banners.approve' AND subject_id = $1 AND after ->> 'reason' = 'the photo is the shop''s own'`, later))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'banners.remove' AND subject_id = $1`, live))
}
