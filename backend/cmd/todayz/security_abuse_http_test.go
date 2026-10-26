package main

// W4-03 abuse suite (08-testing.md §7), the cases the domain tests did not
// already cover. Each test signs real principals in through OTP and drives the
// full router (every middleware slot) against testpg.
//
//   - IDOR sweep: every by-id business route with another business's or
//     another store's ids, on the caller's own path (404) and on the owner's
//     path (403), and nothing changes.
//   - Role escalation: a removed member's token stops working at once, an
//     invite is accepted only by its own phone, and tokens do not cross apps.
//   - Pickup-code guessing through another store's route never counts against,
//     or locks, the real order (BK-38).
//   - Bill QR tokens: a forged, truncated or replayed token, and a claim of one
//     store linked into another store's sale.
//   - Referral farming over HTTP: own code, a second claim, the referrer's
//     device and a farmed device that carries several accounts.
//
// Codes, claim tokens and phones are held in variables and never printed: a
// failing assertion on a response that may carry one names the status only.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// swapCase is one by-id route tried by an outsider.
type swapCase struct {
	name   string
	method string
	suffix string // after the store or business path, starting with "/"
	body   any
	hdr    func() map[string]string
	biz    bool // a business-level route ({B}) rather than a store route ({S})
}

func TestAbuseIDORSweepHTTP(t *testing.T) {
	p := newPosShop(t)
	o := p.orderShop
	a := o.a
	BY := strings.Split(o.SY, "/stores/")[0]

	// Resources of business B, store S.
	order := o.order()
	sale := p.sell(p.cashier, p.saleBody(p.newID(), line{o.salt, 1}))
	require.Equal(t, http.StatusOK, sale.code, sale.raw)
	saleID := sale.body["id"].(string)
	claimID, _ := p.claim(p.cashier, p.newID(), 1000)
	off := a.call(o.manager, http.MethodPost, o.S+"/offers", map[string]any{"kind": "percent", "value": 10, "scope": "store"}, idem(a))
	require.Equal(t, http.StatusCreated, off.code, off.raw)
	offerID := off.body["id"].(string)
	staffList := a.call(o.owner, http.MethodGet, o.B+"/staff", nil, nil)
	require.Equal(t, http.StatusOK, staffList.code, staffList.raw)
	cashierStaff := staffIDOf(t, staffList, "cashier")

	itemETag := func() map[string]string {
		r := a.call(o.owner, http.MethodGet, o.S+"/items/"+o.atta, nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		return ifMatch(r)
	}
	offerETag := func() map[string]string { return ifMatch(off) }
	withIdem := func() map[string]string { return idem(a) }
	none := func() map[string]string { return nil }

	cases := []swapCase{
		{"get item", http.MethodGet, "/items/" + o.atta, nil, none, false},
		{"item history", http.MethodGet, "/items/" + o.atta + "/history", nil, none, false},
		{"patch item price", http.MethodPatch, "/items/" + o.atta, map[string]any{"pricePaise": 100}, itemETag, false},
		{"stock movement", http.MethodPost, "/items/" + o.atta + "/movements", map[string]any{"kind": "damage", "quantity": 1}, withIdem, false},
		{"get order", http.MethodGet, "/orders/" + order, nil, none, false},
		{"accept order", http.MethodPost, "/orders/" + order + "/accept", map[string]any{}, withIdem, false},
		{"get bill claim", http.MethodGet, "/bill-claims/" + claimID, nil, none, false},
		{"reverse sale", http.MethodPost, "/pos/sales/" + saleID + "/reverse", map[string]any{"reason": "rang the wrong item"}, withIdem, false},
		{"pause offer", http.MethodPatch, "/offers/" + offerID, map[string]any{"status": "paused"}, offerETag, false},
		{"patch staff", http.MethodPatch, "/staff/" + cashierStaff, map[string]any{"canDeliver": true}, none, true},
		{"remove staff", http.MethodDelete, "/staff/" + cashierStaff, nil, none, true},
	}

	try := func(tok, base string, c swapCase) resp {
		t.Helper()
		return a.call(tok, c.method, base+c.suffix, c.body, c.hdr())
	}
	for _, c := range cases {
		own, foreign := o.SY, o.S
		if c.biz {
			own, foreign = BY, o.B
		}
		// Another business's owner: their own path finds no such resource,
		// and B's path is not theirs.
		r := try(o.owner2, own, c)
		require.Equal(t, http.StatusNotFound, r.code, "%s: B's id under the other business: %s", c.name, r.raw)
		r = try(o.owner2, foreign, c)
		require.Equal(t, http.StatusForbidden, r.code, "%s: the other business on B's path: %s", c.name, r.raw)
		if c.biz {
			continue
		}
		// A cashier of S2 (same business, other store): S2's path finds
		// nothing of S, and S's path is refused.
		r = try(o.cashierS2, o.S2, c)
		require.Contains(t, []int{http.StatusNotFound, http.StatusForbidden}, r.code, "%s: S's id under S2: %s", c.name, r.raw)
		r = try(o.cashierS2, o.S, c)
		require.Equal(t, http.StatusForbidden, r.code, "%s: S2's cashier on S's path: %s", c.name, r.raw)
	}

	// Nothing moved.
	require.Equal(t, "placed", o.status(order))
	onHand, _ := o.stock(o.atta)
	require.Equal(t, 500, onHand, "no movement on atta")
	r := a.call(o.owner, http.MethodGet, o.S+"/items/"+o.atta, nil, nil)
	require.EqualValues(t, 29000, r.body["pricePaise"])
	r = a.call(o.manager, http.MethodGet, o.S+"/offers", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Contains(t, r.raw, `"live"`, "the offer is still live")
	require.Zero(t, a.count(`SELECT count(*) FROM pos_sales WHERE reversed_at IS NOT NULL`))
	r = a.call(o.owner, http.MethodGet, o.B+"/staff", nil, nil)
	require.Equal(t, cashierStaff, staffIDOf(t, r, "cashier"), "the cashier is still a member")
	require.Equal(t, "open", p.getClaim(claimID).body["status"])
}

func TestAbuseRoleEscalationHTTP(t *testing.T) {
	o := newOrderShop(t)
	a := o.a

	// Tokens do not cross apps: a customer token on business and admin
	// routes, a business token on the customer's order routes and on admin.
	for _, c := range []struct {
		name, tok, method, path string
		body                    any
	}{
		{"customer on a store", o.cust, http.MethodGet, o.S + "/orders", nil},
		{"customer on the business", o.cust, http.MethodGet, o.B + "/staff", nil},
		{"customer on admin", o.cust, http.MethodGet, "/v1/admin/jobs", nil},
		{"owner on admin", o.owner, http.MethodGet, "/v1/admin/jobs", nil},
		{"owner places a customer order", o.owner, http.MethodPost, "/v1/me/orders", o.cart([]line{{o.salt, 1}}, 2500)},
		{"cashier reads customer orders", o.cashier, http.MethodGet, "/v1/me/orders", nil},
	} {
		r := a.call(c.tok, c.method, c.path, c.body, idem(a))
		require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, r.code, "%s: %s", c.name, r.raw)
	}

	// An invite is accepted only by the phone it was made for: the cashier
	// cannot take a manager invite meant for someone else.
	inv := a.call(o.owner, http.MethodPost, o.B+"/staff", map[string]any{"phone": "+919000000091", "role": "manager"}, nil)
	require.Equal(t, http.StatusCreated, inv.code, inv.raw)
	r := a.call(o.cashier, http.MethodPost, o.B+"/staff/"+inv.body["id"].(string)+"/accept", nil, nil)
	require.Contains(t, []int{http.StatusNotFound, http.StatusForbidden}, r.code, "another phone's invite: %s", r.raw)
	r = a.call(o.cashier, http.MethodPost, o.B+"/staff", map[string]any{"phone": "+919000000092", "role": "cashier", "storeIds": []string{o.sid}}, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a cashier invites nobody: %s", r.raw)
	r = a.call(o.manager, http.MethodPost, o.B+"/staff", map[string]any{"phone": "+919000000092", "role": "owner"}, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusUnprocessableEntity}, r.code, "nobody invites an owner: %s", r.raw)
	staff := a.call(o.owner, http.MethodGet, o.B+"/staff", nil, nil)
	require.Equal(t, "manager", staffRoleOf(t, staff, o.manager, a))

	// A removed member's token stops working at the next request: membership
	// is read live (authz slot 15), not from the token.
	r = a.call(o.cashier, http.MethodGet, o.S+"/orders", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	cashierStaff := staffIDOf(t, staff, "cashier")
	r = a.call(o.owner, http.MethodDelete, o.B+"/staff/"+cashierStaff, nil, nil)
	require.Equal(t, http.StatusNoContent, r.code, r.raw)
	r = a.call(o.cashier, http.MethodGet, o.S+"/orders", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "the removed cashier's live token: %s", r.raw)
	r = a.call(o.cashier, http.MethodPost, o.S+"/items/"+o.salt+"/movements", map[string]any{"kind": "restock", "quantity": 5}, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	onHand, _ := o.stock(o.salt)
	require.Equal(t, 500, onHand)
}

// staffRoleOf is the role of the member whose token is tok.
func staffRoleOf(t *testing.T, list resp, tok string, a *bizAPI) string {
	t.Helper()
	me := a.call(tok, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, me.code, me.raw)
	for _, it := range list.body["items"].([]any) {
		m := it.(map[string]any)
		if m["userId"] == me.body["id"] {
			return m["role"].(string)
		}
	}
	t.Fatalf("no staff row for the caller")
	return ""
}

// BK-38: the pickup code tried through any other store's route is 404 and is
// not a wrong attempt on the real order, so an outsider can neither guess it
// nor lock the customer out.
func TestAbuseOrderCodeForeignRoutesHTTP(t *testing.T) {
	o := newOrderShop(t)
	id := o.accepted()
	code := o.code(id)
	other := o.accepted()
	otherCode := o.code(other)

	for i := 0; i < 8; i++ {
		guess := wrongCode(code)
		if i%2 == 1 {
			guess = code // even the right code is no use on the wrong path
		}
		r := o.a.call(o.owner2, http.MethodPost, o.SY+"/orders/"+id+"/verify-code", map[string]any{"code": guess, "paymentMethod": "upi"}, idem(o.a))
		require.Equal(t, http.StatusNotFound, r.code, "another business's store: status %d", r.code)
		r = o.a.call(o.cashierS2, http.MethodPost, o.S2+"/orders/"+id+"/verify-code", map[string]any{"code": guess, "paymentMethod": "upi"}, idem(o.a))
		require.Equal(t, http.StatusNotFound, r.code, "a sibling store: status %d", r.code)
		r = o.a.call(o.owner2, http.MethodPost, o.S+"/orders/"+id+"/verify-code", map[string]any{"code": guess, "paymentMethod": "upi"}, idem(o.a))
		require.Equal(t, http.StatusForbidden, r.code, "S's path for an outsider: status %d", r.code)
	}
	g := o.a.call(o.cashier, http.MethodGet, o.S+"/orders/"+id, nil, nil)
	require.Equal(t, http.StatusOK, g.code, g.raw)
	require.EqualValues(t, 5, g.body["codeAttemptsLeft"], "no foreign attempt counted")
	require.Equal(t, false, g.body["codeLocked"])

	// Another order's code is a wrong code for this one, not a collect.
	if otherCode != code {
		r := o.verify(o.cashier, id, otherCode)
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "another order's code: status %d", r.code)
		require.Equal(t, "code_invalid", problemCode(r))
	}
	r := o.verify(o.cashier, id, code)
	require.Equal(t, http.StatusOK, r.code, "the right code at the right store: status %d", r.code)
	require.Equal(t, "collected", r.body["status"])
	noCode(t, r.body, "the collect response carries no code")
}

func TestAbuseBillTokenHTTP(t *testing.T) {
	p := newPosShop(t)
	bill := p.newID()
	id, tok := p.claim(p.cashier, bill, 29000)

	// A forged tag, a truncated token, another claim's id with this tag, and
	// junk: all the same 404, so a token cannot be probed.
	flip := func(s string, i int) string {
		b := []byte(s)
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		return string(b)
	}
	otherID, otherTok := p.claim(p.cashier, p.newID(), 500)
	require.NotEqual(t, id, otherID)
	for name, bad := range map[string]string{
		"forged tag":         flip(tok, len(tok)-1),
		"forged nonce":       flip(tok, 30),
		"forged id":          flip(tok, 3),
		"truncated":          tok[:len(tok)-2],
		"other claim's id":   otherTok[:22] + tok[22:],
		"not base64url":      strings.Repeat("*", 64),
		"empty-ish":          "AAAA",
		"a valid ULID":       ids.New(p.a.clk.Now()).String(),
		"padded":             tok + "==",
		"other claim's tail": tok[:22] + otherTok[22:],
	} {
		r := p.preview(p.cust, bad)
		require.Contains(t, []int{http.StatusNotFound, http.StatusUnprocessableEntity}, r.code, "%s preview: status %d", name, r.code)
		r = p.confirm(p.cust, bad, 0)
		require.Contains(t, []int{http.StatusNotFound, http.StatusUnprocessableEntity}, r.code, "%s confirm: status %d", name, r.code)
	}
	require.Equal(t, "open", p.getClaim(id).body["status"], "no forged token touched the claim")

	// The first phone wins; a replay by anyone is claim_already_used.
	c := p.confirm(p.cust, tok, 0)
	require.Equal(t, http.StatusOK, c.code, "confirm: status %d", c.code)
	for _, who := range []string{p.cust, p.cust2} {
		r := p.confirm(who, tok, 0)
		require.Equal(t, "claim_already_used", problemCode(r), "replay: status %d", r.code)
	}

	// A claim of S cannot be linked into a sale at S2, even by the owner.
	cross := p.saleBody(bill, line{p.atta, 1})
	cross["link"] = map[string]any{"method": "bill_qr", "claimId": id}
	r := p.a.call(p.owner, http.MethodPost, p.S2+"/pos/sales", cross, idem(p.a))
	require.NotEqual(t, http.StatusOK, r.code, "S's claim at S2: %s", r.raw)
	require.Zero(t, p.a.count(`SELECT count(*) FROM pos_sales`))
	require.Empty(t, p.credits.captured, "no credits captured by the cross-store sale")
	p.noSecrets(tok, otherTok)
}

// loginOn signs phone into app from device (a client-chosen ULID, as the PWA
// sends it) and returns the access token.
func (o *orderShop) loginOn(phone, app, device string) string {
	o.t.Helper()
	a := o.a
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": phone, "app": app, "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(o.t, http.StatusOK, w.code, "otp send: %d", w.code)
	w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"app": app, "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": device,
	}, map[string]string{"User-Agent": "abuse-test"})
	require.Equal(o.t, http.StatusOK, w.code, "otp verify: %d", w.code)
	a.clk.Advance(31 * time.Second)
	return w.body["accessToken"].(string)
}

// Referral farming (D-13) over HTTP. The same-device rule only sees the
// deviceId the client sends, so a farmer who sends a fresh one each time is
// not stopped here: what bounds the farm is one OTP-verified phone per
// account, one claim per account, and no credit until a qualifying purchase.
func TestAbuseReferralFarmingHTTP(t *testing.T) {
	o := newOrderShop(t)
	a := o.a
	refDevice := ids.New(a.clk.Now()).String()
	referrer := o.loginOn("+919000000091", "customer", refDevice)
	me := a.call(referrer, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, me.code, me.raw)
	code := me.body["customer"].(map[string]any)["referralCode"].(string)
	claim := func(tok, code string) resp {
		t.Helper()
		return a.call(tok, http.MethodPost, "/v1/me/referrals/claim", map[string]any{"code": code}, idem(a))
	}
	credits := func() int {
		return a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'referral_reward'`)
	}

	r := claim(referrer, code)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, "referral_self", problemCode(r))

	// A second account on the referrer's device: recorded, rejected, no reward.
	sock := o.loginOn("+919000000092", "customer", refDevice)
	r = claim(sock, code)
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, r.code, r.raw)
	require.Equal(t, "rejected", r.body["status"], "the referrer's device")
	require.EqualValues(t, 0, r.body["rewardCredits"])

	// A farmer with a fresh deviceId per account gets a pending referral,
	// but no credit moves at claim time.
	farmed := o.loginOn("+919000000093", "customer", ids.New(a.clk.Now()).String())
	r = claim(farmed, code)
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, r.code, r.raw)
	require.Equal(t, "pending", r.body["status"], "known gap: the device id is the client's (threat model, referrals)")
	require.Zero(t, credits(), "no credit before a qualifying purchase")

	// One claim per account: a second code is refused.
	other := a.call(o.cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, other.code, other.raw)
	r = claim(farmed, other.body["customer"].(map[string]any)["referralCode"].(string))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "referral_already_claimed", problemCode(r))
	require.Zero(t, credits())
}
