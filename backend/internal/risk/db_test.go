package risk

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	refapi "github.com/todayz/backend/gen/api/admin-referrals"
	riskapi "github.com/todayz/backend/gen/api/admin-risk"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/seed"
	"github.com/todayz/backend/internal/testkit"
)

// The scan over real rows, the A-13 queue and the A-10 reject (SC-RK-01…15).

func TestMain(m *testing.M) { testkit.Main(m) }

const otpMax = 5 // identity.OTPMaxAttempts; the risk package does not import identity

type env struct {
	t     *testing.T
	tdb   *testkit.DB
	svc   *Service
	cache *settings.Cache
	clk   *clock.Fake
	ctx   context.Context
	admin ids.ID
	n     int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := &env{t: t, tdb: tdb, ctx: context.Background(), clk: clock.NewFake(tNow)}
	e.cache = settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	d := db.New(tdb.Pool)
	var err error
	e.svc, err = New(Deps{DB: d, Clock: e.clk, Settings: e.cache, Audit: audit.New(d, e.clk, nil), OTPMaxAttempts: otpMax})
	require.NoError(t, err)
	e.exec(`ALTER TABLE orders DISABLE TRIGGER trg_orders_guard`)
	e.admin = e.id()
	e.exec(`INSERT INTO users (id, name) VALUES ($1, 'Asha')`, ids.UUID(e.admin))
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err, sql)
}

func (e *env) id() ids.ID {
	e.n++
	return ids.New(tNow.Add(time.Duration(e.n) * time.Millisecond))
}

func (e *env) setting(key, value string) {
	e.t.Helper()
	e.exec(`UPDATE platform_settings SET value = $2::jsonb WHERE key = $1`, key, value)
	e.cache.Invalidate()
}

func (e *env) user(name string) ids.ID {
	id := e.id()
	e.exec(`INSERT INTO users (id, name) VALUES ($1, $2)`, ids.UUID(id), name)
	return id
}

// shop is a live store of a new business; it returns both.
func (e *env) shop(name string) (biz, store ids.ID) {
	biz, store = e.id(), e.id()
	code := e.code('B')
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, created_at) VALUES ($1, $2, $3, 'verified', $4)`,
		ids.UUID(biz), name, code, tNow.Add(-30*24*time.Hour))
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, status, live_at) VALUES ($1, $2, $3, $4, $5, 'live', $6)`,
		ids.UUID(store), ids.UUID(biz), fmt.Sprintf("shop-%d", e.n), e.code('S'), name, tNow.Add(-30*24*time.Hour))
	return biz, store
}

func (e *env) staff(biz, user ids.ID) {
	e.exec(`INSERT INTO staff (id, business_id, user_id, role, status) VALUES ($1, $2, $3, 'cashier', 'active')`,
		ids.UUID(e.id()), ids.UUID(biz), ids.UUID(user))
}

func (e *env) flags(rule string) []riskapi.RiskFlag {
	e.t.Helper()
	l, err := e.svc.listFlags(e.ctx, riskapi.ListRiskFlagsParams{Rule: &rule, Limit: ptr(100)})
	require.NoError(e.t, err)
	return l.Items
}

func (e *env) scan() map[string]int64 {
	e.t.Helper()
	sum, err := e.svc.Scan(e.ctx)
	require.NoError(e.t, err)
	return sum
}

func mustParse(t *testing.T, s string) ids.ID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}

// code is a unique six-character code in the referral and short-code
// alphabet (no 0, 1, I, L, O).
func (e *env) code(prefix byte) string {
	const alpha = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	e.n++
	b := []byte{prefix, 'A', 'A', 'A', 'A', 'A'}
	for i, n := 5, e.n; n > 0 && i > 0; i, n = i-1, n/len(alpha) {
		b[i] = alpha[n%len(alpha)]
	}
	return string(b)
}

func hash(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

func problemCode(t *testing.T, err error) string {
	t.Helper()
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	return p.Code
}

// SC-RK-01, SC-RK-02: code lockouts per store and IST day, from the trigger.
func TestScanCodeLockouts(t *testing.T) {
	e := newEnv(t)
	e.setting("risk_code_lockouts_store_per_day", "2")
	_, store := e.shop("Annapurna Kirana")
	cust := e.user("Ravi")
	order := e.tdb.InsertOrder(t, e.id(), cust, store)
	// Lock, "New code" (clears), lock again: each lock is a row (the trigger).
	for i := range 3 {
		e.exec(`UPDATE orders SET code_locked_at = $2 WHERE id = $1`, ids.UUID(order), tNow.Add(-time.Duration(3-i)*time.Hour))
		e.exec(`UPDATE orders SET code_locked_at = NULL WHERE id = $1`, ids.UUID(order))
	}
	// Another store at the limit exactly does not flag.
	_, quiet := e.shop("Quiet Store")
	o2 := e.tdb.InsertOrder(t, e.id(), cust, quiet)
	for i := range 2 {
		e.exec(`UPDATE orders SET code_locked_at = $2 WHERE id = $1`, ids.UUID(o2), tNow.Add(-time.Duration(i+1)*time.Hour))
	}

	sum := e.scan()
	assert.EqualValues(t, 1, sum["orders_code_lockouts"])
	fs := e.flags(RuleCodeLockouts)
	require.Len(t, fs, 1)
	assert.Equal(t, store.String(), fs[0].SubjectId)
	assert.Equal(t, "Annapurna Kirana", *fs[0].SubjectLabel)

	assert.EqualValues(t, 0, e.scan()["orders_code_lockouts"], "one flag per store and IST day")

	d, err := e.svc.getFlag(e.ctx, mustParse(t, fs[0].Id))
	require.NoError(t, err)
	assert.Len(t, d.Evidence, 3)
	assert.Equal(t, riskapi.CodeLockout, d.Evidence[0].Kind)
	assert.Equal(t, "store", d.Subject.Type)
	assert.NotNil(t, d.Subject.BusinessId)
	assert.Contains(t, d.Facts, riskapi.RiskFact{Key: "lockouts", Value: "3"})
	assert.False(t, d.AssistEnabled, "ai_riskexplain_enabled is off by default")

	// The lockout record is append-only.
	_, err = e.tdb.Pool.Exec(e.ctx, `DELETE FROM order_code_lockouts`)
	require.Error(t, err)
}

func (e *env) claim(user, store ids.ID, at time.Time) {
	id := e.tdb.InsertBillClaim(e.t, e.id(), store, e.admin)
	e.exec(`UPDATE bill_claims SET status = 'claimed', claimed_by = $2, claimed_at = $3 WHERE id = $1`, ids.UUID(id), ids.UUID(user), at)
}

// SC-RK-03, SC-RK-04: bill claims from one customer at many stores in minutes.
func TestScanBillClaimSpread(t *testing.T) {
	e := newEnv(t)
	_, a := e.shop("A")
	_, b := e.shop("B")
	_, c := e.shop("C")
	fast, slow := e.user("Fast"), e.user("Slow")
	for i, s := range []ids.ID{a, b, c} {
		e.claim(fast, s, tNow.Add(-20*time.Minute+time.Duration(i)*4*time.Minute))
		e.claim(slow, s, tNow.Add(-3*time.Hour+time.Duration(i)*20*time.Minute))
	}
	assert.EqualValues(t, 1, e.scan()["pos_bill_claim_spread"])
	fs := e.flags(RuleBillClaimSpread)
	require.Len(t, fs, 1)
	assert.Equal(t, fast.String(), fs[0].SubjectId)
	d, err := e.svc.getFlag(e.ctx, mustParse(t, fs[0].Id))
	require.NoError(t, err)
	assert.Len(t, d.Evidence, 3)
	assert.EqualValues(t, 0, e.scan()["pos_bill_claim_spread"])
}

// SC-RK-05, SC-RK-06: the refund/reversal ratio per store, linked sales only.
func TestScanReversalRatio(t *testing.T) {
	e := newEnv(t)
	_, bad := e.shop("Bad")
	_, ok := e.shop("Ok")
	_, few := e.shop("Few")
	cust := e.user("C")
	sale := func(store ids.ID, reversed bool, method string) {
		id := e.tdb.InsertPosSale(t, e.id(), store, e.admin)
		e.exec(`UPDATE pos_sales SET link_method = $2, link_status = 'linked', customer_id = $3, received_at = $4 WHERE id = $1`,
			ids.UUID(id), method, ids.UUID(cust), tNow.Add(-24*time.Hour))
		if reversed {
			e.exec(`UPDATE pos_sales SET status = 'reversed', reversed_at = $2, reversed_by = $3, reversal_reason = 'wrong item' WHERE id = $1`,
				ids.UUID(id), tNow.Add(-23*time.Hour), ids.UUID(e.admin))
		}
	}
	// The 30-sale default (risk_reversal_min_sales): Bad 9 of 30 reversed
	// (30 %), Ok 3 of 30 (10 %), Few 15 of 29, under the minimum.
	for i := range 30 {
		sale(bad, i < 9, "bill_qr")
		sale(ok, i < 3, "counter_code")
		if i < 29 {
			sale(few, i < 15, "bill_qr")
		}
	}
	// Unlinked cash sales reversed at "ok" do not count (01 §7).
	for range 5 {
		id := e.tdb.InsertPosSale(t, e.id(), ok, e.admin)
		e.exec(`UPDATE pos_sales SET status = 'reversed', reversed_at = $2, reversed_by = $3, reversal_reason = 'x' WHERE id = $1`,
			ids.UUID(id), tNow.Add(-time.Hour), ids.UUID(e.admin))
	}
	assert.EqualValues(t, 1, e.scan()["pos_reversal_ratio"])
	fs := e.flags(RuleReversalRatio)
	require.Len(t, fs, 1)
	assert.Equal(t, bad.String(), fs[0].SubjectId)
	d, err := e.svc.getFlag(e.ctx, mustParse(t, fs[0].Id))
	require.NoError(t, err)
	assert.Len(t, d.Evidence, 9)
	for _, ev := range d.Evidence {
		assert.NotContains(t, ev.Label, "wrong item", "the reversal reason is free text and stays out")
	}
	assert.EqualValues(t, 0, e.scan()["pos_reversal_ratio"], "one flag per store per window")
}

func (e *env) referral(kind, rType string, rID ids.ID, eType string, eID ids.ID, qualType string, qualID ids.ID) ids.ID {
	id := e.id()
	e.exec(`INSERT INTO referrals (id, kind, referrer_type, referrer_id, referee_type, referee_id, code, source, status,
	    referrer_credits, referee_credits, qualifying_ref_type, qualifying_ref_id, qualified_at, created_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, 'code', 'qualified', 50, 50, $8, $9, $10, $11, $11)`,
		ids.UUID(id), kind, rType, ids.UUID(rID), eType, ids.UUID(eID), e.code('R'),
		qualType, ids.UUID(qualID), tNow.Add(-time.Hour), tNow.Add(-2*time.Hour))
	return id
}

// SC-RK-07, SC-RK-08: a referee's first order at the referrer's own shop with
// a self-like signal.
func TestScanSelfLikeReferral(t *testing.T) {
	e := newEnv(t)
	biz, store := e.shop("Own Shop")
	_, other := e.shop("Other Shop")

	// The referee works at the referring store's business and bought there.
	cashier := e.user("Cashier")
	e.staff(biz, cashier)
	o1 := e.tdb.InsertOrder(t, e.id(), cashier, store)
	selfLike := e.referral("retailer_customer", "store", store, "customer", cashier, "order", o1)

	// A real customer who bought at the referring shop: the normal case.
	real := e.user("Real")
	o2 := e.tdb.InsertOrder(t, e.id(), real, store)
	e.referral("retailer_customer", "store", store, "customer", real, "order", o2)

	// Staff who bought elsewhere: not at the referrer's own shop.
	cashier2 := e.user("Cashier 2")
	e.staff(biz, cashier2)
	o3 := e.tdb.InsertOrder(t, e.id(), cashier2, other)
	e.referral("retailer_customer", "store", store, "customer", cashier2, "order", o3)

	assert.EqualValues(t, 1, e.scan()["referral_self_like"])
	fs := e.flags(RuleSelfLike)
	require.Len(t, fs, 1)
	assert.Equal(t, selfLike.String(), fs[0].SubjectId)
	assert.Equal(t, "Own Shop → Cashier", *fs[0].SubjectLabel)
	d, err := e.svc.getFlag(e.ctx, mustParse(t, fs[0].Id))
	require.NoError(t, err)
	require.Len(t, d.Evidence, 2, "the referral and its qualifying order")
	assert.Equal(t, riskapi.Referral, d.Evidence[0].Kind)
	assert.Equal(t, riskapi.Order, d.Evidence[1].Kind)
	assert.Equal(t, biz.String(), *d.Subject.BusinessId)
	assert.Equal(t, cashier.String(), *d.Subject.CustomerId)
	assert.EqualValues(t, 0, e.scan()["referral_self_like"], "one flag per referral, ever")

	// The referral's own row lists the flag on A-10.
	l, err := e.svc.listReferrals(e.ctx, refapi.ListAdminReferralsParams{Flagged: ptr(true)})
	require.NoError(t, err)
	require.Len(t, l.Items, 1)
	assert.Equal(t, []string{RuleSelfLike}, l.Items[0].FlagRules)
	assert.Equal(t, 1, l.Items[0].OpenFlags)
}

// D-237: the reward pass checks one referral before it posts; the scan
// never raises a second flag for it, and a referral that is not self-like
// raises nothing.
func TestCheckReferralSelfLike(t *testing.T) {
	e := newEnv(t)
	biz, store := e.shop("Own Shop")
	cashier := e.user("Cashier")
	e.staff(biz, cashier)
	selfLike := e.referral("retailer_customer", "store", store, "customer", cashier, "order", e.tdb.InsertOrder(t, e.id(), cashier, store))
	real := e.user("Real")
	normal := e.referral("retailer_customer", "store", store, "customer", real, "order", e.tdb.InsertOrder(t, e.id(), real, store))

	raised, err := e.svc.CheckReferralSelfLike(e.ctx, normal)
	require.NoError(t, err)
	assert.False(t, raised)
	raised, err = e.svc.CheckReferralSelfLike(e.ctx, selfLike)
	require.NoError(t, err)
	assert.True(t, raised)
	raised, err = e.svc.CheckReferralSelfLike(e.ctx, selfLike)
	require.NoError(t, err)
	assert.False(t, raised, "one flag per referral, ever")

	fs := e.flags(RuleSelfLike)
	require.Len(t, fs, 1)
	assert.Equal(t, selfLike.String(), fs[0].SubjectId)
	assert.EqualValues(t, 0, e.scan()["referral_self_like"], "the scan does not raise it again")
}

func (e *env) otp(phone, ip string, attempts int, used bool, at time.Time) {
	var consumed *time.Time
	if used {
		consumed = &at
	}
	var ipHash []byte
	if ip != "" {
		ipHash = hash("ip:" + ip)
	}
	e.exec(`INSERT INTO otp_requests (id, phone_hash, phone_enc, purpose, app, code_hash, expires_at, attempts, consumed_at, ip_hash, created_at)
	  VALUES ($1, $2, '\x00', 'login', 'customer', $3, $4, $5, $6, $7, $8)`,
		ids.UUID(e.id()), hash("phone:"+phone), hash(fmt.Sprintf("code:%d", e.n)), at.Add(5*time.Minute), attempts, consumed, ipHash, at)
}

// SC-RK-13, SC-RK-14: OTP abuse, never a number, OTP or code in the flag.
func TestScanOTPAbuse(t *testing.T) {
	e := newEnv(t)
	for i := range 10 {
		e.otp(fmt.Sprintf("p%d", i), "bad-ip", 0, false, tNow.Add(-time.Duration(i+1)*time.Minute))
	}
	for i := range 9 {
		e.otp(fmt.Sprintf("q%d", i), "office-ip", 0, true, tNow.Add(-time.Duration(i+1)*time.Minute))
	}
	// A phone with no account that ran out of attempts three times.
	for i := range 3 {
		e.otp("stuck", "", otpMax, false, tNow.Add(-time.Duration(i+1)*time.Hour))
	}
	// A user with two failed OTPs, under the limit, who then got in: not abuse.
	u := e.user("Typo")
	e.exec(`INSERT INTO phone_identities (id, user_id, phone_enc, phone_hash, verified_at) VALUES ($1, $2, '\x00', $3, $4)`,
		ids.UUID(e.id()), ids.UUID(u), hash("phone:typo"), tNow.Add(-48*time.Hour))
	for i := range 2 {
		e.otp("typo", "", otpMax, false, tNow.Add(-time.Duration(i+1)*time.Hour))
	}
	e.otp("typo", "", 1, true, tNow.Add(-10*time.Minute))

	sum := e.scan()
	assert.EqualValues(t, 1, sum["identity_otp_ip_fanout"], "10 phones from one IP in the hour")
	assert.EqualValues(t, 1, sum["identity_otp_failed_phone"])
	ip := e.flags(RuleOTPIPFanout)
	require.Len(t, ip, 1)
	assert.Equal(t, SubjectIP, ip[0].SubjectType)
	assert.Nil(t, ip[0].SubjectLabel)
	ph := e.flags(RuleOTPFailedPhone)
	require.Len(t, ph, 1)
	assert.Equal(t, SubjectPhone, ph[0].SubjectType)

	d, err := e.svc.getFlag(e.ctx, mustParse(t, ip[0].Id))
	require.NoError(t, err)
	assert.Len(t, d.Evidence, 10)
	d2, err := e.svc.getFlag(e.ctx, mustParse(t, ph[0].Id))
	require.NoError(t, err)
	require.Len(t, d2.Evidence, 3)
	assert.Equal(t, "failed", *d2.Evidence[0].Note)
	for _, dd := range []riskapi.RiskFlagDetail{d, d2} {
		b, err := json.Marshal(dd)
		require.NoError(t, err)
		assert.NotContains(t, string(b), "hash")
		assert.NotContains(t, string(b), "stuck")
	}
	sum = e.scan()
	assert.EqualValues(t, 0, sum["identity_otp_ip_fanout"]+sum["identity_otp_failed_phone"], "one flag per subject per window")
}

// The OTP rules dedupe on their rolling window, not the IST day: a flag
// cleared at 23:50 is not raised again at 00:10 from the same requests.
func TestOTPFlagsDoNotComeBackAtMidnight(t *testing.T) {
	e := newEnv(t)
	late := time.Date(2026, 10, 25, 23, 50, 0, 0, clock.IST)
	e.clk.Set(late)
	for i := range 10 {
		e.otp(fmt.Sprintf("p%d", i), "bad-ip", 0, false, late.Add(-time.Duration(i+1)*time.Minute))
	}
	for _, h := range []time.Duration{20 * time.Minute, time.Hour, 2 * time.Hour} {
		e.otp("stuck", "", otpMax, false, late.Add(-h))
	}
	sum := e.scan()
	require.EqualValues(t, 1, sum["identity_otp_ip_fanout"])
	require.EqualValues(t, 1, sum["identity_otp_failed_phone"])
	for _, f := range append(e.flags(RuleOTPIPFanout), e.flags(RuleOTPFailedPhone)...) {
		_, err := e.svc.resolve(e.ctx, e.admin, mustParse(t, f.Id), "open", "cleared", "load test from our office")
		require.NoError(t, err)
	}

	e.clk.Set(late.Add(20 * time.Minute)) // 00:10 IST, the next day
	sum = e.scan()
	assert.EqualValues(t, 0, sum["identity_otp_ip_fanout"], "the same hour's requests, already cleared")
	assert.EqualValues(t, 0, sum["identity_otp_failed_phone"], "the same 24 h, already cleared")

	// A fresh burst once the last flag is older than the window is new facts.
	e.clk.Set(late.Add(2 * time.Hour))
	for i := range 10 {
		e.otp(fmt.Sprintf("r%d", i), "bad-ip", 0, false, late.Add(2*time.Hour-time.Duration(i+1)*time.Minute))
	}
	assert.EqualValues(t, 1, e.scan()["identity_otp_ip_fanout"])
}

// Overlapping risk_scan runs: one holding the lock makes the other skip, and
// two started together raise each flag once.
func TestScanRunsOneAtATime(t *testing.T) {
	e := newEnv(t)
	for i := range 10 {
		e.otp(fmt.Sprintf("p%d", i), "bad-ip", 0, false, tNow.Add(-time.Duration(i+1)*time.Minute))
	}
	tx, err := e.tdb.Pool.Begin(e.ctx)
	require.NoError(t, err)
	_, err = tx.Exec(e.ctx, `SELECT pg_advisory_xact_lock(hashtext('risk_scan'))`)
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"skipped": 1}, e.scan(), "the lock is held: skip")
	assert.Empty(t, e.flags(RuleOTPIPFanout))
	require.NoError(t, tx.Rollback(e.ctx))

	sums := make([]map[string]int64, 4)
	var wg sync.WaitGroup
	for i := range sums {
		wg.Go(func() {
			var err error
			sums[i], err = e.svc.Scan(e.ctx)
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	var raised int64
	for _, s := range sums {
		raised += s["identity_otp_ip_fanout"]
	}
	assert.EqualValues(t, 1, raised)
	assert.Len(t, e.flags(RuleOTPIPFanout), 1)
}

// A rule that fails does not stop the others: they run and commit, the
// error names the failed rule only.
func TestScanKeepsGoingPastAFailedRule(t *testing.T) {
	e := newEnv(t)
	for i := range 10 {
		e.otp(fmt.Sprintf("p%d", i), "bad-ip", 0, false, tNow.Add(-time.Duration(i+1)*time.Minute))
	}
	e.exec(`REVOKE SELECT ON order_code_lockouts FROM todayz_app`)
	sum, err := e.svc.Scan(e.ctx)
	require.Error(t, err)
	assert.Equal(t, "risk_scan: rules failed: "+RuleCodeLockouts, err.Error())
	assert.NotContains(t, sum, "orders_code_lockouts")
	assert.EqualValues(t, 1, sum["identity_otp_ip_fanout"], "a later rule still ran")
	assert.Len(t, e.flags(RuleOTPIPFanout), 1, "and its flag committed")
}

func (e *env) rawFlag(rule, subjectType string, subject ids.ID, details map[string]any) ids.ID {
	id := e.id()
	e.exec(`INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, details, created_at) VALUES ($1, $2, $3, $4, 50, $5, $6)`,
		ids.UUID(id), subjectType, ids.UUID(subject), rule, mustJSON(details), tNow.Add(-time.Hour))
	return id
}

// SC-RK-09…12: the flags other packages raise show with their evidence.
func TestOtherPackagesFlagsHaveEvidence(t *testing.T) {
	e := newEnv(t)
	_, store := e.shop("S")
	cust := e.user("C")
	order := e.tdb.InsertOrder(t, e.id(), cust, store)
	sale := e.tdb.InsertPosSale(t, e.id(), store, e.admin)
	e.exec(`UPDATE pos_sales SET device_seq = 1, link_method = 'counter_code', link_status = 'linked', customer_id = $2,
	    offline_link_code = '1234', offline_link_at = $3, received_at = $3 WHERE id = $1`, ids.UUID(sale), ids.UUID(cust), tNow.Add(-2*time.Hour))
	e.exec(`INSERT INTO strikes (id, subject_type, subject_id, order_id, reason, created_at) VALUES ($1, 'store', $2, $3, 'dispatch_timeout', $4)`,
		ids.UUID(e.id()), ids.UUID(store), ids.UUID(order), tNow.Add(-24*time.Hour))

	cases := []struct {
		rule, subjectType string
		subject           ids.ID
		details           map[string]any
		kind              riskapi.RiskEvidenceKind
	}{
		{"pos.offline_links", "store", store, map[string]any{"offlineLinks": 1, "limit": 0}, riskapi.PosSale},
		{"store_strikes", "store", store, map[string]any{"strikes": 1, "threshold": 3, "windowDays": 30}, riskapi.Strike},
		{"return_unconfirmed", "store", store, map[string]any{"orderId": order.String()}, riskapi.Order},
		{"credits.store_daily_cap", "store", store, map[string]any{"ref_type": "pos_sale", "ref_id": sale.String()}, riskapi.PosSale},
	}
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			id := e.rawFlag(c.rule, c.subjectType, c.subject, c.details)
			d, err := e.svc.getFlag(e.ctx, id)
			require.NoError(t, err)
			require.NotEmpty(t, d.Evidence)
			assert.Equal(t, c.kind, d.Evidence[0].Kind)
			for _, ev := range d.Evidence {
				assert.NotEqual(t, "1234", ev.Label, "an offline counter code never shows")
				if ev.Note != nil {
					assert.NotContains(t, *ev.Note, "1234")
				}
			}
		})
	}
}

// SC-RK-15: clear and confirm need a reason, are audited, and change nothing
// else; support cannot (the policy golden, risk.golden).
func TestClearAndConfirm(t *testing.T) {
	e := newEnv(t)
	_, store := e.shop("S")
	f1 := e.rawFlag(RuleCodeLockouts, "store", store, map[string]any{"lockouts": 11, "limit": 10, "day": "2026-10-25"})
	f2 := e.rawFlag(RuleCodeLockouts, "store", store, map[string]any{"lockouts": 12, "limit": 10, "day": "2026-10-24"})

	_, err := e.svc.resolve(e.ctx, e.admin, f1, "open", "cleared", "   ")
	assert.Equal(t, genproblems.ValidationFailed, problemCode(t, err))

	got, err := e.svc.resolve(e.ctx, e.admin, f1, "open", "cleared", "  Festival day  ")
	require.NoError(t, err)
	assert.Equal(t, riskapi.RiskFlagStatus("cleared"), got.Status)
	assert.Equal(t, "Festival day", *got.ReviewNote)
	assert.Equal(t, "Asha", *got.ReviewedBy)
	assert.Equal(t, tNow, got.ReviewedAt.UTC())

	_, err = e.svc.resolve(e.ctx, e.admin, f1, "open", "confirmed", "changed my mind")
	assert.Equal(t, genproblems.InvalidTransition, problemCode(t, err), "only an open flag")

	_, err = e.svc.resolve(e.ctx, e.admin, f2, "open", "confirmed", "Staff guessed codes")
	require.NoError(t, err)
	_, err = e.svc.resolve(e.ctx, e.admin, f2, "confirmed", "confirmed", "again")
	assert.Equal(t, genproblems.InvalidTransition, problemCode(t, err), "confirm only from open")
	var status string
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT status FROM stores WHERE id = $1`, ids.UUID(store)).Scan(&status))
	assert.Equal(t, "live", status, "confirming never suspends")

	// A confirmed finding that turns out wrong is cleared later (which
	// releases its payout hold, 01 §256); the audit row says it was confirmed.
	e.clk.Set(tNow.Add(time.Hour))
	_, err = e.svc.resolve(e.ctx, e.admin, f2, "open", "cleared", "saw it open")
	assert.Equal(t, genproblems.InvalidTransition, problemCode(t, err), "a clear never lands on a confirm it did not see")
	got, err = e.svc.resolve(e.ctx, e.admin, f2, "confirmed", "cleared", "CCTV showed the customers")
	require.NoError(t, err)
	assert.Equal(t, riskapi.RiskFlagStatus("cleared"), got.Status)
	assert.Equal(t, "CCTV showed the customers", *got.ReviewNote)
	var before string
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx,
		`SELECT before->>'status' FROM audit_log WHERE subject_id = $1 AND action = 'risk.flag_clear'`, f2.String()).Scan(&before))
	assert.Equal(t, "confirmed", before)
	_, err = e.svc.resolve(e.ctx, e.admin, f2, "confirmed", "cleared", "again")
	assert.Equal(t, genproblems.InvalidTransition, problemCode(t, err), "a cleared flag stays cleared")

	_, err = e.svc.resolve(e.ctx, e.admin, e.id(), "open", "cleared", "x")
	assert.Equal(t, genproblems.NotFound, problemCode(t, err))

	var n int
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx,
		`SELECT count(*) FROM audit_log WHERE subject_type = 'risk_flag' AND action IN ('risk.flag_clear','risk.flag_confirm')`).Scan(&n))
	assert.Equal(t, 3, n)

	l, err := e.svc.listFlags(e.ctx, riskapi.ListRiskFlagsParams{Status: ptr(riskapi.RiskFlagStatus("open"))})
	require.NoError(t, err)
	assert.Empty(t, l.Items)
	for _, c := range l.OpenByRule {
		assert.NotEqual(t, RuleCodeLockouts, c.Rule)
	}
}

// Two reviewers on one open flag, one clearing and one confirming at once:
// the row lock orders them and the second gets the 409, so exactly one
// verdict and one audit row land.
func TestConcurrentClearAndConfirm(t *testing.T) {
	e := newEnv(t)
	_, store := e.shop("S")
	for i := range 5 {
		f := e.rawFlag(RuleCodeLockouts, "store", store, map[string]any{"i": i})
		verdicts := []string{"cleared", "confirmed"}
		errs := make([]error, len(verdicts))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for j, to := range verdicts {
			wg.Go(func() {
				<-start
				_, errs[j] = e.svc.resolve(e.ctx, e.admin, f, "open", to, "reviewer "+to)
			})
		}
		close(start)
		wg.Wait()
		won := 0
		for _, err := range errs {
			if err == nil {
				won++
			} else {
				assert.Equal(t, genproblems.InvalidTransition, problemCode(t, err))
			}
		}
		assert.Equal(t, 1, won, "round %d", i)
		var n int
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx,
			`SELECT count(*) FROM audit_log WHERE subject_id = $1`, f.String()).Scan(&n))
		assert.Equal(t, 1, n, "round %d", i)
	}
}

func TestListFlagsPages(t *testing.T) {
	e := newEnv(t)
	_, store := e.shop("S")
	for i := range 5 {
		e.rawFlag(RuleCodeLockouts, "store", store, map[string]any{"i": i})
	}
	var seen []string
	var cur *string
	for {
		l, err := e.svc.listFlags(e.ctx, riskapi.ListRiskFlagsParams{Limit: ptr(2), Cursor: cur})
		require.NoError(t, err)
		for _, f := range l.Items {
			seen = append(seen, f.Id)
		}
		if l.NextCursor == nil {
			break
		}
		cur = l.NextCursor
	}
	assert.Len(t, seen, 5)
	_, err := e.svc.listFlags(e.ctx, riskapi.ListRiskFlagsParams{Cursor: ptr("junk")})
	assert.Equal(t, genproblems.ValidationFailed, problemCode(t, err))
}

// A-10: an admin reject before any reward posts; never after.
func TestRejectReferral(t *testing.T) {
	e := newEnv(t)
	_, store := e.shop("S")
	c1, c2, c3 := e.user("One"), e.user("Two"), e.user("Three")
	o1 := e.tdb.InsertOrder(t, e.id(), c1, store)
	o3 := e.tdb.InsertOrder(t, e.id(), c3, store)
	qualified := e.referral("retailer_customer", "store", store, "customer", c1, "order", o1)
	paid := e.referral("retailer_customer", "store", store, "customer", c3, "order", o3)
	tx := e.tdb.InsertLedgerTx(t, e.id(), "referral_reward")
	e.exec(`UPDATE referrals SET referrer_transaction_id = $2, referrer_rewarded_at = $3 WHERE id = $1`, ids.UUID(paid), ids.UUID(tx), tNow)
	pending := e.id()
	e.exec(`INSERT INTO referrals (id, kind, referrer_type, referrer_id, referee_type, referee_id, code, source,
	    referrer_credits, referee_credits, created_at, updated_at)
	  VALUES ($1, 'retailer_customer', 'store', $2, 'customer', $3, 'PAAAAA', 'code', 50, 50, $4, $4)`,
		ids.UUID(pending), ids.UUID(store), ids.UUID(c2), tNow)

	_, err := e.svc.RejectReferral(e.ctx, e.admin, qualified, "")
	assert.Equal(t, genproblems.ValidationFailed, problemCode(t, err))

	for _, id := range []ids.ID{qualified, pending} {
		r, err := e.svc.RejectReferral(e.ctx, e.admin, id, "Staff self-referral")
		require.NoError(t, err)
		assert.Equal(t, refapi.AdminReferralStatus("rejected"), r.Status)
		assert.Equal(t, "admin_rejected", *r.RejectReason)
		assert.Nil(t, r.QualifiedAt)
	}
	_, err = e.svc.RejectReferral(e.ctx, e.admin, paid, "too late")
	assert.Equal(t, genproblems.InvalidTransition, problemCode(t, err), "one side is paid")
	_, err = e.svc.RejectReferral(e.ctx, e.admin, qualified, "again")
	assert.Equal(t, genproblems.InvalidTransition, problemCode(t, err))

	var n int
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx,
		`SELECT count(*) FROM audit_log WHERE subject_type = 'referral' AND action = 'referral.reject'`).Scan(&n))
	assert.Equal(t, 2, n)
}

// The false-positive review (07 W3-02): on the dev seed, with its stores,
// staff and customers, a scan raises nothing.
func TestSeedRaisesNoFlags(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, seed.Run(e.ctx, config.EnvTest, seed.Dev, seed.Deps{
		DB: db.New(e.tdb.Pool), Clock: clock.NewFake(seed.Epoch), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	e.clk.Set(seed.Epoch.Add(time.Hour))
	for rule, n := range e.scan() {
		assert.Zero(t, n, rule)
	}
	var n int
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT count(*) FROM risk_flags`).Scan(&n))
	assert.Zero(t, n)
}
