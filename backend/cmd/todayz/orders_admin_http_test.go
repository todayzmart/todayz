package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/orders"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

const adminPhone = "+919000000021"

// adminDesk signs a support person in to the admin app (SMS OTP, D-264).
type adminDesk struct {
	o    *orderShop
	cust string // the same person's customer sign-in (kept fresh by o.refresh)
}

// newAdminDesk makes the user (a customer sign-in creates it) an admin with
// roles.
func newAdminDesk(o *orderShop, roles ...string) *adminDesk {
	o.t.Helper()
	d := &adminDesk{o: o}
	o.login(&d.cust, adminPhone, "customer")
	me := o.a.call(d.cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(o.t, http.StatusOK, me.code, me.raw)
	o.a.exec(`INSERT INTO admin_users (user_id, roles) VALUES ($1, $2)`, uuidOf(o.t, me.body["id"].(string)), roles)
	return d
}

// token signs in afresh at the clock's time and passes the SMS step-up.
func (d *adminDesk) token() string {
	d.o.t.Helper()
	return d.o.a.adminToken(adminPhone, "orders-admin-test")
}

func (d *adminDesk) cancel(tok, id string, body map[string]any) resp {
	d.o.t.Helper()
	return d.o.a.call(tok, http.MethodPost, "/v1/admin/orders/"+id+"/cancel", body, idem(d.o.a))
}

func (o *orderShop) storeStrikes() int {
	return o.a.count(`SELECT count(*) FROM strikes WHERE subject_type = 'store'`)
}

func (o *orderShop) storeFlags() int {
	return o.a.count(`SELECT count(*) FROM risk_flags WHERE subject_type = 'store' AND rule = 'store_strikes' AND subject_id = $1`, uuidOf(o.t, o.sid))
}

// D-20 / 01 §5.2 (pulled forward from W3-05, D-83): support cancels an order
// from any non-terminal state; stock, credits and the offer go back, only
// the side named at fault gets a strike, and the audit row is written with
// the cancel.
func TestAdminCancelHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	cust := o.grantCredits(40)
	offer := o.offerOn("percent", 10, nil)
	tok := desk.token()

	// accepted, with credits and the offer: everything is released.
	lines := []line{{o.butter, 2}, {o.atta, 1}}
	body := o.cart(lines, o.total(lines)*9/10-10*100) // 10% off, then 10 credits of ₹1
	body["creditsToUse"] = 10
	r := o.a.call(o.cust, http.MethodPost, "/v1/me/orders", body, idem(o.a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	id := r.body["id"].(string)
	require.True(t, o.walletIs(cust, 30), "10 held")
	r = o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Positive(t, o.held(id))
	require.Equal(t, 1, o.redemptions(offer, false))
	r = desk.cancel(tok, id, map[string]any{"reason": "store_request", "strike": "store", "note": "shop closed for a wedding"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "cancelled", r.body["status"])
	require.Equal(t, "store_request", r.body["statusReason"])
	require.Equal(t, "store", r.body["strike"])
	require.Equal(t, "cancelled", o.status(id))
	require.Zero(t, o.held(id))
	require.Equal(t, 1, o.redemptions(offer, true))
	require.True(t, o.walletIs(cust, 40), "the held credits are back")
	require.Equal(t, 1, o.storeStrikes())
	require.Zero(t, o.strikes(), "no customer strike")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM audit_log WHERE action = 'order.admin_cancel' AND subject_id = $1
	  AND before->>'status' = 'accepted' AND after->>'strike' = 'store' AND NOT (after ? 'note')`, id))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'cancelled' AND actor_kind = 'admin'`, uuidOf(t, id)))

	o.a.exec(`UPDATE offers SET status = 'paused' WHERE id = $1`, uuidOf(t, offer)) // plain prices from here

	// placed, the customer at fault: one customer strike, no store strike.
	id = o.order()
	r = desk.cancel(tok, id, map[string]any{"reason": "fraud_suspected", "strike": "customer"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 1, o.strikes())
	require.Equal(t, 1, o.storeStrikes())

	// awaiting_customer, nobody at fault.
	id = o.orderOf(line{o.butter, 2}, line{o.salt, 1})
	r = o.partial(id, 1, 1)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = desk.cancel(tok, id, map[string]any{"reason": "technical_issue", "strike": "none"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "cancelled", o.status(id))
	require.Equal(t, 1, o.strikes())
	require.Equal(t, 1, o.storeStrikes())

	// A terminal order is 409 and changes nothing; a repeat on the same
	// order adds no strike.
	r = desk.cancel(tok, id, map[string]any{"reason": "technical_issue", "strike": "store"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	require.Equal(t, 1, o.storeStrikes())

	// "other" needs a note.
	id = o.order()
	r = desk.cancel(tok, id, map[string]any{"reason": "other", "strike": "none"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, "placed", o.status(id))

	// A customer, a shop owner and an admin role without the grant are refused.
	body = map[string]any{"reason": "customer_request", "strike": "none"}
	for _, tk := range []string{o.cust, o.owner} {
		r = desk.cancel(tk, id, body)
		require.Equal(t, http.StatusForbidden, r.code, r.raw)
	}
	o.a.exec(`UPDATE admin_users SET roles = ARRAY['finance']`)
	r = desk.cancel(tok, id, body)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "placed", o.status(id))
	o.a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	r = desk.cancel(tok, id, body)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 4, o.a.count(`SELECT count(*) FROM audit_log WHERE action = 'order.admin_cancel'`), "one per cancel, none for a refusal")
}

// reliabilityRun runs the hourly store_reliability job once.
func (o *orderShop) reliabilityRun() platformjobs.FakeRun {
	o.t.Helper()
	fr, err := o.runner.RunScheduled(context.Background(), orders.KindStoreReliability)
	require.NoError(o.t, err)
	return fr
}

func (o *orderShop) reliability() (score *float64, at *time.Time) {
	o.t.Helper()
	require.NoError(o.t, o.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT reliability_score::float8, reliability_computed_at FROM stores WHERE id = $1`, uuidOf(o.t, o.sid)).Scan(&score, &at))
	return score, at
}

// BK-46 (D-67): store_strike_risk_threshold (5) unreset store strikes in 30
// days raise one store_strikes risk flag and nothing pauses the store; the
// hourly store_reliability job lowers the reliability score and writes only
// the reliability fields.
func TestBK46StoreStrikesFlagAndReliability(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	strike := func() {
		t.Helper()
		r := desk.cancel(tok, o.accepted(), map[string]any{"reason": "store_request", "strike": "store"})
		require.Equal(t, http.StatusOK, r.code, r.raw)
	}

	// A clean store: every order accepted, score 1.
	fr := o.reliabilityRun()
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	var version int
	require.NoError(t, o.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT version FROM stores WHERE id = $1`, uuidOf(t, o.sid)).Scan(&version))

	for range 4 {
		strike()
	}
	require.Equal(t, 4, o.storeStrikes())
	require.Zero(t, o.storeFlags(), "4 strikes: no flag")
	strike()
	require.Equal(t, 1, o.storeFlags(), "the 5th strike raises the flag")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM risk_flags WHERE rule = 'store_strikes' AND status = 'open' AND score = 50
	  AND (details->>'strikes')::int = 5 AND (details->>'threshold')::int = 5`))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM stores WHERE id = $1 AND status = 'live'`, uuidOf(t, o.sid)), "no automatic pause")
	strike()
	require.Equal(t, 1, o.storeFlags(), "one open flag, not one per strike")

	// The next hour's run lowers the score; the store row's version and
	// rating stay.
	o.advance(time.Hour)
	fr = o.reliabilityRun()
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	score, at := o.reliability()
	require.NotNil(t, score)
	require.Less(t, *score, 1.0)
	require.Equal(t, o.a.clk.Now(), at.UTC())
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM stores WHERE id = $1 AND version = $2`, uuidOf(t, o.sid), version))
	require.Zero(t, fr.Result.Summary["flags"], "the open flag is not raised again")
	fr = o.reliabilityRun()
	require.Equal(t, platformjobs.StatusSkipped, fr.Status, "an hour runs once")

	// Day 31: the orders and strikes have aged out of the window, so the
	// score goes back to NULL (ranking reads 1.0).
	o.a.clk.Advance(31 * day) // no call needs a session from here
	fr = o.reliabilityRun()
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	score, _ = o.reliability()
	require.Nil(t, score)
}

// The threshold is a setting: at 2 the second store strike raises the flag,
// and the hourly job raises it for a store already over the line (a flag
// cleared since the strikes were counted is not raised again for them).
func TestBK46ThresholdIsASetting(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	strike := func() {
		t.Helper()
		r := desk.cancel(tok, o.accepted(), map[string]any{"reason": "store_request", "strike": "store"})
		require.Equal(t, http.StatusOK, r.code, r.raw)
	}
	strike()
	strike()
	require.Zero(t, o.storeFlags(), "2 < 5")
	o.a.exec(`UPDATE platform_settings SET value = '2' WHERE key = 'store_strike_risk_threshold'`)
	o.settings.Invalidate()
	fr := o.reliabilityRun()
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 1, fr.Result.Summary["flags"])
	require.Equal(t, 1, o.storeFlags())

	// Support clears it; the same strikes do not raise it again.
	o.a.exec(`UPDATE risk_flags SET status = 'cleared', reviewed_at = $1`, o.a.clk.Now())
	o.advance(time.Hour)
	fr = o.reliabilityRun()
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 0, fr.Result.Summary["flags"])
	tok = desk.token()
	strike() // a new strike after the clear counts again
	require.Equal(t, 2, o.storeFlags())
}
