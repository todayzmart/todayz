package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/identity"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/payouts"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/pii"
)

// approverPhone signs in the second admin (two-person approval, D-05).
const approverPhone = "+919000000031"

// payoutShop is the order shop with KYC verified, ₹3,000 of store credit on
// S earned in October 2026, and October closed at 1 Nov 00:30 IST.
type payoutShop struct {
	*orderShop
	bid string
	cr  *credits.Service
}

func newPayoutShop(t *testing.T) *payoutShop {
	t.Helper()
	o := newOrderShop(t)
	bid := strings.TrimPrefix(o.B, "/v1/biz/businesses/")
	seal, err := pii.NewSealer(o.cfg.Keys.PIIEncKey.Bytes())
	require.NoError(t, err)
	b, err := ids.Parse(bid)
	require.NoError(t, err)
	upi, err := seal.Seal(business.SealField(business.FieldUPI, b), []byte("lakshmi@okaxis"))
	require.NoError(t, err)
	bank, err := seal.Seal(business.SealField(business.FieldBankAccount, b), []byte("001234567890"))
	require.NoError(t, err)
	o.a.exec(`INSERT INTO business_kyc (business_id, upi_id_enc, upi_id_last4, bank_account_enc, bank_account_last4, bank_ifsc, legal_name, status)
	  VALUES ($1, $2, 'axis', $3, '7890', 'UTIB0000123', 'Lakshmi Traders LLP', 'verified')
	  ON CONFLICT (business_id) DO UPDATE SET upi_id_enc = excluded.upi_id_enc, upi_id_last4 = excluded.upi_id_last4,
	    bank_account_enc = excluded.bank_account_enc, bank_account_last4 = excluded.bank_account_last4, bank_ifsc = excluded.bank_ifsc,
	    legal_name = excluded.legal_name, status = excluded.status`, ids.UUID(b), upi, bank)

	cr, err := jobs.CreditsService(jobs.Deps{Pool: o.a.tdb.Pool, Clock: o.a.clk, Settings: o.settings, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
	require.NoError(t, err)
	p := &payoutShop{orderShop: o, bid: bid, cr: cr}
	s, err := ids.Parse(o.sid)
	require.NoError(t, err)
	p.post(credits.Account{Kind: credits.StoreCredit, Owner: s}, 3000)

	o.set(time.Date(2026, 11, 1, 0, 30, 0, 0, clock.IST))
	out, err := o.runner.RunScheduled(context.Background(), payouts.KindClose)
	require.NoError(t, err)
	require.NoError(t, out.Err)
	require.Equal(t, platformjobs.StatusSucceeded, out.Status)
	require.EqualValues(t, 1, out.Result.Summary["closed"])
	return p
}

// post moves n credits from platform marketing to the account (a referral).
func (p *payoutShop) post(to credits.Account, n int64) {
	p.t.Helper()
	_, err := p.cr.Post(context.Background(), credits.Posting{Kind: credits.TxReferralReward, IdempotencyKey: "referral:payouts-http:" + ids.New(p.a.clk.Now()).String(),
		Legs: []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -n}, {Account: to, Amount: n}}})
	require.NoError(p.t, err)
}

// secondDesk signs in a second admin user on approverPhone.
type secondDesk struct {
	o      *orderShop
	secret []byte
}

func newSecondDesk(o *orderShop, roles ...string) *secondDesk {
	o.t.Helper()
	var cust string
	o.login(&cust, approverPhone, "customer")
	me := o.a.call(cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(o.t, http.StatusOK, me.code, me.raw)
	o.a.exec(`INSERT INTO admin_users (user_id, roles) VALUES ($1, $2)`, uuidOf(o.t, me.body["id"].(string)), roles)
	return &secondDesk{o: o}
}

func (d *secondDesk) token() string {
	t, a := d.o.t, d.o.a
	t.Helper()
	w := a.call("", http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"phone": approverPhone, "app": "admin", "purpose": "login", "turnstileToken": "XXXX.DUMMY.TOKEN.XXXX",
	}, nil)
	require.Equal(t, http.StatusOK, w.code, "otp send: %d", w.code)
	w = a.call("", http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"app": "admin", "requestId": w.body["requestId"].(string), "code": testOTP, "deviceId": ids.New(a.clk.Now()).String(),
	}, map[string]string{"User-Agent": "payouts-test"})
	require.Equal(t, http.StatusOK, w.code, "otp verify: %d", w.code)
	tok := w.body["accessToken"].(string)
	e := a.call(tok, http.MethodPost, "/v1/auth/totp/enrol", nil, nil)
	require.Equal(t, http.StatusOK, e.code, "enrol: %d", e.code)
	s, err := identity.DecodeTOTPSecret(e.body["secret"].(string))
	require.NoError(t, err)
	d.secret = s
	a.clk.Advance(31 * time.Second)
	v := a.call(tok, http.MethodPost, "/v1/auth/totp/verify", map[string]string{"code": identity.TOTPCode(d.secret, a.clk.Now()), "purpose": "login"}, nil)
	require.Equal(t, http.StatusOK, v.code, "totp verify: %d", v.code)
	return v.body["accessToken"].(string)
}

func items(t *testing.T, r resp) []map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, r.code, r.raw)
	var out []map[string]any
	for _, it := range r.body["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

func isCSV(t *testing.T, r resp) {
	t.Helper()
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.True(t, strings.HasPrefix(r.hdr.Get("Content-Type"), "text/csv"), r.hdr.Get("Content-Type"))
}

// SC-PO-03 over HTTP: A-11 reads for finance, admin and super_admin; the
// money actions (hold, unhold, mark paid, export) for finance and
// super_admin only, with a reason and a fresh step-up; B-10 for the owner
// and manager.
func TestPayoutsHTTP(t *testing.T) {
	p := newPayoutShop(t)
	a := p.a
	desk := newAdminDesk(p.orderShop, "finance")
	tok := desk.token()
	get := func(path string) resp { return a.call(tok, http.MethodGet, path, nil, nil) }
	post := func(path string, body any, hdr map[string]string) resp {
		return a.call(tok, http.MethodPost, path, body, hdr)
	}

	// A-11: the closed month, its CSV and the open payout.
	sts := items(t, get("/v1/admin/statements?period=2026-10"))
	require.Len(t, sts, 1)
	require.Equal(t, p.bid, sts[0]["businessId"])
	require.EqualValues(t, 300000, sts[0]["totals"].(map[string]any)["closingPaise"])
	stID := sts[0]["id"].(string)
	r := get("/v1/admin/statements/" + stID)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = get("/v1/admin/statements/" + stID + "?format=csv")
	isCSV(t, r)
	require.Contains(t, r.raw, "2026-10")
	pays := items(t, get("/v1/admin/payouts?period=2026-10"))
	require.Len(t, pays, 1)
	require.Equal(t, "pending", pays[0]["status"])
	require.EqualValues(t, 300000, pays[0]["amountPaise"])
	pid := pays[0]["id"].(string)
	base := "/v1/admin/payouts/" + pid
	r = get(base)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, true, r.body["causes"].(map[string]any)["kycVerified"])
	require.NotContains(t, r.raw, "lakshmi@okaxis", "the payee block is masked")
	require.NotContains(t, r.raw, "001234567890", "the payee block is masked")

	note := map[string]any{"reason": "checking the October statement"}
	paid := map[string]any{"utr": "AXIS123456", "amountPaise": 300000, "reason": "October payout sent"}
	money := map[string]func() resp{
		"hold":      func() resp { return post(base+"/hold", note, nil) },
		"unhold":    func() resp { return post(base+"/unhold", note, nil) },
		"mark-paid": func() resp { return post(base+"/mark-paid", paid, idem(a)) },
		"export":    func() resp { return get("/v1/admin/payout-exports?period=2026-10") },
	}
	reads := []string{"/v1/admin/statements?period=2026-10", "/v1/admin/statements/" + stID, "/v1/admin/payouts", base}

	// The admin role reads A-11 but moves no money (D-64).
	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	for _, path := range reads {
		require.Equal(t, http.StatusOK, get(path).code, path)
	}
	for name, f := range money {
		r := f()
		require.Equal(t, http.StatusForbidden, r.code, "admin %s: %s", name, r.raw)
	}
	// The other admin roles see nothing of it.
	for _, role := range []string{"support", "catalogue", "analyst", "field_agent"} {
		a.exec(`UPDATE admin_users SET roles = ARRAY[$1]`, role)
		for _, path := range reads {
			require.Equal(t, http.StatusForbidden, get(path).code, "%s %s", role, path)
		}
		for name, f := range money {
			r := f()
			require.Equal(t, http.StatusForbidden, r.code, "%s %s: %s", role, name, r.raw)
		}
	}
	a.exec(`UPDATE admin_users SET roles = ARRAY['finance']`)
	require.Equal(t, "pending", get(base).body["payout"].(map[string]any)["status"], "nothing moved")

	// A reason is required.
	r = post(base+"/hold", map[string]any{}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, "validation_failed", problemCode(r))

	// A stale step-up is asked again for every money action; reads are not.
	a.clk.Advance(authn.StepUpWindow + time.Second)
	for name, f := range money {
		r := f()
		require.Equal(t, http.StatusForbidden, r.code, "%s: %s", name, r.raw)
		require.Equal(t, "step_up_required", problemCode(r), name)
	}
	require.Equal(t, http.StatusOK, get(base).code)
	a.clk.Advance(31 * time.Second)
	v := post("/v1/auth/totp/verify", map[string]string{"code": identity.TOTPCode(desk.secret, a.clk.Now()), "purpose": "step_up"}, nil)
	require.Equal(t, http.StatusOK, v.code, v.raw)

	// Hold, then mark paid is refused; unhold puts it back.
	r = money["hold"]()
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "held", r.body["payout"].(map[string]any)["status"])
	require.Equal(t, "manual", r.body["payout"].(map[string]any)["holdReason"])
	r = money["mark-paid"]()
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "payout_not_payable", problemCode(r))
	r = money["unhold"]()
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "pending", r.body["payout"].(map[string]any)["status"])

	// The export is CSV with the pending payout.
	r = money["export"]()
	isCSV(t, r)
	require.Contains(t, r.raw, pid)

	// Mark paid needs an Idempotency-Key and the amount on screen.
	r = post(base+"/mark-paid", paid, nil)
	require.Equal(t, http.StatusBadRequest, r.code, r.raw)
	require.Equal(t, "idempotency_key_required", problemCode(r))
	r = post(base+"/mark-paid", map[string]any{"utr": "AXIS123456", "amountPaise": 299900, "reason": "October payout sent"}, idem(a))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "payout_amount_changed", problemCode(r))
	key := idem(a)
	r = post(base+"/mark-paid", paid, key)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "paid", r.body["payout"].(map[string]any)["status"])
	require.Equal(t, "AXIS123456", r.body["payout"].(map[string]any)["utr"])
	// A retry with the same key replays; nothing posts twice.
	r = post(base+"/mark-paid", paid, key)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "paid", r.body["payout"].(map[string]any)["status"])
	require.Equal(t, 1, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'payout'`))
	for _, action := range []string{"payouts.hold", "payouts.unhold", "payouts.mark_paid", "payouts.export"} {
		require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = $1`, action), action)
	}

	// B-10: the owner and manager read the statement with its payout; a
	// cashier and another business's owner do not.
	for _, who := range []string{p.owner, p.manager} {
		bs := items(t, a.call(who, http.MethodGet, p.B+"/statements", nil, nil))
		require.Len(t, bs, 1)
		require.Equal(t, "2026-10", bs[0]["period"])
		r := a.call(who, http.MethodGet, p.B+"/statements/2026-10", nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		isCSV(t, a.call(who, http.MethodGet, p.B+"/statements/2026-10?format=csv", nil, nil))
	}
	r = a.call(p.owner, http.MethodGet, p.B+"/statements/2026-10", nil, nil)
	st := r.body["statement"].(map[string]any)
	require.Equal(t, "paid", st["payout"].(map[string]any)["status"])
	require.NotContains(t, r.raw, "001234567890")
	r = a.call(p.cashier, http.MethodGet, p.B+"/statements", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = a.call(p.owner2, http.MethodGet, p.B+"/statements", nil, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, r.code, r.raw)
	r = a.call(p.owner, http.MethodGet, p.B+"/statements/2026-09", nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
}

// SC-CR-15 over HTTP: A-09 adjustments are finance and super_admin only,
// with a reason, an Idempotency-Key and a fresh step-up; above the threshold
// a second person approves, never the requester.
func TestAdminCreditAdjustmentsHTTP(t *testing.T) {
	p := newPayoutShop(t)
	a := p.a
	me := a.call(p.cust, http.MethodGet, "/v1/me", nil, nil)
	require.Equal(t, http.StatusOK, me.code, me.raw)
	cust, err := ids.Parse(me.body["id"].(string))
	require.NoError(t, err)
	p.post(credits.Account{Kind: credits.CustomerWallet, Owner: cust}, 1000)

	desk := newAdminDesk(p.orderShop, "finance")
	tok := desk.token()
	accounts := items(t, a.call(tok, http.MethodGet, "/v1/admin/ledger/accounts?q="+cust.String(), nil, nil))
	require.Len(t, accounts, 1)
	require.Equal(t, "customer_wallet", accounts[0]["kind"])
	require.EqualValues(t, 100000, accounts[0]["balancePaise"])
	acct := accounts[0]["id"].(string)
	adjust := func(tok string, paise int, hdr map[string]string) resp {
		return a.call(tok, http.MethodPost, "/v1/admin/credit-adjustments",
			map[string]any{"accountId": acct, "amountPaise": paise, "reason": "goodwill for a late pickup"}, hdr)
	}
	balance := func() any {
		return items(t, a.call(tok, http.MethodGet, "/v1/admin/ledger/accounts?q="+cust.String(), nil, nil))[0]["balancePaise"]
	}

	// The admin role reads A-09 but cannot adjust (D-64); support sees nothing.
	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	require.Equal(t, http.StatusOK, a.call(tok, http.MethodGet, "/v1/admin/credit-adjustments", nil, nil).code)
	r := adjust(tok, 20000, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	a.exec(`UPDATE admin_users SET roles = ARRAY['support']`)
	require.Equal(t, http.StatusForbidden, a.call(tok, http.MethodGet, "/v1/admin/ledger/accounts?q="+cust.String(), nil, nil).code)
	require.Equal(t, http.StatusForbidden, adjust(tok, 20000, idem(a)).code)
	a.exec(`UPDATE admin_users SET roles = ARRAY['finance']`)

	// At or under the threshold it posts at once, once per key.
	r = adjust(tok, 20000, nil)
	require.Equal(t, http.StatusBadRequest, r.code, r.raw)
	require.Equal(t, "idempotency_key_required", problemCode(r))
	key := idem(a)
	r = adjust(tok, 20000, key)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "posted", r.body["outcome"])
	require.NotNil(t, r.body["transactionId"])
	r = adjust(tok, 20000, key)
	require.Equal(t, http.StatusCreated, r.code, "the replay is the stored response: %s", r.raw)
	require.EqualValues(t, 120000, balance())

	// Above it, a pending request; the requester cannot approve it.
	r = adjust(tok, 60000, idem(a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "pending", r.body["outcome"])
	req := r.body["request"].(map[string]any)["id"].(string)
	approve := "/v1/admin/credit-adjustments/" + req + "/approve"
	decision := map[string]any{"note": "checked the order"}
	r = a.call(tok, http.MethodPost, approve, decision, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "approver_is_requester", problemCode(r))
	require.EqualValues(t, 120000, balance())

	// A second person approves it once; a stale step-up is asked again.
	tok2 := newSecondDesk(p.orderShop, "super_admin").token()
	r = a.call(tok2, http.MethodPost, approve, decision, nil)
	require.Equal(t, http.StatusBadRequest, r.code, r.raw)
	r = a.call(tok2, http.MethodPost, approve, decision, idem(a))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "approved", r.body["outcome"])
	r = a.call(tok2, http.MethodPost, approve, decision, idem(a))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	require.EqualValues(t, 180000, balance())

	a.clk.Advance(authn.StepUpWindow + time.Second)
	r = adjust(tok, 10000, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	require.Equal(t, "step_up_required", problemCode(r))
	require.EqualValues(t, 180000, balance())
}
