package payouts_test

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	api "github.com/todayz/backend/gen/api/admin-payouts"
	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/payouts"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// ist is a wall-clock time in IST.
func ist(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, clock.IST)
}

type env struct {
	t        *testing.T
	ctx      context.Context
	tdb      *testkit.DB
	clk      *clock.Fake
	st       *settings.Cache
	cr       *credits.Service
	svc      *payouts.Service
	reg      *platformjobs.Registry
	gen      *ids.Generator
	seal     *pii.Sealer
	codes    int
	finance  authn.Principal
	biz      ids.ID
	s1, s2   ids.ID
	customer ids.ID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	clk := clock.NewFake(ist(2026, 6, 3, 12, 0))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{TTL: time.Hour, Log: log})
	seal, err := pii.NewSealer([]byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	open := func(f payouts.PayeeField, bid ids.ID, sealed []byte) (string, error) {
		b, err := seal.Open(business.SealField(string(f), bid), sealed)
		return string(b), err
	}
	d := jobs.Deps{Pool: tdb.Pool, Clock: clk, Settings: st, Log: log}
	svc, err := jobs.PayoutsService(d, open)
	require.NoError(t, err)
	cr, err := jobs.CreditsService(d, nil)
	require.NoError(t, err)
	reg := platformjobs.NewRegistry()
	jobs.Register(reg, d)
	e := &env{t: t, ctx: context.Background(), tdb: tdb, clk: clk, st: st, cr: cr, svc: svc, reg: reg, gen: ids.NewGenerator(), seal: seal}
	e.finance = authn.Principal{UserID: e.user(), App: authn.AppAdmin, Roles: []string{"finance"}}
	e.biz = e.business("Lakshmi Traders")
	e.s1, e.s2 = e.store(e.biz, "Lakshmi Traders"), e.store(e.biz, "Lakshmi Traders 2")
	e.kyc(e.biz, "verified")
	e.customer = e.user()
	e.grant(e.customer, 200)
	return e
}

const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func (e *env) code() string {
	e.codes++
	n, b := e.codes, make([]byte, 6)
	for i := 5; i >= 0; i-- {
		b[i] = codeAlphabet[n%len(codeAlphabet)]
		n /= len(codeAlphabet)
	}
	return string(b)
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err)
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, sql, args...).Scan(&n))
	return n
}

func (e *env) id() ids.ID { return e.gen.New(e.clk.Now()) }

func (e *env) user() ids.ID {
	e.t.Helper()
	return e.tdb.InsertUser(e.t, e.id())
}

func (e *env) business(name string) ids.ID {
	e.t.Helper()
	id := e.id()
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code, status) VALUES ($1, $2, $3, 'verified')`, ids.UUID(id), name, e.code())
	return id
}

func (e *env) store(biz ids.ID, name string) ids.ID {
	e.t.Helper()
	id := e.id()
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-")) + "-" + strings.ToLower(e.code())
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, cashback_bp) VALUES ($1, $2, $3, $4, $5, 500)`,
		ids.UUID(id), ids.UUID(biz), slug, e.code(), name)
	return id
}

// kyc writes the business's KYC with sealed UPI and bank details.
func (e *env) kyc(biz ids.ID, status string) {
	e.t.Helper()
	upi, err := e.seal.Seal(business.SealField(business.FieldUPI, biz), []byte("lakshmi@okaxis"))
	require.NoError(e.t, err)
	bank, err := e.seal.Seal(business.SealField(business.FieldBankAccount, biz), []byte("001234567890"))
	require.NoError(e.t, err)
	e.exec(`INSERT INTO business_kyc (business_id, upi_id_enc, upi_id_last4, bank_account_enc, bank_account_last4, bank_ifsc, legal_name, status)
	  VALUES ($1, $2, 'axis', $3, '7890', 'UTIB0000123', 'Lakshmi Traders LLP', $4)
	  ON CONFLICT (business_id) DO UPDATE SET status = excluded.status`, ids.UUID(biz), upi, bank, status)
}

func (e *env) setKYC(status string) {
	e.exec(`UPDATE business_kyc SET status = $2 WHERE business_id = $1`, ids.UUID(e.biz), status)
}

func (e *env) grant(c ids.ID, n int64) {
	e.t.Helper()
	_, err := e.cr.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, IdempotencyKey: "referral:test:" + e.id().String(),
		Legs: []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -n}, {Account: credits.Account{Kind: credits.CustomerWallet, Owner: c}, Amount: n}}})
	require.NoError(e.t, err)
}

// storeReferral is a store's referral reward (referral_in).
func (e *env) storeReferral(s ids.ID, n int64) {
	e.t.Helper()
	_, err := e.cr.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, IdempotencyKey: "referral:test:" + e.id().String(),
		Legs: []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -n}, {Account: credits.Account{Kind: credits.StoreCredit, Owner: s}, Amount: n}}})
	require.NoError(e.t, err)
}

// storeAdjust is a ledger-level store adjustment (adjustments).
func (e *env) storeAdjust(s ids.ID, n int64) {
	e.t.Helper()
	_, err := e.cr.Post(e.ctx, credits.Posting{Kind: credits.TxAdminAdjustment, IdempotencyKey: "adjust:test:" + e.id().String(),
		Legs: []credits.Leg{{Account: credits.Account{Kind: credits.PlatformAdjustments}, Amount: -n}, {Account: credits.Account{Kind: credits.StoreCredit, Owner: s}, Amount: n}}})
	require.NoError(e.t, err)
}

// order spends n credits (0: none) on a bill of eligible paise at s and
// completes it: the capture to the store and 5% cashback from it.
func (e *env) order(s ids.ID, n int, eligible int64) credits.Ref {
	e.t.Helper()
	ref := credits.Ref{Type: credits.RefOrder, ID: e.id()}
	e.tdb.InsertOrder(e.t, ref.ID, e.customer, s)
	if n > 0 {
		a, err := e.cr.Hold(e.ctx, credits.HoldRequest{Ref: ref, CustomerID: e.customer, StoreID: s, Credits: n, EligiblePaise: eligible})
		require.NoError(e.t, err)
		require.Equal(e.t, n, a.Allowed)
	}
	_, err := e.cr.Complete(e.ctx, credits.Completion{Ref: ref, CustomerID: e.customer, StoreID: s, EligiblePaise: eligible})
	require.NoError(e.t, err)
	return ref
}

func (e *env) reverse(ref credits.Ref, s ids.ID) {
	e.t.Helper()
	_, err := e.cr.Reverse(e.ctx, credits.Reversal{Ref: ref, CustomerID: e.customer, StoreID: s})
	require.NoError(e.t, err)
}

func (e *env) at(t time.Time) { e.clk.Set(t) }

// close runs the close of month m (IST) at 00:30 on the 1st after it.
func (e *env) close(m time.Month, y int) payouts.CloseResult {
	e.t.Helper()
	next := ist(y, m, 1, 0, 0).AddDate(0, 1, 0)
	e.at(next.Add(30 * time.Minute))
	res, err := e.svc.CloseMonth(e.ctx, payouts.ClosePeriod(e.clk.Now()))
	require.NoError(e.t, err)
	return res
}

func (e *env) openPayout() api.AdminPayoutDetail {
	e.t.Helper()
	return e.payoutWhere(`business_id = $1 AND status IN ('pending','held')`, ids.UUID(e.biz))
}

// payoutWhere reads the one payout matching the condition.
func (e *env) payoutWhere(cond string, args ...any) api.AdminPayoutDetail {
	e.t.Helper()
	var u pgtype.UUID
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT id FROM payouts WHERE `+cond, args...).Scan(&u))
	id, ok := ids.FromUUID(u)
	require.True(e.t, ok)
	d, err := e.svc.Payout(e.ctx, id)
	require.NoError(e.t, err)
	return d
}

// storeCredit is a store's store_credit balance now.
func (e *env) storeCredit(s ids.ID) int64 {
	e.t.Helper()
	var n int64
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT coalesce(sum(b.balance), 0)::bigint
	  FROM ledger_accounts a JOIN account_balances b ON b.account_id = a.id
	  WHERE a.kind = 'store_credit' AND a.owner_id = $1`, ids.UUID(s)).Scan(&n))
	return n
}

func code(err error) string { return problem.CodeOf(err) }

// totals is a statement's totals in paise; cashback out and payouts out are
// positive amounts the formula subtracts (the contract's StatementTotals).
func totals(opening, creditsIn, referralIn, cashbackOut, reversals, adjustments, payoutsOut, closing int) api.StatementTotals {
	return api.StatementTotals{
		OpeningPaise: opening, CreditsInPaise: creditsIn, ReferralInPaise: referralIn, CashbackOutPaise: cashbackOut,
		ReversalsPaise: reversals, AdjustmentsPaise: adjustments, PayoutsOutPaise: payoutsOut, ClosingPaise: closing,
	}
}

// seedJune is the golden month (all in credits; ×100 paise):
//
//	S1: 30 captured, 11 cashback (5% of ₹220), +5 adjustment     → closing 24
//	S2: +50 referral, 20 captured, 9 cashback, both reversed (−11) → closing 50
//
// and, after the month ends but before the close, a 5-credit cashback at S1.
func (e *env) seedJune() {
	e.t.Helper()
	e.at(ist(2026, 6, 4, 11, 0))
	e.order(e.s1, 30, 25000)
	e.at(ist(2026, 6, 10, 18, 0))
	e.storeReferral(e.s2, 50)
	e.at(ist(2026, 6, 15, 12, 0))
	o2 := e.order(e.s2, 20, 20000)
	e.at(ist(2026, 6, 16, 12, 0))
	e.reverse(o2, e.s2)
	e.at(ist(2026, 6, 20, 9, 0))
	e.storeAdjust(e.s1, 5)
	e.at(ist(2026, 6, 30, 23, 59)) // last minute of June
	e.storeReferral(e.s2, 0+1)
	e.storeAdjust(e.s2, -1)
	e.at(ist(2026, 7, 1, 0, 10)) // July: not in June's statement
	e.order(e.s1, 0, 10000)
}

// Statement golden test over a seeded month, the ledger tie-out and the
// first payout (payable is per store min(closing, balance now)).
func TestStatementGoldenDB(t *testing.T) {
	e := newEnv(t)
	e.seedJune()

	res := e.close(time.June, 2026)
	require.Equal(t, payouts.CloseResult{Closed: 1, Payouts: 1}, res)

	list, err := e.svc.Statements(e.ctx, "2026-06", nil, nil)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	st := list.Items[0]
	require.Equal(t, "2026-06", st.Period)
	require.Equal(t, totals(0, 5000, 5100, 2000, -1100, 400, 0, 7400), st.Totals)
	require.Equal(t, api.PayoutKycStatus("verified"), st.KycStatus)
	require.False(t, st.OpenRiskFlag)
	require.NotNil(t, st.Payout)

	sid, err := ids.Parse(st.Id)
	require.NoError(t, err)
	d, err := e.svc.Statement(e.ctx, sid)
	require.NoError(t, err)
	require.Len(t, d.Stores, 2)
	byName := map[string]api.StatementStore{}
	for _, s := range d.Stores {
		byName[s.StoreName] = s
	}
	require.Equal(t, totals(0, 3000, 0, 1100, 0, 500, 0, 2400), byName["Lakshmi Traders"].Totals)
	require.Equal(t, 3, byName["Lakshmi Traders"].Entries)
	require.Equal(t, totals(0, 2000, 5100, 900, -1100, -100, 0, 5000), byName["Lakshmi Traders 2"].Totals)

	// The CSV: one row per store, amounts in rupees with 2 decimals.
	b, err := e.svc.StatementCSV(e.ctx, sid)
	require.NoError(t, err)
	rows, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"period", "store_id", "store_name", "opening_inr", "credits_in_inr", "referral_in_inr", "cashback_out_inr",
		"reversals_inr", "adjustments_inr", "payouts_out_inr", "closing_inr", "entries"}, rows[0])
	require.Len(t, rows, 3)
	want := map[string][]string{
		"Lakshmi Traders":   {"0.00", "30.00", "0.00", "11.00", "0.00", "5.00", "0.00", "24.00", "3"},
		"Lakshmi Traders 2": {"0.00", "20.00", "51.00", "9.00", "-11.00", "-1.00", "0.00", "50.00", "7"},
	}
	for _, r := range rows[1:] {
		require.Equal(t, "2026-06", r[0])
		require.Equal(t, want[r[2]], r[3:], r[2])
	}

	// Tie-out: Σ statements = Σ store_credit movements in the month, and the
	// closings = Σ store_credit entries before the month's end.
	from, to := ist(2026, 6, 1, 0, 0), ist(2026, 7, 1, 0, 0)
	moved := e.count(`SELECT coalesce(sum(e.amount), 0)::int FROM ledger_entries e JOIN ledger_accounts a ON a.id = e.account_id
	  WHERE a.kind = 'store_credit' AND e.created_at >= $1 AND e.created_at < $2`, from, to)
	stated := e.count(`SELECT coalesce(sum(closing - opening), 0)::int FROM payout_statements WHERE period = '2026-06-01'`)
	require.Equal(t, moved, stated)
	require.Equal(t, e.count(`SELECT coalesce(sum(e.amount), 0)::int FROM ledger_entries e JOIN ledger_accounts a ON a.id = e.account_id
	  WHERE a.kind = 'store_credit' AND e.created_at < $1`, to), e.count(`SELECT sum(closing)::int FROM payout_statements WHERE period = '2026-06-01'`))
	require.Equal(t, 74, stated)

	// The payout: S1 19 (its balance after July's cashback), S2 50.
	require.EqualValues(t, 19, e.storeCredit(e.s1))
	p := e.openPayout()
	require.Equal(t, api.PayoutStatus("pending"), p.Payout.Status)
	require.Equal(t, 6900, p.Payout.AmountPaise)
	lines := map[string]int{}
	for _, l := range p.Lines {
		lines[l.StoreName] = l.AmountPaise
	}
	require.Equal(t, map[string]int{"Lakshmi Traders": 1900, "Lakshmi Traders 2": 5000}, lines)
	require.NotNil(t, p.Payee)
	require.Equal(t, "7890", *p.Payee.BankAccountLast4, "finance sees bank details masked (PO-03)")

	// B-10: the owner's view; a store-limited member sees only their stores.
	bs, err := e.svc.BusinessStatement(e.ctx, e.biz, nil, "2026-06")
	require.NoError(t, err)
	require.Equal(t, st.Totals, bs.Statement.Totals)
	require.NotNil(t, bs.Statement.Payout)
	require.Equal(t, api.BusinessPayoutStateStatusProcessing, bs.Statement.Payout.Status)
	one, err := e.svc.BusinessStatement(e.ctx, e.biz, []ids.ID{e.s1}, "2026-06")
	require.NoError(t, err)
	require.Len(t, one.Stores, 1)
	require.Equal(t, byName["Lakshmi Traders"].Totals, one.Statement.Totals)
	require.Nil(t, one.Statement.Payout)
	_, err = e.svc.BusinessStatement(e.ctx, e.business("Ganesh Stores"), nil, "2026-06")
	require.Equal(t, "not_found", code(err))

	// A rerun of the close changes nothing (idempotent per business and month).
	res, err = e.svc.CloseMonth(e.ctx, payouts.ClosePeriod(e.clk.Now()))
	require.NoError(t, err)
	require.Zero(t, res.Closed)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM payout_statements`))
	require.Equal(t, 6900, e.openPayout().Payout.AmountPaise)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM audit_log WHERE action = 'payouts.statement_close'`))

	// The current month cannot close.
	_, err = e.svc.CloseMonth(e.ctx, e.clk.Now())
	require.Error(t, err)
}

// PO-01, PO-02, mark paid (UTR, amount, idempotent), the payouts_out line,
// carry forward and holds.
func TestOnePayoutPerBusinessDB(t *testing.T) {
	e := newEnv(t)
	e.seedJune()
	e.close(time.June, 2026)
	p1 := e.openPayout()

	// PO-01: July closes while P1 is unpaid → P1 recomputed, no second row.
	e.at(ist(2026, 7, 10, 12, 0))
	e.storeReferral(e.s2, 30)
	res := e.close(time.July, 2026)
	require.Equal(t, 1, res.Closed)
	again := e.openPayout()
	require.Equal(t, p1.Payout.Id, again.Payout.Id)
	require.Equal(t, 9900, again.Payout.AmountPaise, "S1 19 + S2 80")
	require.Equal(t, 1, e.count(`SELECT count(*) FROM payouts WHERE business_id = $1`, ids.UUID(e.biz)))
	_, err := e.tdb.OwnerPool.Exec(e.ctx, `INSERT INTO payouts (id, business_id, amount, status, computed_at, created_at, updated_at)
	  VALUES ($1, $2, 100, 'pending', now(), now(), now())`, ids.UUID(e.id()), ids.UUID(e.biz))
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg))
	require.Equal(t, "ux_payouts_open_business", pg.ConstraintName)

	// Mark paid: the amount must be the one on screen, the UTR well formed.
	pid, err := ids.Parse(again.Payout.Id)
	require.NoError(t, err)
	e.at(ist(2026, 8, 3, 11, 0))
	_, err = e.svc.MarkPaid(e.ctx, e.finance, pid, "AXIS12345678", 6900, "July payout")
	require.Equal(t, "payout_amount_changed", code(err))
	_, err = e.svc.MarkPaid(e.ctx, e.finance, pid, "no", 9900, "July payout")
	require.Equal(t, "validation_failed", code(err))
	paid, err := e.svc.MarkPaid(e.ctx, e.finance, pid, " axis12345678 ", 9900, "July payout")
	require.NoError(t, err)
	require.Equal(t, api.PayoutStatus("paid"), paid.Payout.Status)
	require.Equal(t, "AXIS12345678", *paid.Payout.Utr)
	require.Zero(t, e.storeCredit(e.s1))
	require.Zero(t, e.storeCredit(e.s2))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'payout' AND idempotency_key = $1`, "payout:"+pid.String()))

	// A rerun never pays twice: the same UTR returns the paid payout, another is refused.
	same, err := e.svc.MarkPaid(e.ctx, e.finance, pid, "AXIS12345678", 9900, "July payout")
	require.NoError(t, err)
	require.Equal(t, paid.Payout.PaidAt, same.Payout.PaidAt)
	_, err = e.svc.MarkPaid(e.ctx, e.finance, pid, "AXIS99999999", 9900, "July payout")
	require.Equal(t, "payout_already_paid", code(err))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'payout'`))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM audit_log WHERE action = 'payouts.mark_paid' AND after->>'utr' = 'AXIS12345678'`))

	// PO-02: after P1 is paid the next close creates a new payout; KYC
	// missing holds it, and a held payout is still the open one.
	e.at(ist(2026, 8, 5, 12, 0))
	e.storeReferral(e.s2, 40)
	e.setKYC("submitted")
	e.close(time.August, 2026)
	p2 := e.openPayout()
	require.NotEqual(t, p1.Payout.Id, p2.Payout.Id)
	require.Equal(t, api.PayoutStatus("held"), p2.Payout.Status)
	require.Equal(t, api.PayoutHoldCause("kyc_incomplete"), *p2.Payout.HoldReason)
	require.Equal(t, 4000, p2.Payout.AmountPaise)
	aug, err := e.svc.BusinessStatement(e.ctx, e.biz, nil, "2026-08")
	require.NoError(t, err)
	require.Equal(t, totals(9900, 0, 4000, 0, 0, 0, 9900, 4000), aug.Statement.Totals, "the payout is a line; opening carried")
	require.Equal(t, api.BusinessPayoutStateStatusNeedsKyc, aug.Statement.Payout.Status)
	jul, err := e.svc.BusinessStatement(e.ctx, e.biz, nil, "2026-07")
	require.NoError(t, err)
	require.Equal(t, api.BusinessPayoutStateStatusPaid, jul.Statement.Payout.Status)

	e.at(ist(2026, 9, 9, 12, 0))
	e.storeReferral(e.s2, 10)
	e.close(time.September, 2026)
	p2b := e.openPayout()
	require.Equal(t, p2.Payout.Id, p2b.Payout.Id)
	require.Equal(t, 5000, p2b.Payout.AmountPaise)
	require.Equal(t, api.PayoutStatus("held"), p2b.Payout.Status, "a close never lifts a hold")
	require.Equal(t, 2, e.count(`SELECT count(*) FROM payouts WHERE business_id = $1`, ids.UUID(e.biz)))

	// Held: not payable; unhold refused while the cause is open.
	id2, err := ids.Parse(p2.Payout.Id)
	require.NoError(t, err)
	_, err = e.svc.MarkPaid(e.ctx, e.finance, id2, "AXIS22222222", 5000, "September payout")
	require.Equal(t, "payout_not_payable", code(err))
	_, err = e.svc.Unhold(e.ctx, e.finance, id2, "KYC checked")
	require.Equal(t, "payout_hold_cause_open", code(err))
	e.setKYC("verified")
	u, err := e.svc.Unhold(e.ctx, e.finance, id2, "KYC checked")
	require.NoError(t, err)
	require.Equal(t, api.PayoutStatus("pending"), u.Payout.Status)
	_, err = e.svc.Unhold(e.ctx, e.finance, id2, "again")
	require.Equal(t, "invalid_transition", code(err))

	// A manual hold with a note, then unhold.
	h, err := e.svc.Hold(e.ctx, e.finance, id2, "bank name mismatch")
	require.NoError(t, err)
	require.Equal(t, api.PayoutHoldCause("manual"), *h.Payout.HoldReason)
	require.Equal(t, "bank name mismatch", *h.Payout.HoldNote)
	_, err = e.svc.Hold(e.ctx, e.finance, id2, "again")
	require.Equal(t, "invalid_transition", code(err))
	_, err = e.svc.Unhold(e.ctx, e.finance, id2, "name matched")
	require.NoError(t, err)

	// A risk flag opened after the close blocks mark paid live.
	flag := e.id()
	e.exec(`INSERT INTO risk_flags (id, subject_type, subject_id, rule) VALUES ($1, 'store', $2, 'test.rule')`, ids.UUID(flag), ids.UUID(e.s2))
	_, err = e.svc.MarkPaid(e.ctx, e.finance, id2, "AXIS22222222", 5000, "September payout")
	require.Equal(t, "payout_hold_cause_open", code(err))
	e.exec(`UPDATE risk_flags SET status = 'cleared', reviewed_at = now() WHERE id = $1`, ids.UUID(flag))

	// Another payout's UTR is refused.
	_, err = e.svc.MarkPaid(e.ctx, e.finance, id2, "AXIS12345678", 5000, "September payout")
	require.Equal(t, "utr_in_use", code(err))
	_, err = e.svc.MarkPaid(e.ctx, e.finance, id2, "AXIS22222222", 5000, "September payout")
	require.NoError(t, err)

	for _, a := range []string{"payouts.hold", "payouts.unhold", "payouts.mark_paid"} {
		require.Positive(t, e.count(`SELECT count(*) FROM audit_log WHERE action = $1 AND actor_id = $2`, a, ids.UUID(e.finance.UserID)), a)
	}
	clean, err := e.cr.Reconcile(e.ctx)
	require.NoError(t, err)
	require.True(t, clean.Clean(), "reconcile: %v", clean)
}

// Holds at the close: suspended, risk flag; a negative balance carries
// forward and pays nothing.
func TestHoldCausesAndCarryForwardDB(t *testing.T) {
	e := newEnv(t)
	// June: only cashback at S1 → closing −10, nothing payable.
	e.at(ist(2026, 6, 5, 12, 0))
	e.order(e.s1, 0, 20000)
	res := e.close(time.June, 2026)
	require.Equal(t, payouts.CloseResult{Closed: 1}, res)
	require.Zero(t, e.count(`SELECT count(*) FROM payouts`))
	jun, err := e.svc.BusinessStatement(e.ctx, e.biz, nil, "2026-06")
	require.NoError(t, err)
	require.Equal(t, -1000, jun.Statement.Totals.ClosingPaise)
	require.Nil(t, jun.Statement.Payout)

	// July: S1 earns 30; opening −10 carries; payout 20; the business is suspended.
	e.at(ist(2026, 7, 5, 12, 0))
	e.storeReferral(e.s1, 30)
	e.exec(`UPDATE businesses SET status = 'suspended' WHERE id = $1`, ids.UUID(e.biz))
	e.exec(`INSERT INTO risk_flags (id, subject_type, subject_id, rule) VALUES ($1, 'business', $2, 'test.rule')`, ids.UUID(e.id()), ids.UUID(e.biz))
	res = e.close(time.July, 2026)
	require.Equal(t, 1, res.Held)
	jul, err := e.svc.BusinessStatement(e.ctx, e.biz, nil, "2026-07")
	require.NoError(t, err)
	require.Equal(t, totals(-1000, 0, 3000, 0, 0, 0, 0, 2000), jul.Statement.Totals)
	p := e.openPayout()
	require.Equal(t, 2000, p.Payout.AmountPaise)
	require.Equal(t, api.PayoutHoldCause("business_suspended"), *p.Payout.HoldReason, "suspended outranks the risk flag")

	// A second store's cashback drives the balance under the payout: August
	// recomputes down; nothing left payable cancels it.
	e.at(ist(2026, 8, 2, 12, 0))
	e.order(e.s1, 0, 40000)
	e.close(time.August, 2026)
	require.Zero(t, e.count(`SELECT count(*) FROM payouts WHERE status IN ('pending','held')`))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM payouts WHERE status = 'cancelled' AND id = $1`, mustUUID(t, p.Payout.Id)))

	// A quiet business (no store credit movement ever) gets no statement.
	quiet := e.business("Quiet Stores")
	e.store(quiet, "Quiet Stores")
	require.Zero(t, e.count(`SELECT count(*) FROM payout_statements WHERE business_id = $1`, ids.UUID(quiet)))
}

func mustUUID(t *testing.T, s string) any {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return ids.UUID(id)
}

// PO-04: the export's columns, only pending payouts with no open cause,
// exported_at stamped, formula cells escaped, audited without bank details.
func TestExportDB(t *testing.T) {
	e := newEnv(t)
	e.exec(`UPDATE businesses SET legal_name = '=HYPERLINK("x")' WHERE id = $1`, ids.UUID(e.biz))
	e.exec(`UPDATE business_kyc SET legal_name = '@SUM(1)' WHERE business_id = $1`, ids.UUID(e.biz))
	held := e.business("Held Stores")
	hs := e.store(held, "Held Stores")
	e.kyc(held, "submitted")
	paidBiz := e.business("Paid Stores")
	ps := e.store(paidBiz, "Paid Stores")
	e.kyc(paidBiz, "verified")

	e.at(ist(2026, 6, 5, 12, 0))
	e.storeReferral(e.s1, 25)
	e.storeReferral(hs, 10)
	e.storeReferral(ps, 10)
	e.close(time.June, 2026)
	pd := e.payoutWhere(`business_id = $1`, ids.UUID(paidBiz))
	pid, err := ids.Parse(pd.Payout.Id)
	require.NoError(t, err)
	_, err = e.svc.MarkPaid(e.ctx, e.finance, pid, "HDFC76543210", 1000, "paid by hand")
	require.NoError(t, err)

	b, err := e.svc.Export(e.ctx, e.finance, "2026-06")
	require.NoError(t, err)
	rows, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"payout_id", "period", "business_id", "legal_name", "beneficiary_name", "amount_inr", "upi_id", "bank_account_number", "ifsc"}, rows[0])
	require.Len(t, rows, 2, "only the pending payout: held and paid are absent")
	r := rows[1]
	require.Equal(t, e.biz.String(), r[2])
	require.Equal(t, `'=HYPERLINK("x")`, r[3])
	require.Equal(t, "'@SUM(1)", r[4])
	require.Equal(t, []string{"25.00", "lakshmi@okaxis", "001234567890", "UTIB0000123"}, r[5:])
	require.Equal(t, 1, e.count(`SELECT count(*) FROM payouts WHERE exported_at IS NOT NULL`))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM audit_log WHERE action = 'payouts.export'`))
	require.Zero(t, e.count(`SELECT count(*) FROM audit_log WHERE after::text LIKE '%001234567890%' OR after::text LIKE '%okaxis%'`))

	// A cause opened since the close keeps the payout out of the file.
	e.exec(`INSERT INTO risk_flags (id, subject_type, subject_id, rule) VALUES ($1, 'business', $2, 'test.rule')`, ids.UUID(e.id()), ids.UUID(e.biz))
	b, err = e.svc.Export(e.ctx, e.finance, "2026-06")
	require.NoError(t, err)
	rows, err = csv.NewReader(strings.NewReader(string(b))).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	_, err = e.svc.Export(e.ctx, e.finance, "June")
	require.Equal(t, "validation_failed", code(err))
}

// The registered kind closes the month before its run's period.
func TestCloseJobDB(t *testing.T) {
	e := newEnv(t)
	e.at(ist(2026, 6, 5, 12, 0))
	e.storeReferral(e.s1, 25)
	e.at(ist(2026, 7, 1, 0, 30))
	f := platformjobs.NewFake(e.reg, e.clk)
	var k platformjobs.Kind
	for _, x := range e.reg.Kinds() {
		if x.Name == payouts.KindClose {
			k = x
		}
	}
	require.Equal(t, "30 0 1 * *", k.Schedule)
	require.Equal(t, platformjobs.ClassMoney, k.Class)
	out, err := f.RunScheduled(e.ctx, payouts.KindClose)
	require.NoError(t, err)
	require.NoError(t, out.Err)
	require.Equal(t, platformjobs.StatusSucceeded, out.Status)
	require.EqualValues(t, 1, out.Result.Summary["closed"])
	require.Equal(t, 1, e.count(`SELECT count(*) FROM payout_statements WHERE period = '2026-06-01'`))
	// The same period again is skipped; a fresh worker's rerun closes nothing.
	out, err = f.RunScheduled(e.ctx, payouts.KindClose)
	require.NoError(t, err)
	require.Equal(t, platformjobs.StatusSkipped, out.Status)
	out, err = platformjobs.NewFake(e.reg, e.clk).RunScheduled(e.ctx, payouts.KindClose)
	require.NoError(t, err)
	require.NoError(t, out.Err)
	require.Zero(t, out.Result.Summary["closed"], "a rerun closes nothing")
	require.Equal(t, 1, e.count(`SELECT count(*) FROM payouts`))
}

// F: July's close recomputes June's open payout (D-09). June's statement then
// reads carried with what June's close computed; July's shows the live payout,
// and keeps showing it once paid. A-11 marks June's carried too.
func TestStatementCarriedToTheNextDB(t *testing.T) {
	e := newEnv(t)
	pid := e.pendingJune(100)
	e.at(ist(2026, 7, 10, 12, 0))
	e.storeReferral(e.s1, 30)
	e.close(time.July, 2026)
	require.Equal(t, pid.String(), e.openPayout().Payout.Id)

	june, err := e.svc.BusinessStatement(e.ctx, e.biz, nil, "2026-06")
	require.NoError(t, err)
	require.Equal(t, api.BusinessPayoutStateStatusCarried, june.Statement.Payout.Status)
	require.Equal(t, 10000, june.Statement.Payout.AmountPaise)
	july, err := e.svc.BusinessStatement(e.ctx, e.biz, nil, "2026-07")
	require.NoError(t, err)
	require.Equal(t, api.BusinessPayoutStateStatusProcessing, july.Statement.Payout.Status)
	require.Equal(t, 13000, july.Statement.Payout.AmountPaise)

	admin, err := e.svc.Statements(e.ctx, "2026-06", nil, nil)
	require.NoError(t, err)
	require.True(t, admin.Items[0].Payout.Carried)
	admin, err = e.svc.Statements(e.ctx, "2026-07", nil, nil)
	require.NoError(t, err)
	require.False(t, admin.Items[0].Payout.Carried)

	e.at(ist(2026, 8, 2, 12, 0))
	_, err = e.svc.MarkPaid(e.ctx, e.finance, pid, "AXIS12345678", 13000, "July payout")
	require.NoError(t, err)
	list, err := e.svc.BusinessStatements(e.ctx, e.biz, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, list.Items, 2)
	require.Equal(t, api.BusinessPayoutStateStatusPaid, list.Items[0].Payout.Status)
	require.Equal(t, "AXIS12345678", *list.Items[0].Payout.Utr)
	require.Equal(t, api.BusinessPayoutStateStatusCarried, list.Items[1].Payout.Status)
	require.Nil(t, list.Items[1].Payout.Utr)
}
