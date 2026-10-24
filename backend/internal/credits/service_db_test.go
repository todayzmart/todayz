package credits

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	walletapi "github.com/todayz/backend/gen/api/wallet"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// recNotices records the notices the service writes.
type recNotices struct {
	mu   sync.Mutex
	sent []notice.Notification
}

func (r *recNotices) Send(_ context.Context, _ pgx.Tx, n notice.Notification) (ids.ID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, n)
	return ids.New(time.Unix(int64(len(r.sent)), 0)), nil
}

func (r *recNotices) of(kind string) []notice.Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []notice.Notification
	for _, n := range r.sent {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

type denv struct {
	t       *testing.T
	ctx     context.Context
	tdb     *testkit.DB
	clk     *clock.Fake
	st      *settings.Cache
	svc     *Service
	notices *recNotices
	gen     *ids.Generator
	biz     ids.ID
	codes   int
}

// 12:00 IST on Mon 1 Jun 2026.
var dbStart = time.Date(2026, 6, 1, 6, 30, 0, 0, time.UTC)

func newDBEnv(t *testing.T) *denv {
	t.Helper()
	tdb := testkit.NewDB(t)
	clk := clock.NewFake(dbStart)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := &denv{
		t: t, ctx: context.Background(), tdb: tdb, clk: clk, gen: ids.NewGenerator(), notices: &recNotices{},
		st: settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{TTL: time.Hour, Log: log}),
	}
	svc, err := New(Deps{DB: db.New(tdb.Pool), Clock: clk, Settings: e.st, Notices: e.notices})
	require.NoError(t, err)
	e.svc = svc
	e.biz = e.gen.New(clk.Now())
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code) VALUES ($1, 'Lakshmi Traders', $2)`, ids.UUID(e.biz), e.code())
	return e
}

const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// code is a fresh referral or short code (^[A-HJKMNP-Z2-9]{6}$).
func (e *denv) code() string {
	e.codes++
	n, b := e.codes, make([]byte, 6)
	for i := 5; i >= 0; i-- {
		b[i] = codeAlphabet[n%len(codeAlphabet)]
		n /= len(codeAlphabet)
	}
	return string(b)
}

func (e *denv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err)
}

func (e *denv) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, sql, args...).Scan(&n))
	return n
}

func (e *denv) setting(key settings.Key, value string) {
	e.t.Helper()
	e.exec(`UPDATE platform_settings SET value = $2 WHERE key = $1`, string(key), value)
	e.st.Invalidate()
}

func (e *denv) user() ids.ID {
	e.t.Helper()
	id := e.gen.New(e.clk.Now())
	e.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(id))
	return id
}

func (e *denv) store(name string, bp int) ids.ID {
	e.t.Helper()
	id := e.gen.New(e.clk.Now())
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-")) + "-" + strings.ToLower(e.code())
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, cashback_bp) VALUES ($1, $2, $3, $4, $5, $6)`,
		ids.UUID(id), ids.UUID(e.biz), slug, e.code(), name, bp)
	return id
}

// tx runs fn in one transaction, as a caller of Holder does.
func (e *denv) tx(fn func(ctx context.Context) error) error {
	return e.svc.db.InTx(e.ctx, fn)
}

func (e *denv) hold(ref Ref, c, s ids.ID, n int, eligible int64) Allowance {
	e.t.Helper()
	var a Allowance
	require.NoError(e.t, e.tx(func(ctx context.Context) error {
		var err error
		a, err = e.svc.Hold(ctx, HoldRequest{Ref: ref, CustomerID: c, StoreID: s, Credits: n, EligiblePaise: eligible})
		return err
	}))
	return a
}

func (e *denv) release(ref Ref) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.tx(func(ctx context.Context) error {
		var err error
		n, err = e.svc.Release(ctx, ref)
		return err
	}))
	return n
}

func (e *denv) complete(ref Ref, c, s ids.ID, eligible int64) Completed {
	e.t.Helper()
	var out Completed
	require.NoError(e.t, e.tx(func(ctx context.Context) error {
		var err error
		out, err = e.svc.Complete(ctx, Completion{Ref: ref, CustomerID: c, StoreID: s, EligiblePaise: eligible})
		return err
	}))
	return out
}

func (e *denv) post(p Posting) (Posted, error) {
	var out Posted
	err := e.tx(func(ctx context.Context) error {
		var err error
		out, err = e.svc.Post(ctx, p)
		return err
	})
	return out, err
}

// grant credits a customer n credits (a referral reward, a fresh lot).
func (e *denv) grant(c ids.ID, n int64) {
	e.t.Helper()
	_, err := e.post(Posting{Kind: TxReferralReward, IdempotencyKey: "referral:grant:" + e.gen.New(e.clk.Now()).String(),
		Legs: []Leg{{Account{Kind: PlatformMarketing}, -n}, {wallet(c), n}}})
	require.NoError(e.t, err)
}

func (e *denv) balance(a Account) int64 {
	e.t.Helper()
	var owner any
	if a.Owner != ids.Zero {
		owner = ids.UUID(a.Owner)
	}
	var n int64
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT coalesce(sum(b.balance), 0)::bigint
		FROM ledger_accounts a JOIN account_balances b ON b.account_id = a.id
		WHERE a.kind = $1 AND a.owner_id IS NOT DISTINCT FROM $2`, string(a.Kind), owner).Scan(&n))
	return n
}

func (e *denv) clean() {
	e.t.Helper()
	d, err := e.svc.Reconcile(e.ctx)
	require.NoError(e.t, err)
	require.True(e.t, d.Clean(), "reconcile: %v", d)
}

func (e *denv) order() Ref { return Ref{Type: RefOrder, ID: e.gen.New(e.clk.Now())} }

// CR-01, CR-04, CR-05, CR-23, CR-25: hold, release to the same lots,
// capture and cashback on what was paid.
func TestHoldReleaseCaptureDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 500)
	e.grant(cust, 20)
	e.clk.Advance(time.Hour)
	e.grant(cust, 80)

	o1 := e.order()
	a := e.hold(o1, cust, shop, 30, 25000)
	require.Equal(t, 30, a.Allowed, "CR-01")
	require.Equal(t, LimitNone, a.Limit)
	w, err := e.svc.Wallet(e.ctx, cust)
	require.NoError(t, err)
	require.Equal(t, 70, w.AvailableCredits)
	require.Equal(t, 30, w.HeldCredits)
	require.Equal(t, Allowance{Allowed: 30, Max: 30}, e.hold(o1, cust, shop, 30, 25000), "a repeat returns the hold")

	require.Equal(t, 30, e.release(o1), "CR-05")
	require.Equal(t, 0, e.release(o1), "a second release returns nothing")
	require.EqualValues(t, 100, e.balance(wallet(cust)))
	require.EqualValues(t, 0, e.balance(held(cust)))
	require.Equal(t, 2, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1`, ids.UUID(cust)), "CR-25: no new lot")
	require.Equal(t, 1, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1 AND original = 20 AND remaining = 20 AND held = 0`, ids.UUID(cust)))

	o2 := e.order()
	e.hold(o2, cust, shop, 30, 30000)
	done := e.complete(o2, cust, shop, 30000)
	require.Equal(t, Completed{Captured: 30, Cashback: 13}, done, "CR-04, CR-23: 5%% of ₹270")
	require.Equal(t, done, e.complete(o2, cust, shop, 30000), "a repeat posts nothing")
	require.EqualValues(t, 30-13, e.balance(store(shop)))
	require.EqualValues(t, 70+13, e.balance(wallet(cust)))
	require.Len(t, e.notices.of(notice.KindCreditsEarned), 1)
	require.Equal(t, "13", e.notices.of(notice.KindCreditsEarned)[0].Params["credits"])
	require.Equal(t, 1, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1 AND source_kind = 'cashback' AND original = 13`, ids.UUID(cust)),
		"CR-25: cashback creates a lot")

	// A completion for another customer's hold is refused.
	other := e.user()
	err = e.tx(func(ctx context.Context) error {
		_, err := e.svc.Complete(ctx, Completion{Ref: o2, CustomerID: other, StoreID: shop, EligiblePaise: 30000})
		return err
	})
	require.ErrorIs(t, err, ErrInvalid)
	e.clean()
}

// CR-06, CR-07, CR-16: cashback on an order without credits; 0% posts
// nothing; the store's balance may go negative.
func TestCashbackDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop, zero := e.user(), e.store("Ganesh Stores", 500), e.store("Zero Mart", 0)
	require.Equal(t, Completed{Cashback: 24}, e.complete(e.order(), cust, shop, 48700), "CR-06: floor(5% of ₹487)")
	require.EqualValues(t, -24, e.balance(store(shop)), "CR-16")
	before := e.count(`SELECT count(*) FROM ledger_transactions`)
	require.Equal(t, Completed{}, e.complete(e.order(), cust, zero, 48700), "CR-07")
	require.Equal(t, before, e.count(`SELECT count(*) FROM ledger_transactions`))

	// D-124: a completion's own rate (the order's snapshot) wins over the
	// store's rate now, both ways; a rate out of range is refused.
	withBP := func(bp int, s ids.ID) (Completed, error) {
		var out Completed
		err := e.tx(func(ctx context.Context) error {
			var err error
			out, err = e.svc.Complete(ctx, Completion{Ref: e.order(), CustomerID: cust, StoreID: s, EligiblePaise: 48700, CashbackBP: &bp})
			return err
		})
		return out, err
	}
	out, err := withBP(1000, shop)
	require.NoError(t, err)
	require.Equal(t, Completed{Cashback: 48}, out, "10% snapshotted, the store shows 5% now")
	out, err = withBP(0, shop)
	require.NoError(t, err)
	require.Equal(t, Completed{}, out, "0% snapshotted pays nothing at a 5% store")
	out, err = withBP(500, zero)
	require.NoError(t, err)
	require.Equal(t, Completed{Cashback: 24}, out, "5% snapshotted at a store now at 0%")
	for _, bp := range []int{-1, 2001} {
		_, err = withBP(bp, shop)
		require.ErrorIs(t, err, ErrInvalid, "bp %d", bp)
	}
	e.clean()
}

// CR-02, CR-21, CR-22: the per-order max, the credit minimum and the ₹1
// floor, from settings.
func TestAllowLimitsDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 0)
	e.grant(cust, 200)
	allow := func(n int, eligible int64) Allowance {
		t.Helper()
		a, err := e.svc.Allow(e.ctx, cust, shop, n, eligible)
		require.NoError(t, err)
		return a
	}
	require.Equal(t, LimitMaxExceeded, allow(31, 50000).Limit, "CR-02")
	require.Equal(t, 30, allow(31, 50000).Allowed)
	require.Equal(t, LimitBelowMinimum, allow(10, 9999).Limit, "CR-21")
	require.Equal(t, LimitBelowMinimum, allow(10, 9900).Limit, "CR-21: ₹120 less a ₹21 offer")
	require.Equal(t, 10, allow(10, 10000).Allowed)
	require.Equal(t, 10, allow(10, 10001).Allowed)

	a := e.hold(e.order(), cust, shop, 10, 9999)
	require.Equal(t, Allowance{Available: 200, Limit: LimitBelowMinimum}, a)
	require.EqualValues(t, 0, e.balance(held(cust)))

	// CR-22: with the max above the items, credits stop ₹1 short of them.
	e.setting(settings.CreditMaxPerOrder, "500")
	require.Equal(t, 149, allow(200, 15000).Allowed)
	require.Equal(t, LimitMaxExceeded, allow(200, 15000).Limit)
	// The minimum is a setting too.
	e.setting(settings.CreditMinOrderValuePaise, "20000")
	require.Equal(t, LimitBelowMinimum, allow(10, 15000).Limit)
}

// CR-10, CR-11, CR-26: the customer's monthly cap and the store's daily cap
// on IST boundaries, each refusal flagged.
func TestCapsDB(t *testing.T) {
	e := newDBEnv(t)
	e.setting(settings.CreditOrdersPerMonth, "2")
	e.setting(settings.StoreCreditRedemptionsPerDay, "2")
	ist := func(y int, m time.Month, d, h, min, s int) time.Time {
		return time.Date(y, m, d, h, min, s, 0, clock.IST)
	}

	// The store's day: two redemptions by 23:59:59 IST on 1 Jun.
	shop := e.store("Lakshmi Traders", 0)
	c1, c2, c3 := e.user(), e.user(), e.user()
	for _, c := range []ids.ID{c1, c2, c3} {
		e.grant(c, 100)
	}
	e.clk.Set(ist(2026, 6, 1, 23, 0, 0))
	require.Equal(t, 10, e.hold(e.order(), c1, shop, 10, 20000).Allowed)
	e.clk.Set(ist(2026, 6, 1, 23, 59, 59))
	require.Equal(t, 10, e.hold(e.order(), c2, shop, 10, 20000).Allowed)
	a := e.hold(e.order(), c3, shop, 10, 20000)
	require.Equal(t, LimitStoreCap, a.Limit, "CR-11")
	require.Equal(t, 0, a.Allowed)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM risk_flags WHERE subject_type = 'store' AND subject_id = $1`, ids.UUID(shop)),
		"CR-11: flagged although the caller's transaction committed nothing")
	e.clk.Set(ist(2026, 6, 2, 0, 0, 0))
	require.Equal(t, 10, e.hold(e.order(), c3, shop, 10, 20000).Allowed, "CR-26: a new IST day")

	// The customer's month: two credit orders by 23:59 IST on 31 Aug.
	cust := e.user()
	e.grant(cust, 100)
	shops := []ids.ID{e.store("A Mart", 0), e.store("B Mart", 0), e.store("C Mart", 0)}
	e.clk.Set(ist(2026, 8, 31, 10, 0, 0))
	o1 := e.order()
	require.Equal(t, 10, e.hold(o1, cust, shops[0], 10, 20000).Allowed)
	e.clk.Set(ist(2026, 8, 31, 23, 59, 0))
	require.Equal(t, 10, e.hold(e.order(), cust, shops[1], 10, 20000).Allowed)
	quote, err := e.svc.Allow(e.ctx, cust, shops[2], 0, 20000)
	require.NoError(t, err)
	require.Equal(t, LimitMonthlyCap, quote.Limit, "CR-10: the quote says so before placing")
	require.Equal(t, LimitMonthlyCap, e.hold(e.order(), cust, shops[2], 10, 20000).Limit, "CR-10")
	require.Equal(t, 1, e.count(`SELECT count(*) FROM risk_flags WHERE subject_type = 'user' AND subject_id = $1`, ids.UUID(cust)))
	// A released hold no longer counts.
	e.release(o1)
	require.Equal(t, 10, e.hold(e.order(), cust, shops[2], 10, 20000).Allowed)
	require.Equal(t, LimitMonthlyCap, e.hold(e.order(), cust, shops[2], 10, 20000).Limit)
	e.clk.Set(ist(2026, 9, 1, 0, 0, 0))
	require.Equal(t, 10, e.hold(e.order(), cust, shops[2], 10, 20000).Allowed, "CR-26: a new IST month")
	e.clean()
}

// CR-12: 50 goroutines spend one wallet of 100, 30 each, half by holds and
// half by debits: at most 3 succeed and the wallet never goes below 0.
// CR-29 (builder): a hold and a debit racing for the same credits.
func TestConcurrentSpendDB(t *testing.T) {
	e := newDBEnv(t)
	cust := e.user()
	e.grant(cust, 100)
	shops := make([]ids.ID, 50)
	refs := make([]Ref, 50)
	for i := range shops {
		shops[i], refs[i] = e.store("Shop", 0), e.order()
	}
	var ok atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 50)
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				var a Allowance
				err := e.tx(func(ctx context.Context) error {
					var err error
					a, err = e.svc.Hold(ctx, HoldRequest{Ref: refs[i], CustomerID: cust, StoreID: shops[i], Credits: 30, EligiblePaise: 50000})
					return err
				})
				if err != nil {
					errs <- err
				} else if a.Allowed == 30 {
					ok.Add(1)
				}
				return
			}
			_, err := e.post(Posting{Kind: TxAdminAdjustment, IdempotencyKey: "adjust:race:" + refs[i].ID.String(),
				Legs: []Leg{{wallet(cust), -30}, {Account{Kind: PlatformAdjustments}, 30}}})
			switch {
			case err == nil:
				ok.Add(1)
			case !errors.Is(err, ErrInsufficient):
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 3, ok.Load())
	require.EqualValues(t, 10, e.balance(wallet(cust)))
	require.EqualValues(t, 90, e.balance(held(cust))+e.balance(Account{Kind: PlatformAdjustments}), "the 3 winners spent 90")
	e.clean()
}

// D-120: N customers hold at one store at once with the store's daily cap at
// N-1. Hold locks the store's row, so the cap is counted under the lock and
// exactly one hold is refused.
func TestConcurrentStoreCapDB(t *testing.T) {
	const n = 12
	e := newDBEnv(t)
	e.setting(settings.StoreCreditRedemptionsPerDay, strconv.Itoa(n-1))
	shop := e.store("Shop", 0)
	custs := make([]ids.ID, n)
	refs := make([]Ref, n)
	for i := range custs {
		custs[i], refs[i] = e.user(), e.order()
		e.grant(custs[i], 50)
	}
	var held, capped atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var a Allowance
			err := e.tx(func(ctx context.Context) error {
				var err error
				a, err = e.svc.Hold(ctx, HoldRequest{Ref: refs[i], CustomerID: custs[i], StoreID: shop, Credits: 20, EligiblePaise: 50000})
				return err
			})
			switch {
			case err != nil:
				errs <- err
			case a.Allowed == 20:
				held.Add(1)
			case a.Allowed == 0 && a.Limit == LimitStoreCap:
				capped.Add(1)
			default:
				errs <- fmt.Errorf("unexpected allowance %+v", a)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, n-1, held.Load())
	require.EqualValues(t, 1, capped.Load(), "exactly one hold over the cap")
	require.Equal(t, n-1, e.count(`SELECT count(*) FROM credit_holds WHERE store_id = $1 AND status = 'held'`, ids.UUID(shop)))
	e.clean()
}

// CR-08, CR-09: FIFO spending, expiry of what is left, the reminder once.
func TestExpiryAndReminderDB(t *testing.T) {
	e := newDBEnv(t)
	cust := e.user()
	e.grant(cust, 30) // L1, 1 Jun
	e.clk.Advance(10 * 24 * time.Hour)
	e.grant(cust, 20) // L2, 11 Jun
	_, err := e.post(Posting{Kind: TxAdminAdjustment, IdempotencyKey: "adjust:spend5",
		Legs: []Leg{{wallet(cust), -5}, {Account{Kind: PlatformAdjustments}, 5}}})
	require.NoError(t, err)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1 AND original = 30 AND remaining = 25`, ids.UUID(cust)),
		"CR-09: the oldest lot is spent first")

	// 365 days: L1 is usable through 1 Jun 2027 IST and expires at 00:00 IST on 2 Jun.
	l1Expiry := time.Date(2027, 6, 2, 0, 0, 0, 0, clock.IST)
	e.clk.Set(l1Expiry.Add(-6 * 24 * time.Hour))
	n, err := e.svc.RemindExpiring(e.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	got := e.notices.of(notice.KindCreditsExpiring)
	require.Len(t, got, 1)
	require.Equal(t, map[string]string{"credits": "25", "date": "1 Jun 2027"}, got[0].Params, "CR-08: only L1 is due within 7 days")
	n, err = e.svc.RemindExpiring(e.ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "a lot is reminded once")

	e.clk.Set(l1Expiry.Add(-time.Second))
	cu, cr, err := e.svc.ExpireDue(e.ctx)
	require.NoError(t, err)
	require.Equal(t, [2]int{0, 0}, [2]int{cu, cr}, "not yet")
	e.clk.Set(l1Expiry)
	cu, cr, err = e.svc.ExpireDue(e.ctx)
	require.NoError(t, err)
	require.Equal(t, [2]int{1, 25}, [2]int{cu, cr}, "CR-08: the partly spent lot expires")
	cu, _, err = e.svc.ExpireDue(e.ctx)
	require.NoError(t, err)
	require.Equal(t, 0, cu, "a second run expires nothing")
	require.EqualValues(t, 20, e.balance(wallet(cust)))
	require.EqualValues(t, 25, e.balance(Account{Kind: PlatformExpired}))
	e.clean()
}

// CR-24: a lot expiring while held. Captured after expiry → nothing expires;
// released after expiry → straight to platform_expired.
func TestExpiryWhileHeldDB(t *testing.T) {
	for _, settle := range []string{"collected", "cancelled"} {
		t.Run(settle, func(t *testing.T) {
			e := newDBEnv(t)
			cust, shop := e.user(), e.store("Lakshmi Traders", 0)
			e.grant(cust, 30)
			expiry := time.Date(2027, 6, 2, 0, 0, 0, 0, clock.IST)
			e.clk.Set(expiry.Add(-time.Hour))
			ref := e.order()
			require.Equal(t, 30, e.hold(ref, cust, shop, 30, 50000).Allowed)
			e.clk.Set(expiry)
			_, cr, err := e.svc.ExpireDue(e.ctx)
			require.NoError(t, err)
			require.Equal(t, 0, cr, "the held portion is skipped")
			e.clean()
			e.clk.Advance(time.Hour)
			if settle == "collected" {
				require.Equal(t, 30, e.complete(ref, cust, shop, 50000).Captured)
				require.EqualValues(t, 0, e.balance(Account{Kind: PlatformExpired}))
				require.EqualValues(t, 30, e.balance(store(shop)))
			} else {
				require.Equal(t, 30, e.release(ref))
				require.EqualValues(t, 30, e.balance(Account{Kind: PlatformExpired}))
				require.EqualValues(t, 0, e.balance(wallet(cust)), "the wallet does not regain them")
			}
			e.clean()
		})
	}
}

// CR-17: a partial accept re-caps the hold; the difference goes back to the
// same lots, latest expiry first.
func TestRecapDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 0)
	e.grant(cust, 20) // L1
	e.clk.Advance(24 * time.Hour)
	e.grant(cust, 10) // L2
	ref := e.order()
	require.Equal(t, 30, e.hold(ref, cust, shop, 30, 50000).Allowed)
	recap := func(eligible int64) int {
		t.Helper()
		var n int
		require.NoError(t, e.tx(func(ctx context.Context) error {
			var err error
			n, err = e.svc.Recap(ctx, ref, eligible)
			return err
		}))
		return n
	}
	require.Equal(t, 30, recap(40000), "still above the cap: all stay held")
	e.setting(settings.CreditMaxPerOrder, "25")
	require.Equal(t, 25, recap(40000))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1 AND original = 10 AND remaining = 5 AND held = 5`, ids.UUID(cust)),
		"latest expiry first")
	require.Equal(t, 0, recap(9999), "under the minimum: all released")
	require.EqualValues(t, 30, e.balance(wallet(cust)))
	require.EqualValues(t, 0, e.balance(held(cust)))
	require.Equal(t, 2, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1 AND remaining = original AND held = 0`, ids.UUID(cust)))
	e.clean()
}

// CR-18: a bill QR claim holds, then captures when the sale completes; an
// expired claim releases.
func TestBillClaimDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 0)
	e.grant(cust, 60)
	claim := Ref{Type: RefBillClaim, ID: e.gen.New(e.clk.Now())}
	require.Equal(t, 30, e.hold(claim, cust, shop, 30, 40000).Allowed)
	require.Equal(t, 30, e.complete(claim, cust, shop, 40000).Captured)
	lapsed := Ref{Type: RefBillClaim, ID: e.gen.New(e.clk.Now())}
	require.Equal(t, 20, e.hold(lapsed, cust, shop, 20, 40000).Allowed)
	require.Equal(t, 20, e.release(lapsed))
	lines, err := e.svc.WalletLines(e.ctx, cust, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Returned: the bill was not confirmed in time", lines.Items[0].Text)
	require.Equal(t, walletapi.BillClaimExpired, *lines.Items[0].Params.Reason)
	e.clean()
}

// Poster: the posting rules, duplicates and the payout shape.
func TestPostDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 0)
	mk := Account{Kind: PlatformMarketing}
	bad := map[string]Posting{
		"unbalanced":    {Kind: TxReferralReward, IdempotencyKey: "referral:u", Legs: []Leg{{mk, -10}, {wallet(cust), 9}}},
		"held leg":      {Kind: TxAdminAdjustment, IdempotencyKey: "adjust:h", Legs: []Leg{{held(cust), 10}, {Account{Kind: PlatformAdjustments}, -10}}},
		"zero leg":      {Kind: TxReferralReward, IdempotencyKey: "referral:z", Legs: []Leg{{mk, 0}, {wallet(cust), 0}}},
		"own kind":      {Kind: TxCashback, IdempotencyKey: "cashback:c", Legs: []Leg{{store(shop), -10}, {wallet(cust), 10}}},
		"no key":        {Kind: TxReferralReward, Legs: []Leg{{mk, -10}, {wallet(cust), 10}}},
		"payout wallet": {Kind: TxPayout, IdempotencyKey: "payout:p", Legs: []Leg{{wallet(cust), -10}, {Account{Kind: PlatformPayoutsCash}, 10}}},
		// D-122: each kind's own prefix, so a key never collides with the
		// internal hold:, expiry: and forfeit: keys.
		"no prefix":    {Kind: TxReferralReward, IdempotencyKey: "r1", Legs: []Leg{{mk, -10}, {wallet(cust), 10}}},
		"other prefix": {Kind: TxReferralReward, IdempotencyKey: "adjust:1", Legs: []Leg{{mk, -10}, {wallet(cust), 10}}},
		"internal key": {Kind: TxAdminAdjustment, IdempotencyKey: "hold:x", Legs: []Leg{{wallet(cust), 10}, {Account{Kind: PlatformAdjustments}, -10}}},
		"prefix only":  {Kind: TxReferralReward, IdempotencyKey: "referral:", Legs: []Leg{{mk, -10}, {wallet(cust), 10}}},
	}
	for name, p := range bad {
		_, err := e.post(p)
		require.ErrorIs(t, err, ErrInvalid, name)
	}
	reward := Posting{Kind: TxReferralReward, IdempotencyKey: "referral:1", RefType: "referral", RefID: e.gen.New(e.clk.Now()),
		Legs: []Leg{{mk, -30}, {wallet(cust), 30}}}
	first, err := e.post(reward)
	require.NoError(t, err)
	require.False(t, first.Duplicate)
	again, err := e.post(reward)
	require.NoError(t, err)
	require.Equal(t, Posted{TransactionID: first.TransactionID, Duplicate: true}, again)
	require.EqualValues(t, 30, e.balance(wallet(cust)))
	// D-122: the same key with other legs is an error, not a duplicate.
	other := reward
	other.Legs = []Leg{{mk, -31}, {wallet(cust), 31}}
	_, err = e.post(other)
	require.ErrorIs(t, err, ErrKeyReused)
	other.Legs = []Leg{{mk, -30}, {wallet(e.user()), 30}}
	_, err = e.post(other)
	require.ErrorIs(t, err, ErrKeyReused, "another customer")
	require.EqualValues(t, 30, e.balance(wallet(cust)))

	_, err = e.post(Posting{Kind: TxAdminAdjustment, IdempotencyKey: "adjust:1",
		Legs: []Leg{{wallet(cust), -31}, {Account{Kind: PlatformAdjustments}, 31}}})
	require.ErrorIs(t, err, ErrInsufficient)

	// A payout settles what the platform owes the store: store_credit down.
	_, err = e.post(Posting{Kind: TxPayout, IdempotencyKey: "payout:1",
		Legs: []Leg{{store(shop), -50}, {Account{Kind: PlatformPayoutsCash}, 50}}})
	require.NoError(t, err)
	require.EqualValues(t, -50, e.balance(store(shop)))
	_, err = e.post(Posting{Kind: TxPayout, IdempotencyKey: "payout:2",
		Legs: []Leg{{store(shop), 50}, {Account{Kind: PlatformPayoutsCash}, -50}}})
	require.ErrorIs(t, err, ErrInvalid, "a payout never raises store_credit")
	e.clean()
}

// CR-28: the deletion forfeiture; held credits block it.
func TestForfeitOnDeleteDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 0)
	e.grant(cust, 50)
	e.clk.Advance(time.Hour)
	e.grant(cust, 33)
	del := func() error {
		return e.tx(func(ctx context.Context) error { return e.svc.OnAccountDelete(ctx, cust) })
	}
	ref := e.order()
	e.hold(ref, cust, shop, 10, 20000)
	require.ErrorIs(t, del(), ErrHeld, "an open hold blocks the deletion")
	e.release(ref)

	require.NoError(t, del())
	require.NoError(t, del(), "twice → one posting")
	require.Equal(t, 1, e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'forfeit_deletion'`))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM ledger_entries en JOIN ledger_transactions t ON t.id = en.transaction_id
		WHERE t.kind = 'forfeit_deletion' AND en.amount = 83`))
	require.EqualValues(t, 0, e.balance(wallet(cust)))
	require.EqualValues(t, 83, e.balance(Account{Kind: PlatformExpired}))
	require.Equal(t, 0, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1 AND remaining > 0`, ids.UUID(cust)), "lots closed")
	require.NoError(t, e.tx(func(ctx context.Context) error { return e.svc.OnAccountDelete(ctx, e.user()) }),
		"no wallet: nothing to do")
	e.clean()
}

// CR-19, D-123: ledger rows are append-only for the app role, and balances
// and lots move only through the entry and consumption triggers.
func TestAppendOnlyDB(t *testing.T) {
	e := newDBEnv(t)
	e.grant(e.user(), 10)
	for _, sql := range []string{
		`UPDATE ledger_entries SET amount = amount`,
		`DELETE FROM ledger_entries`,
		`UPDATE ledger_transactions SET memo = 'x'`,
		`DELETE FROM ledger_transactions`,
		`DELETE FROM account_balances`,
		`UPDATE account_balances SET balance = balance + 1`,
		`UPDATE account_balances SET kind = 'platform_expired'`,
		`INSERT INTO account_balances (account_id, kind, balance) VALUES (gen_random_uuid(), 'customer_wallet', 5)`,
		`UPDATE credit_lot_consumptions SET remaining_delta = 0`,
		`DELETE FROM credit_lot_consumptions`,
		`UPDATE credit_lots SET remaining = remaining + 1`,
		`UPDATE credit_lots SET held = held + 1`,
		`UPDATE credit_lots SET original = original + 1`,
		`DELETE FROM credit_lots`,
		`DELETE FROM credit_holds`,
	} {
		_, err := e.tdb.Pool.Exec(e.ctx, sql)
		var pg *pgconn.PgError
		require.ErrorAs(t, err, &pg, sql)
		require.Equal(t, "42501", pg.Code, "%s: permission denied", sql)
	}
}

// CR-14: a corrupted balance row shows on the reconcile and is not fixed.
func TestReconcileDB(t *testing.T) {
	e := newDBEnv(t)
	cust := e.user()
	e.grant(cust, 40)
	e.clean()
	require.NoError(t, e.tdb.AdminExec(e.ctx, `ALTER TABLE account_balances DISABLE TRIGGER USER;
		UPDATE account_balances SET balance = balance + 1 WHERE account_id IN
		  (SELECT id FROM ledger_accounts WHERE kind = 'customer_wallet');
		ALTER TABLE account_balances ENABLE TRIGGER USER`))
	d, err := e.svc.Reconcile(e.ctx)
	require.NoError(t, err)
	require.False(t, d.Clean())
	require.EqualValues(t, 1, d[DriftCacheDrift])
	require.EqualValues(t, 1, d[DriftLotsVsBalances])
	require.EqualValues(t, 41, e.balance(wallet(cust)), "nothing auto-fixed")
}

// The wallet lines and the business account in plain words (C-16, B-10).
func TestReadsDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 250)
	e.grant(cust, 100)
	e.clk.Advance(time.Minute)
	ref := e.order()
	e.hold(ref, cust, shop, 30, 30000)
	e.clk.Advance(time.Minute)
	e.release(ref)
	e.clk.Advance(time.Minute)
	ref2 := e.order()
	e.hold(ref2, cust, shop, 20, 30000)
	e.clk.Advance(time.Minute)
	e.complete(ref2, cust, shop, 30000)

	all, err := e.svc.WalletLines(e.ctx, cust, nil, nil)
	require.NoError(t, err)
	var texts []string
	for _, l := range all.Items {
		texts = append(texts, l.Text)
	}
	require.Equal(t, []string{
		"2.5% back at Lakshmi Traders",
		"Spent at Lakshmi Traders",
		"Held for your order at Lakshmi Traders",
		"Returned to your wallet",
		"Held for your order at Lakshmi Traders",
		"Referral bonus",
	}, texts)
	require.Equal(t, 7, all.Items[0].Credits, "floor(2.5% of ₹280)")
	require.Equal(t, -20, all.Items[1].Credits, "the capture leaves the customer's held credits")
	require.Equal(t, 0, all.Items[1].AvailableCredits)
	require.Equal(t, -30, all.Items[4].AvailableCredits)
	require.Nil(t, all.NextCursor)

	two := 2
	p1, err := e.svc.WalletLines(e.ctx, cust, &two, nil)
	require.NoError(t, err)
	require.Len(t, p1.Items, 2)
	require.NotNil(t, p1.NextCursor)
	p2, err := e.svc.WalletLines(e.ctx, cust, &two, p1.NextCursor)
	require.NoError(t, err)
	require.Equal(t, all.Items[2].Id, p2.Items[0].Id)
	bad := "nope"
	_, err = e.svc.WalletLines(e.ctx, cust, nil, &bad)
	require.Error(t, err)

	acct, err := e.svc.CreditAccount(e.ctx, e.biz, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 20-7, acct.BalanceCredits)
	require.Len(t, acct.Lines, 2)
	require.Equal(t, "Cashback paid", acct.Lines[0].Text)
	require.Equal(t, -7, acct.Lines[0].Credits)
	require.Equal(t, "Credits redeemed", acct.Lines[1].Text)

	// D-125: a member limited to some stores sees only those.
	other := e.store("Ganesh Stores", 0)
	acct, err = e.svc.CreditAccount(e.ctx, e.biz, []ids.ID{other}, nil, nil)
	require.NoError(t, err)
	require.Len(t, acct.Stores, 1)
	require.Equal(t, other.String(), acct.Stores[0].StoreId)
	require.Zero(t, acct.BalanceCredits)
	require.Empty(t, acct.Lines)
	acct, err = e.svc.CreditAccount(e.ctx, e.biz, []ids.ID{}, nil, nil)
	require.NoError(t, err)
	require.Empty(t, acct.Stores, "no stores: nothing")
	require.Empty(t, acct.Lines)
	acct, err = e.svc.CreditAccount(e.ctx, e.biz, []ids.ID{shop}, nil, nil)
	require.NoError(t, err)
	require.Len(t, acct.Stores, 1)
	require.Equal(t, 20-7, acct.BalanceCredits)
	require.Len(t, acct.Lines, 2)

	w, err := e.svc.Wallet(e.ctx, cust)
	require.NoError(t, err)
	require.Equal(t, walletapi.Wallet{
		AvailableCredits: 87, HeldCredits: 0, ExpiringSoon: []walletapi.ExpiringCredits{},
		MaxCreditsPerOrder: 30, MinOrderPaise: 10000, CreditOrdersPerMonth: 10, CreditOrdersThisMonth: 1,
		ServerNow: e.clk.Now(),
	}, w)
}

// CR-13 over the database: random sequences through the service keep the
// ledger invariants (the reconcile finds nothing) and never take a balance
// below 0. Fewer runs than the Fake's 10k: each step is real SQL.
func TestServiceInvariantsPropertyDB(t *testing.T) {
	e := newDBEnv(t)
	e.setting(settings.CreditOrdersPerMonth, "4")
	e.setting(settings.StoreCreditRedemptionsPerDay, "4")
	e.setting(settings.CreditExpiryDays, "3")
	rules, err := e.svc.Rules(e.ctx)
	require.NoError(t, err)
	propertyChecks(t, 150)
	rapid.Check(t, func(rt *rapid.T) {
		m := newLedgerMachine(rt, e.ctx, e.clk, rules)
		for _, c := range m.customers {
			e.exec(`INSERT INTO users (id) VALUES ($1)`, ids.UUID(c))
		}
		for _, s := range m.stores {
			bp := rapid.SampledFrom([]int{0, 50, 500, 2000}).Draw(rt, "bp")
			e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, cashback_bp) VALUES ($1, $2, $3, $4, 'Shop', $5)`,
				ids.UUID(s), ids.UUID(e.biz), "shop-"+strings.ToLower(e.code()), e.code(), bp)
		}
		// Fresh keys per run: the database outlives the run.
		for i := range m.keys {
			m.keys[i] = e.gen.New(e.clk.Now()).String()
		}
		h := txHolder{e}
		m.h, m.p = h, h
		m.expire = func(ctx context.Context) error { _, _, err := e.svc.ExpireDue(ctx); return err }
		m.forfeit = func(ctx context.Context, c ids.ID) error {
			return e.tx(func(ctx context.Context) error { return e.svc.OnAccountDelete(ctx, c) })
		}
		m.check = func() error {
			d, err := e.svc.Reconcile(e.ctx)
			if err != nil {
				return err
			}
			if !d.Clean() {
				return errors.New("reconcile: " + strings.ReplaceAll(strings.TrimSpace(strings.Join(dirty(d), " ")), "  ", " "))
			}
			return nil
		}
		rt.Repeat(m.actions())
	})
}

func dirty(d Drift) []string {
	var out []string
	for k, n := range d {
		if n != 0 {
			out = append(out, k)
		}
	}
	return out
}

// txHolder runs each Holder and Poster call in its own transaction.
type txHolder struct{ e *denv }

func (h txHolder) Allow(ctx context.Context, c, s ids.ID, n int, p int64) (Allowance, error) {
	return h.e.svc.Allow(ctx, c, s, n, p)
}

func (h txHolder) Hold(ctx context.Context, r HoldRequest) (a Allowance, err error) {
	err = h.e.tx(func(ctx context.Context) error { a, err = h.e.svc.Hold(ctx, r); return err })
	return a, err
}

func (h txHolder) Recap(ctx context.Context, r Ref, p int64) (n int, err error) {
	err = h.e.tx(func(ctx context.Context) error { n, err = h.e.svc.Recap(ctx, r, p); return err })
	return n, err
}

func (h txHolder) Release(ctx context.Context, r Ref) (n int, err error) {
	err = h.e.tx(func(ctx context.Context) error { n, err = h.e.svc.Release(ctx, r); return err })
	return n, err
}

func (h txHolder) Complete(ctx context.Context, c Completion) (out Completed, err error) {
	err = h.e.tx(func(ctx context.Context) error { out, err = h.e.svc.Complete(ctx, c); return err })
	return out, err
}

func (h txHolder) Post(ctx context.Context, p Posting) (out Posted, err error) {
	err = h.e.tx(func(ctx context.Context) error { out, err = h.e.svc.Post(ctx, p); return err })
	return out, err
}
