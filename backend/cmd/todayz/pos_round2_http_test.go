package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/pos"
)

// The W2-04 second review round (D-131): ordered sync locks, the shared
// pos deps, the per-claim sweep, the cross-store phone ceiling, offline
// offers counted once and only when the engine would not give them, the
// lock check before a counter check is added, and the edges left
// untested. Codes and phones are never printed, as in pos_http_test.go.

func (p *posShop) verifyCodeAt(tok, store, phone, code, saleClientID string) resp {
	p.t.Helper()
	return p.a.call(tok, http.MethodPost, store+"/counter-code/verify",
		map[string]any{"phone": phone, "code": code, "saleClientId": saleClientID}, idem(p.a))
}

// codeEntry is an offline sale of a custom line linked by a counter code
// for phone (custom, so two syncs share no item row).
func (p *posShop) codeEntry(seq int, phone, code string) map[string]any {
	return map[string]any{
		"clientId": p.newID(), "deviceSeq": seq, "kind": "sale", "at": p.a.clk.Now(),
		"sale": map[string]any{
			"paymentMethod": "upi",
			"lines":         []any{map[string]any{"clientId": p.newID(), "name": "loose sugar", "unitPricePaise": 4500, "quantity": 1}},
			"link":          map[string]any{"method": "counter_code", "phone": phone, "code": code, "clockOffsetMs": 0},
		},
	}
}

// Two cashiers syncing the same phones in opposite order, at once, both
// succeed: the batch's code locks are taken up front in sorted order.
func TestPOSSyncCodeLocksNoDeadlock(t *testing.T) {
	p := newPosShop(t)
	const x, y = "+917000000101", "+917000000102" // no account: wrong codes
	seqA, seqB := 0, 0
	for round := range 6 {
		var ra, rb resp
		seqA, seqB = seqA+2, seqB+2
		ea := []any{p.codeEntry(seqA-1, x, "0000"), p.codeEntry(seqA, y, "0000")}
		eb := []any{p.codeEntry(seqB-1, y, "0000"), p.codeEntry(seqB, x, "0000")}
		cursor := p.cursor()
		parallel(
			func() { ra = p.syncKey(p.cashier, idem(p.a), cursor, ea...) },
			func() { rb = p.syncKey(p.manager, idem(p.a), cursor, eb...) },
		)
		require.Equal(t, http.StatusOK, ra.code, "round %d cashier: %s", round, problemCode(ra))
		require.Equal(t, http.StatusOK, rb.code, "round %d manager: %s", round, problemCode(rb))
		require.Equal(t, []string{"applied", "applied"}, statuses(ra), "round %d", round)
		require.Equal(t, []string{"applied", "applied"}, statuses(rb), "round %d", round)
	}
}

// The API's routes and the worker's sweep build pos with the same stock,
// credits and referrals, so a hold is released through the ledger that
// made it.
func TestPOSDepsShared(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC))
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	api := posDeps(&deps{Clock: clk, Log: log})
	job := jobs.PosJobDeps(jobs.Deps{Clock: clk, Log: log})
	for name, pair := range map[string][2]any{
		"Stock": {api.Stock, job.Stock}, "Credits": {api.Credits, job.Credits}, "Referrals": {api.Referrals, job.Referrals},
	} {
		require.NotNil(t, pair[0], name)
		require.Equal(t, fmt.Sprintf("%T", pair[0]), fmt.Sprintf("%T", pair[1]), name)
	}
}

// A claim whose release fails is logged, counted and left live; the claims
// behind it still expire, and the next pass retries it.
func TestPOSSweepPoisonClaim(t *testing.T) {
	p := newPosShop(t)
	var claims []string
	for range 3 {
		id, tok := p.claim(p.cashier, p.newID(), 29000)
		require.Equal(t, http.StatusOK, p.confirm(p.cust, tok, 5).code, "confirm")
		claims = append(claims, id)
		p.noSecrets(tok)
		p.a.clk.Advance(time.Second) // expires_at orders the sweep: the poison claim first
	}
	poison := idOf(t, claims[0])
	p.credits.mu.Lock()
	p.credits.poison[poison] = errors.New("fake credits: ledger down")
	p.credits.mu.Unlock()
	p.advance(121 * time.Second)

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc, err := pos.New(pos.Deps{
		Config: p.cfg, DB: db.New(p.a.tdb.Pool), Clock: p.a.clk, Log: log, Credits: p.credits,
		Settings: settings.NewCache(settings.PGLoader(p.a.tdb.Pool), settings.Options{Log: log}),
	})
	require.NoError(t, err)
	sw, err := svc.ExpireClaims(context.Background())
	require.NoError(t, err)
	require.Equal(t, pos.Sweep{Expired: 2, Released: 2, Failed: 1}, sw)
	status := func(id string) string {
		var s string
		require.NoError(t, p.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT status FROM bill_claims WHERE id = $1`, uuidOf(t, id)).Scan(&s))
		return s
	}
	require.Equal(t, "claimed", status(claims[0]), "left live")
	require.Equal(t, "expired", status(claims[1]))
	require.Equal(t, "expired", status(claims[2]))
	require.Equal(t, []ids.ID{idOf(t, claims[1]), idOf(t, claims[2])}, p.credits.released)

	p.credits.mu.Lock()
	delete(p.credits.poison, poison)
	p.credits.mu.Unlock()
	sw, err = svc.ExpireClaims(context.Background())
	require.NoError(t, err)
	require.Equal(t, pos.Sweep{Expired: 1, Released: 1}, sw, "retried")
	require.Equal(t, "expired", status(claims[0]))
	require.Empty(t, p.credits.holds)
}

// pos_code_phone_failures_per_hour (10) wrong codes for a phone across
// stores refuse it at every store, under each store's own 5-in-15-minutes,
// until the hour's count drops below it.
func TestPOSPhoneCeilingAcrossStores(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	wrongAt := func(tok, store string) resp {
		return p.verifyCodeAt(tok, store, custPhone, wrongCode(testTOTP(secret, p.a.clk.Now())), p.newID())
	}
	for i := range 4 {
		require.Equal(t, "code_invalid", problemCode(wrongAt(p.cashier, p.S)), "S %d", i+1)
	}
	p.advance(16 * time.Minute) // S's 15-minute count starts again; the hour's does not
	for i := range 4 {
		require.Equal(t, "code_invalid", problemCode(wrongAt(p.cashier, p.S)), "S again %d", i+1)
	}
	require.Equal(t, "code_invalid", problemCode(wrongAt(p.cashierS2, p.S2)), "9 in the hour")
	require.Equal(t, "code_locked", problemCode(wrongAt(p.cashierS2, p.S2)), "the 10th reaches the ceiling")
	require.Equal(t, 10, p.a.count(`SELECT count(*) FROM counter_code_failures`))

	right := testTOTP(secret, p.a.clk.Now())
	r := p.verifyCodeAt(p.cashierS2, p.S2, custPhone, right, p.newID())
	require.Equal(t, "code_locked", problemCode(r), "second store: status %d", r.code)
	r = p.verifyCodeAt(p.manager, p.S, custPhone, right, p.newID())
	require.Equal(t, "code_locked", problemCode(r), "first store, 4 in its 15 minutes: status %d", r.code)
	r = p.verifyCodeAt(p.manager, p.S, cust2Phone, right, p.newID())
	require.Equal(t, "code_invalid", problemCode(r), "another phone is not refused: status %d", r.code)

	p.advance(44*time.Minute + time.Second) // the first 4 leave the hour
	r = p.verifyCodeAt(p.cashierS2, p.S2, custPhone, testTOTP(secret, p.a.clk.Now()), p.newID())
	require.Equal(t, http.StatusOK, r.code, "under the ceiling again: status %d", r.code)
	p.noSecrets(right, custPhone)
}

// A phone lock ends at exactly 15:00 after the locking failure, and a
// locked phone adds no counter check.
func TestPOSPhoneLockEndsAt15(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	for i := range 5 {
		r := p.verifyCode(p.cashier, custPhone, wrongCode(testTOTP(secret, p.a.clk.Now())), p.newID())
		require.NotEqual(t, http.StatusOK, r.code, "wrong %d", i+1)
	}
	checks := `SELECT count(*) FROM counter_code_checks WHERE store_id = $1`
	require.Equal(t, 5, p.a.count(checks, uuidOf(t, p.sid)))

	p.advance(15*time.Minute - time.Millisecond)
	r := p.verifyCode(p.cashier, custPhone, testTOTP(secret, p.a.clk.Now()), p.newID())
	require.Equal(t, "code_locked", problemCode(r), "at 14:59.999: status %d", r.code)
	require.Equal(t, 5, p.a.count(checks, uuidOf(t, p.sid)), "a locked phone adds no counter check")

	p.advance(time.Millisecond)
	r = p.verifyCode(p.cashier, custPhone, testTOTP(secret, p.a.clk.Now()), p.newID())
	require.Equal(t, http.StatusOK, r.code, "at 15:00: status %d", r.code)
	p.noSecrets(custPhone)
}

// A phone not in E.164 never reaches the code check (the contract's
// pattern), so no spelling of a number gets a lock key of its own.
func TestPOSPhoneVariantsRefused(t *testing.T) {
	p := newPosShop(t)
	for _, v := range []string{"98765 43210", "09876543210", "919876543210", "+91-98765-43210", "9876543210"} {
		r := p.verifyCode(p.cashier, v, "0000", p.newID())
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "variant %q", v)
		require.Equal(t, "validation_failed", problemCode(r))
	}
	require.Zero(t, p.a.count(`SELECT count(*) FROM counter_code_failures`))
}

// The fresh-verified answer is for the verified phone only: another phone
// on the same bill is checked again.
func TestPOSFreshVerifiedNeedsSamePhone(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	right := testTOTP(secret, p.a.clk.Now())
	bill := p.newID()
	r := p.verifyCode(p.cashier, custPhone, right, bill)
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	name := r.body["customerName"]

	r = p.verifyCode(p.cashier, cust2Phone, right, bill)
	require.Equal(t, "code_invalid", problemCode(r), "another phone: status %d", r.code)
	r = p.verifyCode(p.cashier, custPhone, right, bill)
	require.Equal(t, http.StatusOK, r.code, "the same phone again: status %d", r.code)
	require.Equal(t, name, r.body["customerName"])
	s := p.codeSale(p.cashier, bill)
	require.Equal(t, http.StatusOK, s.code, "sale: %s", problemCode(s))
	p.noSecrets(right, custPhone, cust2Phone)
}

// A hold the ledger already released cannot be captured: the sale fails
// and leaves nothing.
func TestPOSCaptureOfReleasedHold(t *testing.T) {
	p := newPosShop(t)
	bill := p.newID()
	id, tok := p.claim(p.cashier, bill, 29000)
	require.Equal(t, http.StatusOK, p.confirm(p.cust, tok, 5).code, "confirm")
	require.NoError(t, p.credits.Release(context.Background(), idOf(t, id)))

	r := p.sell(p.cashier, p.qrSale(bill, id))
	require.Equal(t, http.StatusInternalServerError, r.code, problemCode(r))
	require.Empty(t, p.credits.captured)
	require.Zero(t, p.a.count(`SELECT count(*) FROM pos_sales WHERE client_id = $1`, uuidOf(t, bill)))
	require.Equal(t, "claimed", p.getClaim(id).body["status"], "rolled back with the sale")
	p.noSecrets(tok)
}

// reverseRefused is the stock writer with every reversal out of range.
type reverseRefused struct{ pos.Stock }

func (reverseRefused) Reverse(context.Context, pgx.Tx, pos.StockReversal) (ids.ID, error) {
	return ids.Zero, pos.ErrStockInvalid
}

// A reversal the stock writer refuses is 409 and reverses nothing.
func TestPOSStockReverseRefused(t *testing.T) {
	p := newPosShopWith(t, func(d *pos.Deps) { d.Stock = reverseRefused{d.Stock} })
	s := p.sell(p.cashier, p.saleBody(p.newID(), line{p.atta, 1}))
	require.Equal(t, http.StatusOK, s.code, "sale: %s", problemCode(s))
	sale := s.body["id"].(string)
	r := p.reverse(p.manager, sale)
	require.Equal(t, http.StatusConflict, r.code, problemCode(r))
	require.Equal(t, "invalid_transition", problemCode(r))
	require.Zero(t, p.a.count(`SELECT count(*) FROM pos_sales WHERE id = $1 AND reversed_at IS NOT NULL`, uuidOf(t, sale)))
	require.Empty(t, p.credits.reversed)
}

// sellRefused is the stock writer with every sale line's client id taken.
type sellRefused struct{ pos.Stock }

func (sellRefused) Sell(context.Context, pgx.Tx, pos.StockSale) (ids.ID, error) {
	return ids.Zero, pos.ErrClientIDReused
}

// A sync entry rejected after its wrong code was counted rolls the failure
// back with the rest of the entry.
func TestPOSRejectedEntryRollsBackFailure(t *testing.T) {
	p := newPosShopWith(t, func(d *pos.Deps) { d.Stock = sellRefused{d.Stock} })
	secret := p.seed(p.cust)
	now := p.a.clk.Now()
	link := map[string]any{"method": "counter_code", "phone": custPhone, "code": wrongCode(testTOTP(secret, now)), "clockOffsetMs": 0}
	e := p.saleEntry(1, now, link, line{p.salt, 1})
	r := p.sync(p.cashier, e)
	require.Equal(t, http.StatusOK, r.code, "sync: status %d", r.code)
	require.Equal(t, []string{"rejected:invalid"}, statuses(r))
	require.Zero(t, p.a.count(`SELECT count(*) FROM counter_code_failures`), "rolled back with the entry")
	require.Zero(t, p.a.count(`SELECT count(*) FROM sync_entries WHERE client_id = $1`, uuidOf(t, e["clientId"].(string))))
	p.noSecrets(custPhone)
}

// An offline offer discount the store's offers give at sold_at is a normal
// sale: no flag and no offline risk. One above it is flagged, and a sale
// both linked offline and over the engine counts once toward the limit.
func TestPOSOfflineOfferMatchesEngine(t *testing.T) {
	p := newPosShop(t)
	p.offerOn("percent", 10, nil) // 10% of ₹290 = ₹29 on the POS channel
	flags := func(rule string) int {
		return p.a.count(`SELECT count(*) FROM risk_flags WHERE subject_type = 'store' AND subject_id = $1 AND rule = $2`, uuidOf(t, p.sid), rule)
	}
	offered := func(seq int, at time.Time, link map[string]any, paise int) map[string]any {
		e := p.saleEntry(seq, at, link, line{p.atta, 1})
		e["sale"].(map[string]any)["lines"].([]any)[0].(map[string]any)["offerDiscountPaise"] = paise
		return e
	}
	unmatched := func(r resp) int {
		return p.a.count(`SELECT count(*) FROM pos_sales WHERE id = $1 AND offline_offer_unmatched`, uuidOf(t, results(r)[0]["saleId"].(string)))
	}
	r := p.sync(p.cashier, offered(1, p.a.clk.Now(), nil, 2900))
	require.Equal(t, []string{"applied"}, statuses(r), "status %d", r.code)
	require.Zero(t, unmatched(r))
	require.Zero(t, flags("pos.offline_offer_without_discount_role"), "the store's own offer")

	r = p.sync(p.cashier, offered(2, p.a.clk.Now(), nil, 3000))
	require.Equal(t, []string{"applied"}, statuses(r), "status %d", r.code)
	require.Equal(t, 1, unmatched(r))
	require.Equal(t, 1, flags("pos.offline_offer_without_discount_role"), "₹1 over the engine")

	// 19 more, each linked offline and over the engine: 20 sales, once each.
	secret := p.seed(p.cust)
	now := p.a.clk.Now()
	var es []any
	for i := range 19 {
		at := now.Add(-time.Duration(i+1) * time.Minute)
		link := map[string]any{"method": "counter_code", "phone": custPhone, "code": testTOTP(secret, at), "clockOffsetMs": 0}
		es = append(es, offered(i+3, at, link, 3000))
	}
	r = p.sync(p.manager, es...)
	require.Equal(t, http.StatusOK, r.code)
	for i, s := range linkStatuses(r) {
		require.Equal(t, "linked", s, "entry %d", i+1)
	}
	require.Zero(t, flags("pos.offline_links"), "20 sales, not 39 counts")
	r = p.sync(p.manager, offered(30, p.a.clk.Now(), nil, 3000))
	require.Equal(t, []string{"applied"}, statuses(r), "status %d", r.code)
	require.Equal(t, 1, flags("pos.offline_links"), "the 21st")
	p.noSecrets(custPhone)
}
