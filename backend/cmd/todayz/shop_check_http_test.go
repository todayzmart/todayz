package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// D-274: the shop check. A shop is live as soon as onboarding is filled in;
// field_agent, admin and super_admin work the queue (newest first) and mark a
// shop checked or reject it with a reason, both audited. Every other admin
// role gets 403. A reject hides the shop and cancels its open online orders
// with shop_rejected: no strike, held credits back, each customer told.
func TestShopCheckHTTP(t *testing.T) {
	o := newOrderShop(t)
	a := o.a
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	get := func(path string) resp { t.Helper(); return a.call(tok, http.MethodGet, path, nil, nil) }
	post := func(path string, body any) resp { t.Helper(); return a.call(tok, http.MethodPost, path, body, nil) }
	bid := o.B[len("/v1/biz/businesses/"):]
	base := "/v1/admin/businesses/" + bid
	reason := map[string]any{"reason": "the shop was closed and empty on the visit"}

	// Two more live shops, older and newer, and a draft that is not live.
	older, _ := a.newBusiness(o.owner, "Older Kirana")
	newer, _ := a.newBusiness(o.owner, "Newer Kirana")
	draft, _ := a.newBusiness(o.owner, "Draft Kirana")
	now := a.clk.Now()
	a.exec(`UPDATE businesses SET verified_at = $2 WHERE id = $1`, uuidOf(t, bid), now.Add(-2*time.Hour))
	// The second owner's shop (o.SY) was checked long ago.
	a.exec(`UPDATE businesses SET verified_at = $1, check_status = 'checked', checked_at = $1 WHERE id <> ALL($2)`,
		now.Add(-90*24*time.Hour), []pgtype.UUID{uuidOf(t, bid), uuidOf(t, older), uuidOf(t, newer), uuidOf(t, draft)})
	a.exec(`UPDATE businesses SET status = 'verified', verified_at = $2 WHERE id = $1`, uuidOf(t, older), now.Add(-48*time.Hour))
	a.exec(`UPDATE businesses SET status = 'verified', verified_at = $2 WHERE id = $1`, uuidOf(t, newer), now.Add(-time.Minute))

	// Roles without the check: the queue and both moves are 403, nothing moves.
	for _, role := range []string{"support", "finance", "catalogue", "analyst"} {
		a.exec(`UPDATE admin_users SET roles = $1`, []string{role})
		require.Equal(t, http.StatusForbidden, get("/v1/admin/shop-checks").code, role)
		require.Equal(t, http.StatusForbidden, post(base+"/check", map[string]any{}).code, role)
		require.Equal(t, http.StatusForbidden, post(base+"/check-reject", reason).code, role)
	}
	require.Equal(t, 3, a.count(`SELECT count(*) FROM businesses WHERE status = 'verified' AND check_status = 'unchecked'`))

	// A field agent sees the live, unchecked shops, newest first, with stores.
	a.exec(`UPDATE admin_users SET roles = ARRAY['field_agent']`)
	r := get("/v1/admin/shop-checks")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	var queue []string
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		queue = append(queue, m["id"].(string))
		require.Equal(t, "unchecked", m["checkStatus"])
		require.Equal(t, "verified", m["status"])
	}
	require.Equal(t, []string{newer, bid, older}, queue, "newest first, no draft")
	require.NotContains(t, queue, draft)
	first := r.body["items"].([]any)[1].(map[string]any)
	require.NotEmpty(t, first["stores"], r.raw)
	r = get("/v1/admin/shop-checks?limit=1")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	next := r.body["nextCursor"].(string)
	r = get("/v1/admin/shop-checks?limit=1&cursor=" + next)
	require.Equal(t, bid, r.body["items"].([]any)[0].(map[string]any)["id"], "the cursor walks on")

	// A draft is not live: nothing to check.
	r = post("/v1/admin/businesses/"+draft+"/check", map[string]any{})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	require.Equal(t, http.StatusConflict, post("/v1/admin/businesses/"+draft+"/check-reject", reason).code)

	// The shop has open orders: placed, accepted, one paid partly in credits.
	cust := o.grantCredits(50)
	placed := o.order()
	accepted := o.acceptedOf(line{o.atta, 1})
	withCredits := o.placeWithCredits(20)
	open := a.count(`SELECT count(*) FROM orders o JOIN stores s ON s.id = o.store_id
	  WHERE s.business_id = $1 AND o.status IN ('placed','awaiting_customer','accepted','ready','out_for_delivery')`, uuidOf(t, bid))
	require.GreaterOrEqual(t, open, 3)
	strikes := o.strikes()

	// Reject needs a reason, then hides the shop and cancels every open order.
	require.Equal(t, http.StatusUnprocessableEntity, post(base+"/check-reject", map[string]any{}).code)
	r = post(base+"/check-reject", reason)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, open, r.body["cancelledOrders"])
	shop := r.body["shop"].(map[string]any)
	require.Equal(t, "rejected", shop["status"])
	require.Equal(t, "rejected", shop["checkStatus"])
	require.Equal(t, reason["reason"], shop["checkNote"])
	for _, id := range []string{placed, accepted, withCredits} {
		require.Equal(t, 1, a.count(`SELECT count(*) FROM orders WHERE id = $1 AND status = 'cancelled' AND status_reason = 'shop_rejected'`, uuidOf(t, id)), id)
		require.Equal(t, 1, a.count(`SELECT count(*) FROM notifications WHERE kind = 'order_update' AND ref_id = $1 AND params->>'variant' = 'shop_rejected'`, uuidOf(t, id)), id)
	}
	require.Equal(t, strikes, o.strikes(), "a shop reject is no customer's fault")
	require.Equal(t, 0, o.storeStrikes())
	require.Equal(t, 0, a.count(`SELECT count(*) FROM credit_holds WHERE order_id = $1 AND status = 'held'`, uuidOf(t, withCredits)))
	require.True(t, o.walletIs(cust, 50), "the held credits are back in the wallet")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'business.check_reject' AND subject_id = $1
	  AND before->>'checkStatus' = 'unchecked' AND (after->>'cancelledOrders')::int = $2`, bid, open))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM businesses WHERE id = $1 AND status = 'verified'`, uuidOf(t, bid)), "hidden: search and orders need verified")
	r = a.call(o.cust, http.MethodPost, "/v1/me/orders", o.cart([]line{{o.butter, 1}}, o.total([]line{{o.butter, 1}})), idem(a))
	require.NotEqual(t, http.StatusCreated, r.code, "a rejected shop takes no order: %s", r.raw)

	// Rejected is final here: neither move again.
	require.Equal(t, http.StatusConflict, post(base+"/check-reject", reason).code)
	require.Equal(t, http.StatusConflict, post(base+"/check", map[string]any{}).code)

	// An admin checks the newer shop with a note; it leaves the queue.
	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	r = post("/v1/admin/businesses/"+newer+"/check", map[string]any{"note": "visited, stock and photos match"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "checked", r.body["checkStatus"])
	require.Equal(t, "verified", r.body["status"], "a check changes nothing the shop sees")
	require.NotNil(t, r.body["checkedAt"])
	require.Equal(t, http.StatusConflict, post("/v1/admin/businesses/"+newer+"/check", map[string]any{}).code)
	require.Equal(t, http.StatusConflict, post("/v1/admin/businesses/"+newer+"/check-reject", reason).code, "a checked shop is suspended on A-03 instead")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'business.check' AND subject_id = $1 AND after->>'note' = 'visited, stock and photos match'`, newer))

	// super_admin can check too; the queue is down to the older shop.
	a.exec(`UPDATE admin_users SET roles = ARRAY['super_admin']`)
	r = get("/v1/admin/shop-checks")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	r = post("/v1/admin/businesses/"+older+"/check", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = get(base)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "rejected", r.body["checkStatus"], "A-03 shows the check too")
}
