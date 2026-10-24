package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP is HMAC-SHA1 (D-12)
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/pos"
)

// The W2-04 tests (SC-POS, 08-testing.md): the real router, the catalogue's
// stock writer and the pos service on a fresh database. Credits and
// referrals are recording fakes behind the pos seams (W2-01 and W2-07 wire
// the real ones), swapped in through posSeams before the router is built.
// Codes, claim tokens, receipt tokens and phones are read into variables and
// never printed: a failing assertion on a response that may carry one names
// the status only.

// fakeCredits is a wallet with a balance that records what the pos service
// asked of it.
type fakeCredits struct {
	mu       sync.Mutex
	balance  int
	holds    map[ids.ID]int // claim → credits held
	captured []ids.ID       // claims captured by a sale
	released []ids.ID       // claims whose hold was given back, once each
	cashback []pos.Cashback
	reversed []ids.ID         // sales whose credits were reversed
	poison   map[ids.ID]error // claims whose release fails
}

func (f *fakeCredits) Allow(_ context.Context, _, _ ids.ID, requested int, _ int64) (pos.CreditsAllowance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.balance == 0 {
		return pos.CreditsAllowance{Limit: "no_balance"}, nil
	}
	return pos.CreditsAllowance{Allowed: min(requested, f.balance)}, nil
}

func (f *fakeCredits) Hold(_ context.Context, _, _, claimID ids.ID, credits int, _ int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.holds[claimID] = credits
	f.balance -= credits
	return credits, nil
}

func (f *fakeCredits) Capture(_ context.Context, claimID, _, _ ids.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.Contains(f.released, claimID) {
		return pos.ErrHoldReleased
	}
	f.captured = append(f.captured, claimID)
	delete(f.holds, claimID) // spent
	return nil
}

func (f *fakeCredits) Release(_ context.Context, claimID ids.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.poison[claimID]; err != nil {
		return err
	}
	if !slices.Contains(f.released, claimID) {
		f.released = append(f.released, claimID)
	}
	f.balance += f.holds[claimID]
	delete(f.holds, claimID)
	return nil
}

func (f *fakeCredits) Cashback(_ context.Context, in pos.Cashback) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cashback = append(f.cashback, in)
	return fakeCashback(in), nil
}

// fakeCashback is the ledger's cashback: CashbackBP of the paise paid, in
// whole credits.
func fakeCashback(in pos.Cashback) int { return int(in.PaidPaise * int64(in.CashbackBP) / 1_000_000) }

func (f *fakeCredits) ReverseSale(_ context.Context, r pos.SaleReversal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reversed = append(f.reversed, r.SaleID)
	return nil
}

type qualified struct {
	customer, sale ids.ID
	paise          int64 // the qualifying amount: eligible less credits used
	method         string
}

// fakeReferrals records qualifications and revocations.
type fakeReferrals struct {
	mu        sync.Mutex
	qualified []qualified
	revoked   []ids.ID // sales
}

func (f *fakeReferrals) Qualify(_ context.Context, p pos.Purchase) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.qualified = append(f.qualified, qualified{p.CustomerID, p.SaleID, p.EligiblePaise - int64(p.CreditsUsed)*100, p.LinkMethod})
	return nil
}

func (f *fakeReferrals) Revoke(_ context.Context, _, saleID ids.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, saleID)
	return nil
}

// posShop is the orders fixture with the recording seams.
type posShop struct {
	*orderShop
	credits *fakeCredits
	refs    *fakeReferrals
}

func newPosShop(t *testing.T) *posShop {
	t.Helper()
	return newPosShopWith(t, nil)
}

// newPosShopWith is newPosShop with more seams set after the recording ones.
func newPosShopWith(t *testing.T, more func(*pos.Deps)) *posShop {
	t.Helper()
	fc := &fakeCredits{balance: 50, holds: map[ids.ID]int{}, poison: map[ids.ID]error{}}
	fr := &fakeReferrals{}
	posSeams = func(d *pos.Deps) {
		d.Credits, d.Referrals = fc, fr
		if more != nil {
			more(d)
		}
	}
	t.Cleanup(func() { posSeams = nil })
	return &posShop{orderShop: newOrderShop(t), credits: fc, refs: fr}
}

func (p *posShop) newID() string { return ids.New(p.a.clk.Now()).String() }

// saleLines are POS lines at the items' prices now.
func (p *posShop) saleLines(lines ...line) []any {
	out := make([]any, len(lines))
	for i, l := range lines {
		out[i] = map[string]any{"clientId": p.newID(), "storeItemId": l.item, "unitPricePaise": p.price[l.item], "quantity": l.qty}
	}
	return out
}

func (p *posShop) saleBody(clientID string, lines ...line) map[string]any {
	return map[string]any{"clientId": clientID, "lines": p.saleLines(lines...), "paymentMethod": "cash"}
}

func (p *posShop) sell(tok string, body map[string]any) resp {
	p.t.Helper()
	return p.a.call(tok, http.MethodPost, p.S+"/pos/sales", body, idem(p.a))
}

func (p *posShop) reverse(tok, saleID string) resp {
	p.t.Helper()
	return p.a.call(tok, http.MethodPost, p.S+"/pos/sales/"+saleID+"/reverse", map[string]any{"reason": "rang the wrong item"}, idem(p.a))
}

// cursor is store S's catalogue seq now.
func (p *posShop) cursor() int {
	return p.a.count(`SELECT catalogue_seq FROM stores WHERE id = $1`, uuidOf(p.t, p.sid))
}

func (p *posShop) syncKey(tok string, key map[string]string, cursor int, entries ...any) resp {
	p.t.Helper()
	if entries == nil {
		entries = []any{}
	}
	return p.a.call(tok, http.MethodPost, p.S+"/sync", map[string]any{"cursor": cursor, "entries": entries}, key)
}

func (p *posShop) sync(tok string, entries ...any) resp {
	p.t.Helper()
	return p.syncKey(tok, idem(p.a), p.cursor(), entries...)
}

func (p *posShop) saleEntry(seq int, at time.Time, link map[string]any, lines ...line) map[string]any {
	ls := p.saleLines(lines...)
	for i, l := range lines {
		ls[i].(map[string]any)["name"] = "item " + l.item[:6]
	}
	sale := map[string]any{"lines": ls, "paymentMethod": "upi"}
	if link != nil {
		sale["link"] = link
	}
	return map[string]any{"clientId": p.newID(), "deviceSeq": seq, "kind": "sale", "at": at, "sale": sale}
}

func (p *posShop) stockEntry(seq int, kind, item string, qty int) map[string]any {
	st := map[string]any{"storeItemId": item}
	if kind == "count" {
		st["counted"] = qty
	} else {
		st["quantity"] = qty
	}
	return map[string]any{"clientId": p.newID(), "deviceSeq": seq, "kind": kind, "at": p.a.clk.Now(), "stock": st}
}

func results(r resp) []map[string]any {
	rs, _ := r.body["results"].([]any)
	out := make([]map[string]any, len(rs))
	for i, x := range rs {
		out[i], _ = x.(map[string]any)
	}
	return out
}

func statuses(r resp) []string {
	var out []string
	for _, x := range results(r) {
		s, _ := x["status"].(string)
		if reason, ok := x["reason"].(string); ok {
			s += ":" + reason
		}
		out = append(out, s)
	}
	return out
}

func linkStatuses(r resp) []any {
	var out []any
	for _, x := range results(r) {
		out = append(out, x["linkStatus"])
	}
	return out
}

// seed is the signed-in customer device's counter-code secret.
func (p *posShop) seed(tok string) []byte {
	p.t.Helper()
	r := p.a.call(tok, http.MethodGet, "/v1/me/counter-code-seed", nil, nil)
	require.Equal(p.t, http.StatusOK, r.code, "seed: status %d", r.code)
	require.Equal(p.t, "SHA1", r.body["algorithm"])
	require.EqualValues(p.t, 4, r.body["digits"])
	require.EqualValues(p.t, 60, r.body["periodSeconds"])
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(r.body["secret"].(string))
	require.NoError(p.t, err)
	require.Len(p.t, secret, 20)
	return secret
}

// testTOTP is RFC 6238 at 60 s steps truncated to 4 digits, written apart
// from internal/pos so the test checks the server against the RFC.
func testTOTP(secret []byte, at time.Time) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/60)) //nolint:gosec // after 1970
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	h := m.Sum(nil)
	off := h[len(h)-1] & 0x0f
	bin := binary.BigEndian.Uint32(h[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%04d", bin%10_000)
}

func (p *posShop) verifyCode(tok, phone, code, saleClientID string) resp {
	p.t.Helper()
	return p.a.call(tok, http.MethodPost, p.S+"/counter-code/verify",
		map[string]any{"phone": phone, "code": code, "saleClientId": saleClientID}, idem(p.a))
}

func (p *posShop) claim(tok, saleClientID string, amount int) (id, token string) {
	p.t.Helper()
	r := p.a.call(tok, http.MethodPost, p.S+"/bill-claims", map[string]any{"saleClientId": saleClientID, "amountPaise": amount}, idem(p.a))
	require.Equal(p.t, http.StatusCreated, r.code, "create claim: status %d", r.code)
	token, _ = r.body["token"].(string)
	require.Len(p.t, token, 64, "the token comes back once")
	require.Equal(p.t, "open", r.body["status"])
	return r.body["id"].(string), token
}

func (p *posShop) preview(tok, token string) resp {
	p.t.Helper()
	return p.a.call(tok, http.MethodPost, "/v1/me/bill-claims/"+token+"/preview", nil, nil)
}

func (p *posShop) confirm(tok, token string, credits int) resp {
	p.t.Helper()
	return p.a.call(tok, http.MethodPost, "/v1/me/bill-claims/"+token+"/confirm", map[string]any{"creditsToUse": credits}, idem(p.a))
}

func (p *posShop) getClaim(id string) resp {
	p.t.Helper()
	r := p.a.call(p.cashier, http.MethodGet, p.S+"/bill-claims/"+id, nil, nil)
	require.Equal(p.t, http.StatusOK, r.code, r.raw)
	return r
}

// noSecrets: none of the values reached the API's logs.
func (p *posShop) noSecrets(vals ...string) {
	p.t.Helper()
	logs := p.logs.String()
	for _, v := range vals {
		require.NotEmpty(p.t, v)
		require.False(p.t, strings.Contains(logs, v), "a code, token or phone reached the logs")
	}
}

func (p *posShop) setCashback(bp int) {
	p.a.exec(`UPDATE stores SET cashback_bp = $1 WHERE id = $2`, bp, uuidOf(p.t, p.sid))
}

func mustParse(t *testing.T, s any) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, s.(string))
	require.NoError(t, err)
	return at
}

func idOf(t *testing.T, s any) ids.ID {
	t.Helper()
	id, err := ids.Parse(s.(string))
	require.NoError(t, err)
	return id
}

// --- SC-POS ----------------------------------------------------------------------

// POS-01: an online sale lowers stock with one sale_pos movement per
// catalogued line, and its receipt link shows the bill without PII.
func TestPOS01OnlineSale(t *testing.T) {
	p := newPosShop(t)
	before, _ := p.stock(p.atta)
	body := p.saleBody(p.newID(), line{p.atta, 2}, line{p.salt, 1})
	body["lines"] = append(body["lines"].([]any), map[string]any{"clientId": p.newID(), "name": "Carry bag", "unitPricePaise": 500, "quantity": 1})
	body["expectedTotalPaise"] = 2*29000 + 2500 + 500
	r := p.sell(p.cashier, body)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 2*29000+2500+500, r.body["totalPaise"])
	require.Equal(t, "completed", r.body["status"])
	require.Nil(t, r.body["link"])
	receipt, _ := r.body["receiptToken"].(string)
	require.Len(t, receipt, 22)
	onHand, _ := p.stock(p.atta)
	require.Equal(t, before-2, onHand)
	sale := uuidOf(t, r.body["id"].(string))
	require.Equal(t, 2, p.a.count(`SELECT count(*) FROM stock_movements WHERE source = 'sale_pos' AND ref_type = 'pos_sale' AND ref_id = $1`, sale))
	require.Equal(t, 3, p.a.count(`SELECT count(*) FROM pos_sale_lines WHERE sale_id = $1`, sale))

	// A replay of the same client id is the same sale, without a token.
	again := p.sell(p.cashier, body)
	require.Equal(t, http.StatusOK, again.code, again.raw)
	require.Equal(t, r.body["id"], again.body["id"])
	require.Nil(t, again.body["receiptToken"])
	onHand2, _ := p.stock(p.atta)
	require.Equal(t, onHand, onHand2)

	rc := p.a.call("", http.MethodGet, "/v1/public/receipts/"+receipt, nil, nil)
	require.Equal(t, http.StatusOK, rc.code, "receipt: status %d", rc.code)
	require.Equal(t, "no-store", rc.hdr.Get("Cache-Control"))
	require.Equal(t, "Lakshmi Traders", rc.body["store"].(map[string]any)["name"])
	require.Len(t, rc.body["lines"], 3)
	require.EqualValues(t, 2*29000+2500+500, rc.body["totalPaise"])
	require.NotContains(t, rc.raw, "+91")
	require.NotContains(t, rc.raw, "customer")
	p.noSecrets(receipt)
}

// POS-02: a manager reverses within pos_reversal_window_hours (24 h exactly
// passes, 24 h + 1 s does not), the stock comes back, a cashier may not
// reverse or discount, and a second reversal is refused.
func TestPOS02Reversal(t *testing.T) {
	p := newPosShop(t)

	disc := p.saleBody(p.newID(), line{p.butter, 1})
	disc["lines"].([]any)[0].(map[string]any)["manualDiscountPaise"] = 500
	r := p.sell(p.cashier, disc)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = p.sell(p.manager, disc)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 5000, r.body["totalPaise"])

	before, _ := p.stock(p.atta)
	a := p.sell(p.cashier, p.saleBody(p.newID(), line{p.atta, 3}))
	require.Equal(t, http.StatusOK, a.code, a.raw)
	b := p.sell(p.cashier, p.saleBody(p.newID(), line{p.atta, 1}))
	require.Equal(t, http.StatusOK, b.code, b.raw)
	soldA, soldB := mustParse(t, a.body["soldAt"]), mustParse(t, b.body["soldAt"])

	p.set(soldA.Add(24 * time.Hour))
	r = p.reverse(p.cashier, a.body["id"].(string))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = p.reverse(p.manager, a.body["id"].(string))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "reversed", r.body["status"])
	require.Equal(t, "rang the wrong item", r.body["reversalReason"])
	onHand, _ := p.stock(p.atta)
	require.Equal(t, before-1, onHand, "the reversed sale's 3 are back")
	require.Equal(t, 1, p.a.count(`SELECT count(*) FROM stock_movements WHERE source = 'pos_reversal' AND store_item_id = $1`, uuidOf(t, p.atta)))

	r = p.reverse(p.manager, a.body["id"].(string))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))

	p.set(soldB.Add(24*time.Hour + time.Second))
	r = p.reverse(p.owner, b.body["id"].(string))
	require.Equal(t, "reversal_window_passed", problemCode(r), r.raw)
	onHand, _ = p.stock(p.atta)
	require.Equal(t, before-1, onHand)

	// No customer, no ledger.
	require.Empty(t, p.credits.reversed)
	require.Empty(t, p.refs.revoked)
}

// POS-04: two devices sell the last unit offline; both sales apply, stock
// goes to -1 and the item comes back countNeeded (02 §4.3).
func TestPOS04OversellOffline(t *testing.T) {
	p := newPosShop(t)
	r := p.sync(p.manager, p.stockEntry(1, "damage", p.atta, 499))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, []string{"applied"}, statuses(r))
	onHand, _ := p.stock(p.atta)
	require.Equal(t, 1, onHand)

	at := p.a.clk.Now().Add(-10 * time.Minute) // both rung before either synced
	first := p.sync(p.cashier, p.saleEntry(1, at, nil, line{p.atta, 1}))
	require.Equal(t, []string{"applied"}, statuses(first), first.raw)
	second := p.sync(p.manager, p.saleEntry(2, at.Add(time.Minute), nil, line{p.atta, 1}))
	require.Equal(t, []string{"applied"}, statuses(second), second.raw)

	onHand, _ = p.stock(p.atta)
	require.Equal(t, -1, onHand)
	var found bool
	for _, c := range second.body["changes"].([]any) {
		it, _ := c.(map[string]any)["item"].(map[string]any)
		if it != nil && it["id"] == p.atta {
			found = true
			require.EqualValues(t, -1, it["onHand"])
			require.Equal(t, true, it["countNeeded"])
		}
	}
	require.True(t, found, "the sync returns the item's change")
	// The sold price is recorded as sent.
	require.Equal(t, 2, p.a.count(`SELECT count(*) FROM pos_sale_lines l JOIN pos_sales s ON s.id = l.sale_id
	  WHERE s.store_id = $1 AND l.unit_price_paise = 29000`, uuidOf(t, p.sid)))
}

// POS-05: a batch sent again with the same key is the stored answer; with a
// new key each entry is a duplicate of what it made, and nothing moves twice.
func TestPOS05DuplicateBatch(t *testing.T) {
	p := newPosShop(t)
	at := p.a.clk.Now().Add(-time.Hour)
	entries := []any{p.saleEntry(1, at, nil, line{p.atta, 1}), p.stockEntry(2, "restock", p.butter, 10)}
	cur := p.cursor()
	key := idem(p.a)
	r1 := p.syncKey(p.manager, key, cur, entries...)
	require.Equal(t, http.StatusOK, r1.code, r1.raw)
	require.Equal(t, []string{"applied", "applied"}, statuses(r1))
	r2 := p.syncKey(p.manager, key, cur, entries...)
	require.Equal(t, http.StatusOK, r2.code, r2.raw)
	require.Equal(t, r1.body["results"], r2.body["results"])

	r3 := p.syncKey(p.manager, idem(p.a), cur, entries...)
	require.Equal(t, http.StatusOK, r3.code, r3.raw)
	require.Equal(t, []string{"duplicate", "duplicate"}, statuses(r3))
	require.Equal(t, results(r1)[0]["saleId"], results(r3)[0]["saleId"])
	require.Equal(t, results(r1)[1]["movementId"], results(r3)[1]["movementId"])
	require.Nil(t, results(r3)[0]["receiptToken"], "a duplicate never repeats the token")

	// Another device replaying the same outbox is a duplicate too.
	r4 := p.syncKey(p.owner, idem(p.a), cur, entries...)
	require.Equal(t, []string{"duplicate", "duplicate"}, statuses(r4))

	atta, _ := p.stock(p.atta)
	butter, _ := p.stock(p.butter)
	require.Equal(t, 499, atta)
	require.Equal(t, 510, butter)
	require.Equal(t, 1, p.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1`, uuidOf(t, p.sid)))
}

// POS-06: a device clock that is wrong keeps the device's sold_at; the
// server's time is received_at.
func TestPOS06DeviceClock(t *testing.T) {
	p := newPosShop(t)
	now := p.a.clk.Now()
	for _, at := range []time.Time{now.Add(-5 * time.Hour), now.Add(2 * time.Hour), now.Add(-80 * time.Hour)} {
		r := p.sync(p.cashier, p.saleEntry(1, at, nil, line{p.salt, 1}))
		require.Equal(t, []string{"applied"}, statuses(r), r.raw)
		var sold, received time.Time
		require.NoError(t, p.a.tdb.OwnerPool.QueryRow(context.Background(),
			`SELECT sold_at, received_at FROM pos_sales WHERE id = $1`, uuidOf(t, results(r)[0]["saleId"].(string))).Scan(&sold, &received))
		require.True(t, received.Equal(p.a.clk.Now()), "received_at is the server's")
		switch {
		case at.After(now):
			require.True(t, sold.Equal(received), "a device ahead of the server is clamped to received_at (D-131)")
		case now.Sub(at) > 72*time.Hour:
			require.True(t, sold.Equal(received.Add(-72*time.Hour)), "at most 72 h before received_at (01 §17)")
		default:
			require.True(t, sold.Equal(at), "sold_at is the device's time")
		}
	}
}

// POS-08: a bill QR can be confirmed for 120 s; at 120 s it is expired.
func TestPOS08ClaimExpiry(t *testing.T) {
	p := newPosShop(t)
	_, live := p.claim(p.cashier, p.newID(), 29000)
	id, tok := p.claim(p.cashier, p.newID(), 29000)
	p.a.clk.Advance(119 * time.Second)
	r := p.preview(p.cust, live)
	require.Equal(t, http.StatusOK, r.code, "preview at 119 s: status %d", r.code)
	require.EqualValues(t, 29000, r.body["amountPaise"])
	require.EqualValues(t, 30, r.body["maxCredits"], "credit_max_per_order")

	p.a.clk.Advance(time.Second)
	r = p.preview(p.cust, tok)
	require.Equal(t, "claim_expired", problemCode(r), "preview at 120 s: status %d", r.code)
	r = p.confirm(p.cust, tok, 0)
	require.Equal(t, "claim_expired", problemCode(r), "confirm at 120 s: status %d", r.code)
	require.Equal(t, "expired", p.getClaim(id).body["status"])
	require.Nil(t, p.getClaim(id).body["token"], "the token comes back on create only")
	require.Empty(t, p.credits.holds)

	// A token that does not verify is a plain 404.
	bad := []byte(tok)
	bad[60] ^= 1
	r = p.preview(p.cust, string(bad))
	require.Equal(t, http.StatusNotFound, r.code, "tampered token: status %d", r.code)
	p.noSecrets(tok, live)
}

// POS-09: two phones confirm one QR at once; one wins, the other is
// claim_already_used, and credits are held once.
func TestPOS09ClaimRace(t *testing.T) {
	p := newPosShop(t)
	for range 5 {
		id, tok := p.claim(p.cashier, p.newID(), 29000)
		var r1, r2 resp
		parallel(
			func() { r1 = p.confirm(p.cust, tok, 10) },
			func() { r2 = p.confirm(p.cust2, tok, 10) },
		)
		codes := []int{r1.code, r2.code}
		require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, codes, "one confirm wins")
		loser := r1
		if r1.code == http.StatusOK {
			loser = r2
		}
		require.Equal(t, "claim_already_used", problemCode(loser))
		c := p.getClaim(id)
		require.Equal(t, "claimed", c.body["status"])
		require.EqualValues(t, 10, c.body["creditsUsed"])
		require.Equal(t, 10, p.credits.holds[idOf(t, id)])
	}
	require.Len(t, p.credits.holds, 5)
}

// POS-11: a counter code verified at the till links the sale to the account;
// cashback and the referral go through the seams.
func TestPOS11CounterCode(t *testing.T) {
	p := newPosShop(t)
	p.setCashback(200)
	secret := p.seed(p.cust)
	require.Equal(t, secret, p.seed(p.cust), "the same device gets the same secret")
	code := testTOTP(secret, p.a.clk.Now())
	bill := p.newID()
	r := p.verifyCode(p.cashier, custPhone, code, bill)
	require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
	require.Equal(t, bill, r.body["saleClientId"])
	r = p.verifyCode(p.cashier, custPhone, code, bill)
	require.Equal(t, http.StatusOK, r.code, "a verified bill answers again: status %d", r.code)
	r = p.verifyCode(p.cashier, custPhone, code, p.newID())
	require.Equal(t, "code_invalid", problemCode(r), "a used code is wrong for another bill: status %d", r.code)

	body := p.saleBody(bill, line{p.atta, 1})
	body["link"] = map[string]any{"method": "counter_code"}
	s := p.sell(p.cashier, body)
	require.Equal(t, http.StatusOK, s.code, s.raw)
	lk := s.body["link"].(map[string]any)
	require.Equal(t, "counter_code", lk["method"])
	require.Equal(t, "linked", lk["status"])
	require.EqualValues(t, 5, s.body["cashbackCredits"], "2% of ₹290, whole credits")
	saleID := idOf(t, s.body["id"])
	require.Len(t, p.credits.cashback, 1)
	require.Equal(t, 5, fakeCashback(p.credits.cashback[0]))
	require.Equal(t, saleID, p.credits.cashback[0].SaleID)
	require.Len(t, p.refs.qualified, 1)
	require.EqualValues(t, 29000, p.refs.qualified[0].paise)

	// A counter-code link with no verified check is refused.
	other := p.saleBody(p.newID(), line{p.salt, 1})
	other["link"] = map[string]any{"method": "counter_code"}
	r = p.sell(p.cashier, other)
	require.Equal(t, "code_invalid", problemCode(r), r.raw)
	require.EqualValues(t, 5, ext(r, "attemptsLeft"))

	// Reversing a linked sale returns cashback and revokes the referral.
	rv := p.reverse(p.manager, saleID.String())
	require.Equal(t, http.StatusOK, rv.code, rv.raw)
	require.Equal(t, []ids.ID{saleID}, p.credits.reversed)
	require.Equal(t, []ids.ID{saleID}, p.refs.revoked)
	p.noSecrets(code, custPhone)
}

// POS-13: five wrong codes lock the bill; then even the right code is refused.
func TestPOS13CodeLockout(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	right := testTOTP(secret, p.a.clk.Now())
	wrong := wrongCode(right)
	bill := p.newID()
	for left := 4; left >= 1; left-- {
		r := p.verifyCode(p.cashier, custPhone, wrong, bill)
		require.Equal(t, "code_invalid", problemCode(r), "status %d", r.code)
		require.EqualValues(t, left, ext(r, "attemptsLeft"))
	}
	r := p.verifyCode(p.cashier, custPhone, wrong, bill)
	require.Equal(t, "code_locked", problemCode(r), "status %d", r.code)
	r = p.verifyCode(p.cashier, custPhone, right, bill)
	require.Equal(t, "code_locked", problemCode(r), "status %d", r.code)
	body := p.saleBody(bill, line{p.salt, 1})
	body["link"] = map[string]any{"method": "counter_code"}
	r = p.sell(p.cashier, body)
	require.Equal(t, "code_locked", problemCode(r), r.raw)

	// A phone with no account counts as a wrong code (D-12).
	bill2 := p.newID()
	r = p.verifyCode(p.cashier, "+919876500001", right, bill2)
	require.Equal(t, "code_invalid", problemCode(r), "status %d", r.code)
	require.EqualValues(t, 4, ext(r, "attemptsLeft"))
	p.noSecrets(right, wrong, custPhone)
}

// POS-14: every linked sale goes to referrals with its qualifying amount;
// referrals applies ref_qualifying_min_order_paise (₹100) itself, since a
// shop's count takes any amount (D-165, D-132). The real threshold is
// TestPOSLedgerReferralDB's.
func TestPOS14ReferralQualifies(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	for _, qty := range []int{3, 4} { // ₹75, ₹100
		p.a.clk.Advance(61 * time.Second) // a new step: a code is used once
		bill := p.newID()
		r := p.verifyCode(p.cashier, custPhone, testTOTP(secret, p.a.clk.Now()), bill)
		require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
		body := p.saleBody(bill, line{p.salt, qty})
		body["link"] = map[string]any{"method": "counter_code"}
		s := p.sell(p.cashier, body)
		require.Equal(t, http.StatusOK, s.code, s.raw)
	}
	require.Len(t, p.refs.qualified, 2)
	require.EqualValues(t, 7500, p.refs.qualified[0].paise)
	require.EqualValues(t, 10000, p.refs.qualified[1].paise)
	require.Equal(t, "counter_code", p.refs.qualified[1].method)
	require.Empty(t, p.credits.cashback, "cashback_bp is 0 by default")
}

// POS-17: a typed phone is pending verification: no account, no ledger, not
// in purchases, and never returned or logged.
func TestPOS17PhoneUnverified(t *testing.T) {
	p := newPosShop(t)
	p.setCashback(500)
	body := p.saleBody(p.newID(), line{p.atta, 1})
	body["link"] = map[string]any{"method": "phone_unverified", "phone": custPhone}
	r := p.sell(p.cashier, body)
	require.Equal(t, http.StatusOK, r.code, "sale: status %d", r.code)
	lk := r.body["link"].(map[string]any)
	require.Equal(t, "pending_verification", lk["status"])
	require.Nil(t, lk["customerName"])
	require.NotContains(t, r.raw, custPhone)
	require.Equal(t, 1, p.a.count(`SELECT count(*) FROM pos_sales WHERE id = $1 AND customer_id IS NULL
	  AND link_phone_hash IS NOT NULL AND link_phone_enc IS NOT NULL`, uuidOf(t, r.body["id"].(string))))
	require.Empty(t, p.credits.cashback)
	require.Empty(t, p.refs.qualified)

	pr := p.a.call(p.cust, http.MethodGet, "/v1/me/purchases", nil, nil)
	require.Equal(t, http.StatusOK, pr.code, pr.raw)
	require.Empty(t, pr.body["items"])

	rc := p.a.call("", http.MethodGet, "/v1/public/receipts/"+r.body["receiptToken"].(string), nil, nil)
	require.Equal(t, http.StatusOK, rc.code, "receipt: status %d", rc.code)
	require.NotContains(t, rc.raw, custPhone)
	require.NotContains(t, rc.raw, custPhone[3:])

	// Offline, the same.
	s := p.sync(p.cashier, p.saleEntry(1, p.a.clk.Now(), map[string]any{"method": "phone_unverified", "phone": custPhone}, line{p.salt, 1}))
	require.Equal(t, []string{"applied"}, statuses(s), "sync: status %d", s.code)
	require.Nil(t, results(s)[0]["linkStatus"])
	p.noSecrets(custPhone)
}

// POS-22: an offline counter code links within pos_offline_link_max_hours
// (12 h) and a clock offset of 2 min, typed no later than a step ahead of
// the server, and once only.
func TestPOS22OfflineCodeBoundaries(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	now := p.a.clk.Now()
	codeLink := func(trueAt time.Time, offset time.Duration) map[string]any {
		return map[string]any{"method": "counter_code", "phone": custPhone, "code": testTOTP(secret, trueAt), "clockOffsetMs": offset.Milliseconds()}
	}
	a := now.Add(-(11*time.Hour + 59*time.Minute))
	c := now.Add(-10 * time.Minute)
	d := now.Add(-20 * time.Minute)
	b := now.Add(-(12*time.Hour + time.Minute))
	f := now.Add(5 * time.Minute)
	e1 := p.saleEntry(1, a, codeLink(a, 0), line{p.salt, 1})
	e2 := p.saleEntry(2, b, codeLink(b, 0), line{p.salt, 1})
	e3 := p.saleEntry(3, c.Add(119*time.Second), codeLink(c, 119*time.Second), line{p.salt, 1}) // device 1 m 59 s ahead
	e4 := p.saleEntry(4, d.Add(121*time.Second), codeLink(d, 121*time.Second), line{p.salt, 1}) // 2 m 1 s
	e5 := p.saleEntry(5, a, codeLink(a, 0), line{p.salt, 1})                                    // the code of e1 again
	e6 := p.saleEntry(6, f, codeLink(f, 0), line{p.salt, 1})                                    // typed in the future
	r := p.sync(p.cashier, e1, e2, e3, e4, e5, e6)
	require.Equal(t, http.StatusOK, r.code, "sync: status %d", r.code)
	require.Equal(t, []string{"applied", "applied", "applied", "applied", "applied", "applied"}, statuses(r), "sales always apply")
	require.Equal(t, []any{"linked", "failed", "linked", "failed", "failed", "failed"}, linkStatuses(r))

	var offlineAt time.Time
	var skew int
	require.NoError(t, p.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT offline_link_at, device_clock_skew_ms FROM pos_sales WHERE id = $1`, uuidOf(t, results(r)[2]["saleId"].(string))).Scan(&offlineAt, &skew))
	require.True(t, offlineAt.Equal(c), "the code's time is the device's less its offset")
	require.Equal(t, 119_000, skew)
	require.Len(t, p.credits.cashback, 0)
	require.Len(t, p.refs.qualified, 2, "the linked sales go to referrals, which applies the minimum")
	for _, q := range p.refs.qualified {
		require.EqualValues(t, 2500, q.paise)
	}
	require.Equal(t, 2, p.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1 AND link_status = 'linked' AND customer_id IS NOT NULL`, uuidOf(t, p.sid)))
	for _, x := range results(r) {
		require.NotContains(t, fmt.Sprint(x), custPhone)
	}
	p.noSecrets(custPhone, e1["sale"].(map[string]any)["link"].(map[string]any)["code"].(string))
}

// POS-23: more than pos_offline_links_risk_per_day (20) offline links in a
// day flag the store once.
func TestPOS23OfflineLinkRisk(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	now := p.a.clk.Now()
	flags := func() int {
		return p.a.count(`SELECT count(*) FROM risk_flags WHERE subject_type = 'store' AND subject_id = $1 AND rule = 'pos.offline_links'`, uuidOf(t, p.sid))
	}
	k := 0
	batch := func(n int) []any {
		var es []any
		for i := range n {
			k++
			at := now.Add(-time.Duration(2*k) * time.Minute) // 2 steps apart: never a neighbour's code
			es = append(es, p.saleEntry(i+1, at, map[string]any{"method": "counter_code", "phone": custPhone, "code": testTOTP(secret, at), "clockOffsetMs": 0}, line{p.salt, 1}))
		}
		return es
	}
	r := p.sync(p.cashier, batch(20)...)
	require.Equal(t, http.StatusOK, r.code, "sync: status %d", r.code)
	for _, s := range linkStatuses(r) {
		require.Equal(t, "linked", s)
	}
	require.Zero(t, flags(), "20 is the limit, not past it")
	r = p.sync(p.cashier, batch(1)...)
	require.Equal(t, []any{"linked"}, linkStatuses(r))
	require.Equal(t, 1, flags())
	r = p.sync(p.cashier, batch(1)...)
	require.Equal(t, []any{"linked"}, linkStatuses(r))
	require.Equal(t, 1, flags(), "once a day")
}

// POS-24: a cursor is good while its change and the device's last sync are
// within 30 days; older, unknown or ahead of the store is 410 resync_required.
func TestPOS24ResyncRequired(t *testing.T) {
	p := newPosShop(t)
	cur := p.cursor()
	r := p.syncKey(p.manager, idem(p.a), cur)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, cur, r.body["cursor"])
	require.Equal(t, false, r.body["hasMore"])
	r = p.sync(p.manager, p.stockEntry(1, "restock", p.salt, 5))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, cur+1, r.body["cursor"])
	require.Len(t, r.body["changes"], 1)

	sid := uuidOf(t, p.sid)
	age := func(changes, synced time.Duration) {
		now := p.a.clk.Now()
		p.a.exec(`UPDATE catalogue_changes SET created_at = $1 WHERE store_id = $2 AND seq <= $3`, now.Add(-changes), sid, cur)
		p.a.exec(`UPDATE sync_cursors SET last_synced_at = $1 WHERE store_id = $2`, now.Add(-synced), sid)
	}
	day := 24 * time.Hour
	age(29*day, 29*day)
	r = p.syncKey(p.manager, idem(p.a), cur)
	require.Equal(t, http.StatusOK, r.code, "29 days: %s", r.raw)
	require.Len(t, r.body["changes"], 1)

	age(30*day+time.Second, time.Hour)
	r = p.syncKey(p.manager, idem(p.a), cur)
	require.Equal(t, http.StatusGone, r.code, r.raw)
	require.Equal(t, "resync_required", problemCode(r))

	age(time.Hour, 30*day+time.Second)
	r = p.syncKey(p.manager, idem(p.a), p.cursor())
	require.Equal(t, http.StatusGone, r.code, "a device silent for 30 days: %s", r.raw)

	r = p.syncKey(p.owner, idem(p.a), p.cursor()+5)
	require.Equal(t, http.StatusGone, r.code, "ahead of the store: %s", r.raw)
	r = p.syncKey(p.owner, idem(p.a), p.cursor())
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

// POS-25: a receipt link is 404 for an unknown token and after 30 days.
func TestPOS25ReceiptExpiry(t *testing.T) {
	p := newPosShop(t)
	r := p.sell(p.cashier, p.saleBody(p.newID(), line{p.salt, 1}))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	tok := r.body["receiptToken"].(string)
	get := func(tok string) int { return p.a.call("", http.MethodGet, "/v1/public/receipts/"+tok, nil, nil).code }
	require.Equal(t, http.StatusNotFound, get("AAAAAAAAAAAAAAAAAAAAAA"))
	require.Equal(t, http.StatusOK, get(tok))
	p.a.clk.Advance(30*24*time.Hour - time.Second)
	require.Equal(t, http.StatusOK, get(tok))
	p.a.clk.Advance(time.Second)
	require.Equal(t, http.StatusNotFound, get(tok))
	p.noSecrets(tok)
}

// POS-26: purchases are the customer's linked sales, newest first, by pages,
// and no one else's.
func TestPOS26Purchases(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	now := p.a.clk.Now()
	var es []any
	for i, back := range []time.Duration{3 * time.Hour, time.Hour, 2 * time.Hour} {
		at := now.Add(-back)
		es = append(es, p.saleEntry(i+1, at, map[string]any{"method": "counter_code", "phone": custPhone, "code": testTOTP(secret, at), "clockOffsetMs": 0}, line{p.butter, i + 1}))
	}
	r := p.sync(p.cashier, es...)
	require.Equal(t, []any{"linked", "linked", "linked"}, linkStatuses(r))
	sale := func(i int) string { return results(r)[i]["saleId"].(string) }

	pr := p.a.call(p.cust, http.MethodGet, "/v1/me/purchases?limit=2", nil, nil)
	require.Equal(t, http.StatusOK, pr.code, pr.raw)
	items := pr.body["items"].([]any)
	require.Len(t, items, 2)
	require.Equal(t, sale(1), items[0].(map[string]any)["id"])
	require.Equal(t, sale(2), items[1].(map[string]any)["id"])
	require.Equal(t, "pos_sale", items[0].(map[string]any)["kind"])
	require.EqualValues(t, 2*5500, items[0].(map[string]any)["totalPaise"])
	next := pr.body["nextCursor"].(string)
	pr = p.a.call(p.cust, http.MethodGet, "/v1/me/purchases?limit=2&cursor="+next, nil, nil)
	require.Equal(t, http.StatusOK, pr.code, pr.raw)
	items = pr.body["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, sale(0), items[0].(map[string]any)["id"])
	require.Nil(t, pr.body["nextCursor"])

	pr = p.a.call(p.cust2, http.MethodGet, "/v1/me/purchases", nil, nil)
	require.Equal(t, http.StatusOK, pr.code, pr.raw)
	require.Empty(t, pr.body["items"])
}

// POS-27: under credit_min_order_value_paise (₹100) a bill takes no credits.
func TestPOS27CreditMinimum(t *testing.T) {
	p := newPosShop(t)
	_, tok := p.claim(p.cashier, p.newID(), 9900)
	r := p.preview(p.cust, tok)
	require.Equal(t, http.StatusOK, r.code, "preview: status %d", r.code)
	require.EqualValues(t, 0, r.body["maxCredits"])
	require.Equal(t, "below_credit_minimum", r.body["creditsLimit"])
	r = p.confirm(p.cust, tok, 5)
	require.Equal(t, http.StatusOK, r.code, "confirm: status %d", r.code)
	require.EqualValues(t, 0, r.body["creditsUsed"])
	require.EqualValues(t, 9900, r.body["payablePaise"])
	require.Empty(t, p.credits.holds)

	// At ₹100 the credits leave ₹1 to pay.
	_, tok = p.claim(p.cashier, p.newID(), 10000)
	r = p.preview(p.cust2, tok)
	require.EqualValues(t, 30, r.body["maxCredits"])
	r = p.confirm(p.cust2, tok, 99)
	require.Equal(t, http.StatusOK, r.code, "confirm: status %d", r.code)
	require.EqualValues(t, 30, r.body["creditsUsed"], "credit_max_per_order")
	require.EqualValues(t, 7000, r.body["payablePaise"])
}

// The bill QR flow end to end: confirm holds credits, the sale captures them
// and binds the claim, and a mismatched or unconfirmed claim is refused.
func TestPOSBillQRSale(t *testing.T) {
	p := newPosShop(t)
	bill := p.newID()
	id, tok := p.claim(p.cashier, bill, 29000)
	linked := func(clientID string, claimID string) map[string]any {
		b := p.saleBody(clientID, line{p.atta, 1})
		b["link"] = map[string]any{"method": "bill_qr", "claimId": claimID}
		return b
	}
	r := p.sell(p.cashier, linked(bill, id))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "conflict", problemCode(r), "not yet confirmed")

	c := p.confirm(p.cust, tok, 10)
	require.Equal(t, http.StatusOK, c.code, "confirm: status %d", c.code)
	require.EqualValues(t, 10, c.body["creditsUsed"])
	require.EqualValues(t, 28000, c.body["payablePaise"])
	require.Equal(t, 10, p.credits.holds[idOf(t, id)])
	again := p.confirm(p.cust, tok, 10)
	require.Equal(t, "claim_already_used", problemCode(again), "status %d", again.code)

	r = p.sell(p.cashier, linked(p.newID(), id))
	require.Equal(t, "claim_mismatch", problemCode(r), r.raw)
	more := linked(bill, id)
	more["lines"] = p.saleLines(line{p.atta, 2})
	r = p.sell(p.cashier, more)
	require.Equal(t, "claim_mismatch", problemCode(r), r.raw)
	_, otherTok := p.claim(p.cashier, p.newID(), 100)
	r = p.sell(p.cashier, linked(bill, p.newID()))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.link.claimId")

	p.a.clk.Advance(119 * time.Second) // a confirmed claim binds within its 120 s (D-131)
	s := p.sell(p.cashier, linked(bill, id))
	require.Equal(t, http.StatusOK, s.code, s.raw)
	require.EqualValues(t, 10, s.body["creditsUsed"])
	require.EqualValues(t, 28000, s.body["totalPaise"])
	require.Equal(t, "bill_qr", s.body["link"].(map[string]any)["method"])
	require.Equal(t, []ids.ID{idOf(t, id)}, p.credits.captured)
	g := p.getClaim(id)
	require.Equal(t, "used", g.body["status"])
	require.Equal(t, s.body["id"], g.body["posSaleId"])
	require.Equal(t, 2, p.a.count(`SELECT count(*) FROM outbox_events WHERE payload->>'type' = 'bill_claim.updated' AND payload->>'id' = $1`, id))

	r = p.sell(p.cashier, linked(p.newID(), id))
	require.Equal(t, "claim_already_used", problemCode(r), r.raw)

	// The reversal returns the spent credits.
	rv := p.reverse(p.manager, s.body["id"].(string))
	require.Equal(t, http.StatusOK, rv.code, rv.raw)
	require.Equal(t, []ids.ID{idOf(t, s.body["id"])}, p.credits.reversed)
	p.noSecrets(tok, otherTok)
}

// TOTP drift over HTTP: a code one step behind or ahead of the server
// verifies; two steps off does not.
func TestPOSCodeDrift(t *testing.T) {
	p := newPosShop(t)
	secret := p.seed(p.cust)
	now := p.a.clk.Now()
	for _, d := range []time.Duration{-time.Minute, time.Minute} {
		r := p.verifyCode(p.cashier, custPhone, testTOTP(secret, now.Add(d)), p.newID())
		require.Equal(t, http.StatusOK, r.code, "drift %v: status %d", d, r.code)
	}
	for _, d := range []time.Duration{-2 * time.Minute, 2 * time.Minute} {
		code := testTOTP(secret, now.Add(d))
		if code == testTOTP(secret, now) || code == testTOTP(secret, now.Add(-time.Minute)) || code == testTOTP(secret, now.Add(time.Minute)) {
			continue // a 1-in-10,000 collision with a step in the window
		}
		r := p.verifyCode(p.cashier, custPhone, code, p.newID())
		require.Equal(t, "code_invalid", problemCode(r), "drift %v: status %d", d, r.code)
	}
}

// POS-28: a bill rung online and then sent again by the offline outbox is a
// duplicate of the online sale.
func TestPOS28OnlineThenSync(t *testing.T) {
	p := newPosShop(t)
	e := p.saleEntry(1, p.a.clk.Now(), nil, line{p.atta, 1})
	body := p.saleBody(e["clientId"].(string), line{p.atta, 1})
	s := p.sell(p.cashier, body)
	require.Equal(t, http.StatusOK, s.code, s.raw)
	r := p.sync(p.cashier, e)
	require.Equal(t, []string{"duplicate"}, statuses(r), r.raw)
	require.Equal(t, s.body["id"], results(r)[0]["saleId"])
	onHand, _ := p.stock(p.atta)
	require.Equal(t, 499, onHand)
}

// POS-29: each offline entry is checked as its own action: a cashier may
// sell and count but not restock, write off damage or expire stock, or
// discount.
func TestPOS29EntryRoles(t *testing.T) {
	p := newPosShop(t)
	disc := p.saleEntry(5, p.a.clk.Now(), nil, line{p.salt, 1})
	disc["sale"].(map[string]any)["billDiscountPaise"] = 100
	r := p.sync(p.cashier,
		p.stockEntry(1, "restock", p.salt, 5),
		p.stockEntry(2, "damage", p.salt, 1),
		p.stockEntry(3, "expired", p.salt, 1),
		p.stockEntry(4, "count", p.salt, 490),
		disc,
		p.saleEntry(6, p.a.clk.Now(), nil, line{p.salt, 2}),
	)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, []string{"rejected:forbidden", "rejected:forbidden", "rejected:forbidden", "applied", "rejected:forbidden", "applied"}, statuses(r))
	onHand, _ := p.stock(p.salt)
	require.Equal(t, 488, onHand)

	r = p.sync(p.manager, p.stockEntry(1, "restock", p.salt, 12), p.stockEntry(2, "count", p.salt, 500))
	require.Equal(t, []string{"applied", "applied"}, statuses(r))
	require.Nil(t, results(r)[1]["movementId"], "a count equal to on hand writes nothing")
	onHand, _ = p.stock(p.salt)
	require.Equal(t, 500, onHand)
}

// POS-30: a stock change for an item that is not this store's is
// item_not_found; a sale line for one is a custom line.
func TestPOS30ItemNotFound(t *testing.T) {
	p := newPosShop(t)
	var product pgtype.UUID
	require.NoError(t, p.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT product_id FROM store_items WHERE id = $1`, uuidOf(t, p.atta)).Scan(&product))
	pid, _ := ids.FromUUID(product)
	r := p.a.call(p.owner2, http.MethodPost, p.SY+"/items", map[string]any{"productId": pid.String(), "pricePaise": 29500, "openingStock": 10}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	foreign := r.body["id"].(string)
	sale := p.saleEntry(3, p.a.clk.Now(), nil, line{p.atta, 1})
	sale["sale"].(map[string]any)["lines"].([]any)[0].(map[string]any)["storeItemId"] = foreign
	r = p.sync(p.manager,
		p.stockEntry(1, "restock", p.newID(), 5),
		p.stockEntry(2, "damage", foreign, 1),
		sale,
	)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, []string{"rejected:item_not_found", "rejected:item_not_found", "applied"}, statuses(r))
	require.Equal(t, 1, p.a.count(`SELECT count(*) FROM pos_sale_lines WHERE sale_id = $1 AND store_item_id IS NULL AND movement_id IS NULL`,
		uuidOf(t, results(r)[2]["saleId"].(string))))
	require.Equal(t, 10, p.a.count(`SELECT on_hand FROM store_items WHERE id = $1`, uuidOf(t, foreign)))

	// Online, another store's item is a field error.
	body := p.saleBody(p.newID(), line{p.atta, 1})
	body["lines"].([]any)[0].(map[string]any)["storeItemId"] = foreign
	r = p.sell(p.cashier, body)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.lines[0].storeItemId")
}

// POS-31: tenancy: a cashier of S2 cannot ring, sync or make a QR at S.
func TestPOS31Tenancy(t *testing.T) {
	p := newPosShop(t)
	r := p.sell(p.cashierS2, p.saleBody(p.newID(), line{p.atta, 1}))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = p.sync(p.cashierS2, p.saleEntry(1, p.a.clk.Now(), nil, line{p.atta, 1}))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = p.a.call(p.cashierS2, http.MethodPost, p.S+"/bill-claims", map[string]any{"saleClientId": p.newID(), "amountPaise": 1000}, idem(p.a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = p.a.call(p.owner2, http.MethodPost, p.S+"/sync", map[string]any{"cursor": 0, "entries": []any{}}, idem(p.a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = p.a.call(p.cust, http.MethodPost, p.S+"/sync", map[string]any{"cursor": 0, "entries": []any{}}, idem(p.a))
	require.Contains(t, []int{http.StatusForbidden, http.StatusUnauthorized}, r.code, r.raw)
	// A claim of S is not found from S2.
	id, _ := p.claim(p.cashier, p.newID(), 1000)
	r = p.a.call(p.cashierS2, http.MethodGet, p.S2+"/bill-claims/"+id, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	onHand, _ := p.stock(p.atta)
	require.Equal(t, 500, onHand)
}

// POS-32: a catalogued line at a stale price, or a total that is not the
// server's, is 409 price_changed with the price to show.
func TestPOS32PriceChanged(t *testing.T) {
	p := newPosShop(t)
	body := p.saleBody(p.newID(), line{p.atta, 1})
	body["lines"].([]any)[0].(map[string]any)["unitPricePaise"] = 28000
	r := p.sell(p.cashier, body)
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "price_changed", problemCode(r))
	require.Equal(t, p.atta, ext(r, "storeItemId"))
	require.EqualValues(t, 29000, ext(r, "pricePaise"))

	body = p.saleBody(p.newID(), line{p.atta, 1})
	body["expectedTotalPaise"] = 28000
	r = p.sell(p.cashier, body)
	require.Equal(t, "price_changed", problemCode(r), r.raw)
	require.EqualValues(t, 29000, ext(r, "totalPaise"))
	require.Zero(t, p.a.count(`SELECT count(*) FROM pos_sales`))

	// Offline, the device's price stands (02 §4.3).
	e := p.saleEntry(1, p.a.clk.Now(), nil, line{p.atta, 1})
	e["sale"].(map[string]any)["lines"].([]any)[0].(map[string]any)["unitPricePaise"] = 28000
	s := p.sync(p.cashier, e)
	require.Equal(t, []string{"applied"}, statuses(s), s.raw)
	require.Equal(t, 1, p.a.count(`SELECT count(*) FROM pos_sales WHERE total_paise = 28000`))
}

// POS-33: entries out of deviceSeq order are refused whole.
func TestPOS33DeviceSeqOrder(t *testing.T) {
	p := newPosShop(t)
	r := p.sync(p.manager, p.stockEntry(2, "restock", p.salt, 1), p.stockEntry(2, "restock", p.salt, 1))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.entries[1].deviceSeq")
	r = p.sync(p.manager, p.stockEntry(3, "restock", p.salt, 1), p.stockEntry(1, "restock", p.salt, 1))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	onHand, _ := p.stock(p.salt)
	require.Equal(t, 500, onHand)
}

// POS-34: concurrent sales each get their own receipt token and lines.
func TestPOS34ConcurrentReceipts(t *testing.T) {
	p := newPosShop(t)
	const n = 10
	rs := make([]resp, n)
	fs := make([]func(), n)
	for i := range n {
		tok := p.cashier
		if i%2 == 1 {
			tok = p.manager
		}
		body := p.saleBody(p.newID(), line{p.butter, 1})
		fs[i] = func() { rs[i] = p.sell(tok, body) }
	}
	parallel(fs...)
	seen := map[string]bool{}
	for _, r := range rs {
		require.Equal(t, http.StatusOK, r.code, r.raw)
		tok := r.body["receiptToken"].(string)
		require.False(t, seen[tok], "receipt tokens are unique")
		seen[tok] = true
	}
	require.Equal(t, n, p.a.count(`SELECT count(DISTINCT token_hash) FROM receipt_tokens`))
	onHand, _ := p.stock(p.butter)
	require.Equal(t, 500-n, onHand)
}

// The sync property: whatever order two devices' outboxes arrive in, in
// whatever batches and with replays, every entry applies once and the stock
// ends the same. Sales, restocks, damage and expiries commute; counts are
// absolute and depend on order, so they are left out.
func TestPOSSyncProperty(t *testing.T) {
	p := newPosShop(t)
	devices := []string{p.manager, p.owner}
	items := []string{p.atta, p.butter, p.salt}
	for trial := range 6 {
		seed := uint64(trial) + 1
		rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // a seeded test interleaving
		want := map[string]int{}
		for _, it := range items {
			want[it], _ = p.stock(it)
		}
		outbox := make([][]any, len(devices))
		total := 0
		for d := range devices {
			for seq := 1; seq <= 3+rng.IntN(6); seq++ {
				it := items[rng.IntN(len(items))]
				qty := 1 + rng.IntN(5)
				switch rng.IntN(4) {
				case 0:
					outbox[d] = append(outbox[d], p.stockEntry(seq, "restock", it, qty))
					want[it] += qty
				case 1:
					kind := []string{"damage", "expired"}[rng.IntN(2)]
					outbox[d] = append(outbox[d], p.stockEntry(seq, kind, it, qty))
					want[it] -= qty
				default:
					outbox[d] = append(outbox[d], p.saleEntry(seq, p.a.clk.Now().Add(-time.Duration(rng.IntN(600))*time.Second), nil, line{it, qty}))
					want[it] -= qty
				}
				total++
			}
		}
		next := make([]int, len(devices))
		var sent [][2]any // (device, batch) already sent
		for next[0] < len(outbox[0]) || next[1] < len(outbox[1]) {
			d := rng.IntN(len(devices))
			if next[d] == len(outbox[d]) {
				d = 1 - d
			}
			n := 1 + rng.IntN(len(outbox[d])-next[d])
			batch := outbox[d][next[d] : next[d]+n]
			next[d] += n
			r := p.sync(devices[d], batch...)
			require.Equal(t, http.StatusOK, r.code, "trial %d: %s", trial, r.raw)
			for _, s := range statuses(r) {
				require.Equal(t, "applied", s, "trial %d", trial)
			}
			sent = append(sent, [2]any{d, batch})
			if rng.IntN(3) == 0 { // a replay of an earlier batch, by either device
				old := sent[rng.IntN(len(sent))]
				r := p.sync(devices[rng.IntN(len(devices))], old[1].([]any)...)
				require.Equal(t, http.StatusOK, r.code, "trial %d replay: %s", trial, r.raw)
				for _, s := range statuses(r) {
					require.Equal(t, "duplicate", s, "trial %d replay", trial)
				}
			}
		}
		for _, it := range items {
			got, _ := p.stock(it)
			require.Equal(t, want[it], got, "trial %d (seed %d): item stock", trial, seed)
		}
		var cids []string
		for _, ob := range outbox {
			for _, e := range ob {
				cids = append(cids, e.(map[string]any)["clientId"].(string))
			}
		}
		require.Equal(t, total, p.a.count(`SELECT count(*) FROM sync_entries WHERE store_id = $1 AND client_id::text = ANY($2)`,
			uuidOf(t, p.sid), uuidStrings(t, cids)), "trial %d: each entry once", trial)
	}
}

func uuidStrings(t *testing.T, ss []string) []string {
	t.Helper()
	out := make([]string, len(ss))
	for i, s := range ss {
		u := uuidOf(t, s)
		out[i] = fmt.Sprintf("%x-%x-%x-%x-%x", u.Bytes[0:4], u.Bytes[4:6], u.Bytes[6:8], u.Bytes[8:10], u.Bytes[10:16])
	}
	return out
}

// W2-07 at the counter: the real offers service prices an online sale on the
// POS channel, the sale redeems the offer in its transaction, a reversal
// gives it back, and an offline sale (priced on the device) redeems nothing.
func TestPOSOffersRedeemAndRelease(t *testing.T) {
	p := newPosShop(t)
	offer := p.offerOn("percent", 10, nil)
	s := p.sell(p.cashier, p.saleBody(p.newID(), line{p.atta, 1})) // ₹290 → 10% = ₹29
	require.Equal(t, http.StatusOK, s.code, s.raw)
	ln := s.body["lines"].([]any)[0].(map[string]any)
	require.EqualValues(t, 2900, ln["offerDiscountPaise"])
	require.EqualValues(t, 26100, s.body["totalPaise"])
	saleID := s.body["id"].(string)
	require.Equal(t, 1, p.a.count(`SELECT count(*) FROM offer_redemptions WHERE offer_id = $1 AND pos_sale_id = $2 AND released_at IS NULL`,
		uuidOf(t, offer), uuidOf(t, saleID)))

	rv := p.reverse(p.manager, saleID)
	require.Equal(t, http.StatusOK, rv.code, rv.raw)
	require.Equal(t, 1, p.redemptions(offer, true))
	require.Zero(t, p.redemptions(offer, false))

	sy := p.sync(p.cashier, p.saleEntry(1, p.a.clk.Now(), nil, line{p.salt, 1}))
	require.Equal(t, []string{"applied"}, statuses(sy), "sync: status %d", sy.code)
	require.Equal(t, 1, p.a.count(`SELECT count(*) FROM offer_redemptions WHERE offer_id = $1`, uuidOf(t, offer)))

	// An offer that has run out no longer prices the bill.
	one := 1
	capped := p.offerOn("flat", 1000, &one)
	a := p.sell(p.cashier, p.saleBody(p.newID(), line{p.salt, 1}))
	require.Equal(t, http.StatusOK, a.code, a.raw)
	b := p.sell(p.cashier, p.saleBody(p.newID(), line{p.salt, 1}))
	require.Equal(t, http.StatusOK, b.code, b.raw)
	require.Equal(t, 1, p.redemptions(capped, false))
	require.Less(t, a.body["totalPaise"].(float64), b.body["totalPaise"].(float64))
}

// W2-06 meets the POS: a customer reviews their counter-code sale once; a
// phone-only sale is not theirs, and a reversed sale is not eligible.
func TestPOSSaleReview(t *testing.T) {
	p := newPosShop(t)
	linked := func() string {
		code := testTOTP(p.seed(p.cust), p.a.clk.Now())
		bill := p.newID()
		require.Equal(t, http.StatusOK, p.verifyCode(p.cashier, custPhone, code, bill).code)
		body := p.saleBody(bill, line{p.atta, 1})
		body["link"] = map[string]any{"method": "counter_code"}
		s := p.sell(p.cashier, body)
		require.Equal(t, http.StatusOK, s.code, s.raw)
		p.a.clk.Advance(2 * time.Minute) // the next code is a new step
		return s.body["id"].(string)
	}
	review := func(saleID string) resp {
		return p.a.call(p.cust, http.MethodPost, "/v1/me/reviews", map[string]any{"posSaleId": saleID, "stars": 5}, nil)
	}
	sale := linked()
	r := review(sale)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "review_exists", problemCode(review(sale)))

	phone := p.saleBody(p.newID(), line{p.salt, 1})
	phone["link"] = map[string]any{"method": "phone_unverified", "phone": custPhone}
	ps := p.sell(p.cashier, phone)
	require.Equal(t, http.StatusOK, ps.code, ps.raw)
	require.Equal(t, http.StatusNotFound, review(ps.body["id"].(string)).code)

	reversed := linked()
	require.Equal(t, http.StatusOK, p.reverse(p.manager, reversed).code)
	require.Equal(t, "review_not_eligible", problemCode(review(reversed)))
}
