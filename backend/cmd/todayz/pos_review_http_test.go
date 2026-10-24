package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/pos"
)

// The W2-04 review fixes (D-131): counter-code locks per phone and per
// cashier, sold_at and the reversal window, the guard tables, the bill
// claim lifecycle and the offline offer risk. Codes and phones are never
// printed, as in pos_http_test.go.

func (p *posShop) codeSale(tok, bill string) resp {
	p.t.Helper()
	body := p.saleBody(bill, line{p.salt, 1})
	body["link"] = map[string]any{"method": "counter_code"}
	return p.sell(tok, body)
}

func (p *posShop) qrSale(clientID, claimID string) map[string]any {
	b := p.saleBody(clientID, line{p.atta, 1})
	b["link"] = map[string]any{"method": "bill_qr", "claimId": claimID}
	return b
}

// 5 wrong codes for a phone at a store lock the phone there for 15 min,
// whatever bill the next check names.
func TestPOSPhoneLockAcrossBills(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	right := testTOTP(secret, p.a.clk.Now())
	wrong := wrongCode(right)
	for i := range 5 {
		r := p.verifyCode(p.cashier, custPhone, wrong, p.newID()) // a new bill each time
		want := "code_invalid"
		if i == 4 {
			want = "code_locked"
		}
		require.Equal(t, want, problemCode(r), "wrong code %d: status %d", i+1, r.code)
	}
	r := p.verifyCode(p.cashier, custPhone, right, p.newID())
	require.Equal(t, "code_locked", problemCode(r), "a new saleClientId is still locked: status %d", r.code)
	r = p.verifyCode(p.manager, custPhone, right, p.newID())
	require.Equal(t, "code_locked", problemCode(r), "the phone is locked for every cashier: status %d", r.code)
	require.Equal(t, 5, p.a.count(`SELECT count(*) FROM counter_code_failures WHERE store_id = $1 AND source = 'online'`, uuidOf(t, p.sid)))

	p.advance(15*time.Minute + time.Second)
	bill := p.newID()
	r = p.verifyCode(p.cashier, custPhone, testTOTP(secret, p.a.clk.Now()), bill)
	require.Equal(t, http.StatusOK, r.code, "after 15 min: status %d", r.code)
	s := p.codeSale(p.cashier, bill)
	require.Equal(t, http.StatusOK, s.code, s.raw)
	p.noSecrets(right, wrong, custPhone)
}

// 20 wrong codes by one cashier in an hour, across phones, lock that
// cashier's code checks for an hour; other cashiers are not locked.
func TestPOSCashierCodeLock(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	right := testTOTP(secret, p.a.clk.Now())
	for i := range 20 {
		phone := fmt.Sprintf("+9198765%05d", 100+i) // no account: a wrong code (D-12)
		r := p.verifyCode(p.cashier, phone, right, p.newID())
		want := "code_invalid"
		if i == 19 {
			want = "code_locked"
		}
		require.Equal(t, want, problemCode(r), "failure %d: status %d", i+1, r.code)
	}
	r := p.verifyCode(p.cashier, custPhone, right, p.newID())
	require.Equal(t, "code_locked", problemCode(r), "the cashier is locked: status %d", r.code)
	bill := p.newID()
	r = p.verifyCode(p.manager, custPhone, right, bill)
	require.Equal(t, http.StatusOK, r.code, "another cashier is not: status %d", r.code)

	p.advance(time.Hour + time.Second)
	r = p.verifyCode(p.cashier, custPhone, testTOTP(secret, p.a.clk.Now()), p.newID())
	require.Equal(t, http.StatusOK, r.code, "after an hour: status %d", r.code)
	p.noSecrets(right, custPhone)
}

// Offline wrong codes count into the same locks; once the phone is locked
// the sync's remaining links fail, the right code included, and the lock
// holds online too.
func TestPOSOfflineCodesHitTheLock(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	now := p.a.clk.Now()
	link := func(code string) map[string]any {
		return map[string]any{"method": "counter_code", "phone": custPhone, "code": code, "clockOffsetMs": 0}
	}
	var es []any
	for i := range 7 {
		at := now.Add(-time.Duration(2*(i+1)) * time.Minute)
		es = append(es, p.saleEntry(i+1, at, link(wrongCode(testTOTP(secret, at))), line{p.salt, 1}))
	}
	at := now.Add(-30 * time.Minute)
	es = append(es, p.saleEntry(8, at, link(testTOTP(secret, at)), line{p.salt, 1}))
	r := p.sync(p.cashier, es...)
	require.Equal(t, http.StatusOK, r.code, "sync: status %d", r.code)
	for i, s := range statuses(r) {
		require.Equal(t, "applied", s, "entry %d: sales always apply", i+1)
	}
	for i, s := range linkStatuses(r) {
		require.Equal(t, "failed", s, "entry %d", i+1)
	}
	require.Equal(t, 5, p.a.count(`SELECT count(*) FROM counter_code_failures WHERE store_id = $1 AND source = 'sync'`, uuidOf(t, p.sid)),
		"the 5th locks; the locked ones are not counted again")
	require.Zero(t, p.a.count(`SELECT count(*) FROM counter_code_uses WHERE store_id = $1`, uuidOf(t, p.sid)), "the right code was never matched")

	v := p.verifyCode(p.cashier, custPhone, testTOTP(secret, p.a.clk.Now()), p.newID())
	require.Equal(t, "code_locked", problemCode(v), "online after the sync: status %d", v.code)
	p.noSecrets(custPhone)
}

// A device clock ahead of the server cannot stretch the reversal window:
// sold_at is at most received_at, and an offline sale's window runs from
// received_at.
func TestPOSReversalWindowFromReceived(t *testing.T) {
	p := newPosShop(t)
	now := p.a.clk.Now()
	ahead := p.sync(p.cashier, p.saleEntry(1, now.Add(20*time.Hour), nil, line{p.salt, 1}))
	require.Equal(t, []string{"applied"}, statuses(ahead), ahead.raw)
	behind := p.sync(p.cashier, p.saleEntry(2, now.Add(-5*time.Hour), nil, line{p.salt, 1}))
	require.Equal(t, []string{"applied"}, statuses(behind), behind.raw)
	aheadID := results(ahead)[0]["saleId"].(string)
	behindID := results(behind)[0]["saleId"].(string)

	p.advance(23 * time.Hour) // 28 h after the behind sale's device time
	rv := p.reverse(p.manager, behindID)
	require.Equal(t, http.StatusOK, rv.code, "23 h after it reached the server: %s", rv.raw)

	p.advance(time.Hour + time.Second)
	rv = p.reverse(p.manager, aheadID)
	require.Equal(t, http.StatusConflict, rv.code, rv.raw)
	require.Equal(t, "reversal_window_passed", problemCode(rv), "a future at is no longer 20 h of extra window")
}

// The counter-code and sync guard tables are append-only for the app role,
// and no role truncates them.
func TestPOSGuardTablesAppendOnly(t *testing.T) {
	p := newPosShop(t)
	ctx := context.Background()
	for _, tbl := range []string{"counter_code_uses", "counter_code_checks", "counter_code_failures", "sync_entries", "receipt_tokens"} {
		_, err := p.a.tdb.Pool.Exec(ctx, "DELETE FROM "+tbl)
		require.ErrorContains(t, err, "permission denied", "todayz_app DELETE %s", tbl)
		_, err = p.a.tdb.Pool.Exec(ctx, "TRUNCATE "+tbl)
		require.ErrorContains(t, err, "permission denied", "todayz_app TRUNCATE %s", tbl)
		_, err = p.a.tdb.OwnerPool.Exec(ctx, "TRUNCATE "+tbl)
		require.Error(t, err, "the no_truncate trigger refuses the owner too: %s", tbl)
	}
	for tbl, col := range map[string]string{"sync_entries": "store_id", "receipt_tokens": "expires_at", "counter_code_failures": "store_id"} {
		_, err := p.a.tdb.Pool.Exec(ctx, "UPDATE "+tbl+" SET "+col+" = "+col)
		require.ErrorContains(t, err, "permission denied", "todayz_app UPDATE %s", tbl)
	}
}

// An offline line's offer discount (no offer id reaches the server) is kept
// as sold and counts as offline risk; a cashier without pos.discount is
// flagged for the manager, a manager is not.
func TestPOSOfflineOfferDiscount(t *testing.T) {
	p := newPosShop(t)
	flags := func(rule string) int {
		return p.a.count(`SELECT count(*) FROM risk_flags WHERE subject_type = 'store' AND subject_id = $1 AND rule = $2`, uuidOf(t, p.sid), rule)
	}
	offered := func(seq int) map[string]any {
		e := p.saleEntry(seq, p.a.clk.Now(), nil, line{p.atta, 1})
		e["sale"].(map[string]any)["lines"].([]any)[0].(map[string]any)["offerDiscountPaise"] = 2900
		return e
	}
	r := p.sync(p.manager, offered(1))
	require.Equal(t, []string{"applied"}, statuses(r), r.raw)
	require.Equal(t, 2900, p.a.count(`SELECT offer_discount_paise FROM pos_sale_lines WHERE sale_id = $1`,
		uuidOf(t, results(r)[0]["saleId"].(string))), "kept as sold")
	require.Zero(t, flags("pos.offline_offer_without_discount_role"), "a manager may discount")

	r = p.sync(p.cashier, offered(1))
	require.Equal(t, []string{"applied"}, statuses(r), r.raw)
	require.Equal(t, 1, flags("pos.offline_offer_without_discount_role"))
	require.Zero(t, flags("pos.offline_links"), "2 is under pos_offline_links_risk_per_day")

	// Offer sales and offline links share the day's limit (20).
	var es []any
	for i := range 19 {
		es = append(es, offered(i+1))
	}
	r = p.sync(p.cashier, es...)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 1, flags("pos.offline_links"), "21 offer sales in a day")
	require.Equal(t, 1, flags("pos.offline_offer_without_discount_role"), "once a day")
}

// A retry of a bill-QR sale with the same clientId under a new
// Idempotency-Key, at the same time, is the same sale; the hold is captured
// once.
func TestPOSBillQRConcurrentRetry(t *testing.T) {
	p := newPosShop(t)
	for i := range 3 {
		bill := p.newID()
		id, tok := p.claim(p.cashier, bill, 29000)
		c := p.confirm(p.cust, tok, 5)
		require.Equal(t, http.StatusOK, c.code, "confirm: status %d", c.code)
		body := p.qrSale(bill, id)
		var r1, r2 resp
		parallel(
			func() { r1 = p.sell(p.cashier, body) },
			func() { r2 = p.sell(p.cashier, body) },
		)
		require.Equal(t, http.StatusOK, r1.code, r1.raw)
		require.Equal(t, http.StatusOK, r2.code, r2.raw)
		require.Equal(t, r1.body["id"], r2.body["id"], "one sale")
		require.Len(t, p.credits.captured, i+1, "captured once")
		p.noSecrets(tok)
	}
}

// The credit-hold lifecycle: a new QR for a bill supersedes the old one and
// releases its hold; a confirmed claim past 120 s does not bind; the bill's
// sale releases what is left; the sweep releases expired holds. Every hold
// is either released or captured, never both.
func TestPOSClaimHoldLifecycle(t *testing.T) {
	p := newPosShop(t)
	bill := p.newID()
	a, tokA := p.claim(p.cashier, bill, 29000)
	require.Equal(t, http.StatusOK, p.confirm(p.cust, tokA, 10).code)
	b, tokB := p.claim(p.cashier, bill, 29000)
	require.Equal(t, []ids.ID{idOf(t, a)}, p.credits.released, "the new QR released the old one's hold")
	require.Equal(t, "expired", p.getClaim(a).body["status"])
	r := p.confirm(p.cust, tokA, 10)
	require.Equal(t, "claim_expired", problemCode(r), "a superseded claim: status %d", r.code)
	require.Equal(t, http.StatusOK, p.confirm(p.cust2, tokB, 10).code)

	r = p.sell(p.cashier, p.qrSale(bill, a))
	require.Equal(t, "claim_expired", problemCode(r), r.raw)
	s := p.sell(p.cashier, p.qrSale(bill, b))
	require.Equal(t, http.StatusOK, s.code, s.raw)
	require.Equal(t, []ids.ID{idOf(t, b)}, p.credits.captured)
	require.Equal(t, []ids.ID{idOf(t, a)}, p.credits.released)

	// Past 120 s a confirmed claim does not bind.
	late := p.newID()
	c, tokC := p.claim(p.cashier, late, 29000)
	require.Equal(t, http.StatusOK, p.confirm(p.cust, tokC, 10).code)
	p.advance(120 * time.Second)
	r = p.sell(p.cashier, p.qrSale(late, c))
	require.Equal(t, "claim_expired", problemCode(r), r.raw)
	require.Equal(t, "expired", p.getClaim(c).body["status"])

	// The bill's sale, rung without the QR, releases its confirmed claim.
	plain := p.newID()
	d, tokD := p.claim(p.cashier, plain, 29000)
	require.Equal(t, http.StatusOK, p.confirm(p.cust, tokD, 10).code)
	s = p.sell(p.cashier, p.saleBody(plain, line{p.atta, 1}))
	require.Equal(t, http.StatusOK, s.code, s.raw)
	require.Equal(t, []ids.ID{idOf(t, a), idOf(t, d)}, p.credits.released)
	require.Equal(t, "expired", p.getClaim(d).body["status"])

	// The sweep releases the expired hold of c, once.
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc, err := pos.New(pos.Deps{
		Config: p.cfg, DB: db.New(p.a.tdb.Pool), Clock: p.a.clk, Log: log, Credits: p.credits,
		Settings: settings.NewCache(settings.PGLoader(p.a.tdb.Pool), settings.Options{Log: log}),
	})
	require.NoError(t, err)
	expired, released, err := svc.ExpireClaims(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.Equal(t, 1, released)
	require.Equal(t, []ids.ID{idOf(t, a), idOf(t, d), idOf(t, c)}, p.credits.released)
	require.Equal(t, []ids.ID{idOf(t, b)}, p.credits.captured, "no hold is both")
	require.Empty(t, p.credits.holds)

	// The registered timer: idempotent, and an open claim has no hold.
	fr, err := p.runner.RunScheduled(context.Background(), jobs.KindPosBillClaimExpiry)
	require.NoError(t, err)
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 0, fr.Result.Summary["expired"])
	p.claim(p.cashier, p.newID(), 100)
	p.advance(121 * time.Second)
	fr, err = p.runner.RunScheduled(context.Background(), jobs.KindPosBillClaimExpiry)
	require.NoError(t, err)
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 1, fr.Result.Summary["expired"])
	require.EqualValues(t, 0, fr.Result.Summary["released"])
	p.noSecrets(tokA, tokB, tokC, tokD)
}

// The review's nits: a manual discount past what the offer left is refused,
// not cut down; a verified check binds a sale for 2 minutes only.
func TestPOSManualDiscountAndCheckAge(t *testing.T) {
	p := newPosShop(t)
	p.offerOn("percent", 10, nil) // ₹290 → ₹29 off
	body := p.saleBody(p.newID(), line{p.atta, 1})
	body["lines"].([]any)[0].(map[string]any)["manualDiscountPaise"] = 27000
	r := p.sell(p.manager, body)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	body["lines"].([]any)[0].(map[string]any)["manualDiscountPaise"] = 26000
	r = p.sell(p.manager, body)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 100, r.body["totalPaise"])

	secret := p.seed(p.cust)
	bill := p.newID()
	v := p.verifyCode(p.cashier, custPhone, testTOTP(secret, p.a.clk.Now()), bill)
	require.Equal(t, http.StatusOK, v.code, "verify: status %d", v.code)
	p.advance(2*time.Minute + time.Second)
	s := p.codeSale(p.cashier, bill)
	require.Equal(t, "code_invalid", problemCode(s), s.raw)
	p.noSecrets(custPhone)
}
