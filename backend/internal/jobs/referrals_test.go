package jobs

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/referrals"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// The referral rewards through the real credits ledger (D-169): the API's
// and the sweep's service both come from ReferralsService.

var refNow = time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)

type refEnv struct {
	t     *testing.T
	tdb   *testkit.DB
	ctx   context.Context
	clk   *clock.Fake
	cache *settings.Cache
	svc   *referrals.Service
	cs    *credits.Service
	pool  *db.DB
	jobs  *platformjobs.Fake
	flaky *flakyPoster
	codes int
}

// flakyPoster fails the next fail postings before they post, as a ledger
// that is down, and posts then fails with a real SQL error in the posting's
// own transaction the next sqlFail[side] postings of a side. before[side]
// runs once, outside the posting's transaction, just before a side posts.
type flakyPoster struct {
	p       credits.Poster
	mu      sync.Mutex
	fail    int
	sqlFail map[string]int
	before  map[string]func()
}

func (f *flakyPoster) Post(ctx context.Context, p credits.Posting) (credits.Posted, error) {
	f.mu.Lock()
	down := f.fail > 0
	if down {
		f.fail--
	}
	side := p.IdempotencyKey[strings.LastIndex(p.IdempotencyKey, ":")+1:]
	broken := f.sqlFail[side] > 0
	if broken {
		f.sqlFail[side]--
	}
	hook := f.before[side]
	delete(f.before, side)
	f.mu.Unlock()
	if down {
		return credits.Posted{}, errors.New("ledger down")
	}
	if hook != nil {
		hook()
	}
	out, err := f.p.Post(ctx, p)
	if err != nil || !broken {
		return out, err
	}
	tx, ok := db.TxFrom(ctx)
	if !ok {
		return credits.Posted{}, errors.New("flakyPoster: no transaction")
	}
	_, err = tx.Exec(ctx, `SELECT 1 / 0`)
	return credits.Posted{}, err
}

// newRefEnv builds the service with ReferralsService and the registered
// referral_reward kind on a fake queue, or, flaky, with the same ledger
// behind a poster that can fail and a test kind that runs the service's
// PostRewards as the registered one does.
func newRefEnv(t *testing.T, flaky bool) *refEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := &refEnv{t: t, tdb: tdb, ctx: context.Background(), clk: clock.NewFake(refNow), pool: db.New(tdb.Pool)}
	e.cache = settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	d := Deps{Pool: tdb.Pool, Clock: e.clk, Settings: e.cache,
		Config: &config.Config{APIPublicURL: "https://api.todayz.localhost", Keys: config.AppKeys{PIIHashKey: config.NewSecret(make([]byte, pii.KeySize))}}}
	var err error
	e.cs, err = CreditsService(d, nil)
	require.NoError(t, err)
	reg := platformjobs.NewRegistry()
	if !flaky {
		Register(reg, d)
		e.jobs = platformjobs.NewFake(reg, e.clk)
		e.svc, err = ReferralsService(d, nil, e.jobs)
		require.NoError(t, err)
		return e
	}
	platformjobs.Define(reg, platformjobs.Kind{
		Name: referrals.KindReward, Owner: "W2-03", Class: platformjobs.ClassMoney, Runbook: ledgerRunbook,
		Description: "test: posts through the flaky ledger", Timeout: 30 * time.Second, MaxAttempts: referralRewardAttempts,
		Handler: func(ctx context.Context, rn platformjobs.Run) (platformjobs.Result, error) {
			var args referrals.RewardArgs
			if err := rn.Decode(&args); err != nil {
				return platformjobs.Result{}, err
			}
			res, err := e.svc.PostRewards(ctx, args.ReferralID)
			return platformjobs.Result{Summary: map[string]int64{"posted": int64(res.Posted)}}, err
		},
	})
	e.jobs = platformjobs.NewFake(reg, e.clk)
	h, err := pii.NewHasher(make([]byte, pii.KeySize))
	require.NoError(t, err)
	e.flaky = &flakyPoster{p: e.cs, sqlFail: map[string]int{}, before: map[string]func(){}}
	e.svc, err = referrals.New(referrals.Deps{DB: e.pool, Clock: e.clk, Settings: e.cache, Hasher: h, Ledger: ReferralsLedger(e.flaky), Jobs: e.jobs,
		PublicURL: "https://api.todayz.localhost", CustomerApp: "https://app.todayz.localhost", BusinessApp: "https://business.todayz.localhost"})
	require.NoError(t, err)
	return e
}

// runJobs works the due jobs once each; every run succeeds.
func (e *refEnv) runJobs() []platformjobs.FakeRun {
	e.t.Helper()
	runs := e.jobs.RunDue(e.ctx)
	for _, r := range runs {
		require.Equal(e.t, platformjobs.StatusSucceeded, r.Status, "%s: %v", r.Run.Kind, r.Err)
	}
	return runs
}

func (e *refEnv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err, sql)
}

func (e *refEnv) scan(sql string, args []any, dst ...any) {
	e.t.Helper()
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, sql, args...).Scan(dst...), sql)
}

func (e *refEnv) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	e.scan(sql, args, &n)
	return n
}

func (e *refEnv) code() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	e.codes++
	n, b := e.codes, []byte("T00000")
	for i := 5; i >= 1; i-- {
		b[i] = alphabet[n%len(alphabet)]
		n /= len(alphabet)
	}
	return string(b)
}

func (e *refEnv) customer(joined time.Time) (ids.ID, string) {
	id, code := ids.New(joined), e.code()
	e.exec(`INSERT INTO users (id, created_at) VALUES ($1, $2)`, ids.UUID(id), joined)
	e.exec(`INSERT INTO customer_profiles (user_id, referral_code, created_at) VALUES ($1, $2, $3)`, ids.UUID(id), code, joined)
	return id, code
}

// shop is a verified business with one live store; the store's short code
// is its referral code.
func (e *refEnv) shop() (store ids.ID, code string) {
	biz := ids.New(refNow.Add(-30 * 24 * time.Hour))
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, created_at) VALUES ($1, 'Shop', $2, 'verified', $3)`,
		ids.UUID(biz), e.code(), refNow.Add(-30*24*time.Hour))
	store, code = ids.New(refNow.Add(-29*24*time.Hour)), e.code()
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, status, live_at) VALUES ($1, $2, $3, $4, 'Shop', 'live', $5)`,
		ids.UUID(store), ids.UUID(biz), "shop-"+strings.ToLower(code), code, refNow.Add(-29*24*time.Hour))
	return store, code
}

// claim makes a new customer who claims code; it returns them and the referral.
func (e *refEnv) claim(code string) (ids.ID, ids.ID) {
	e.t.Helper()
	u, _ := e.customer(refNow.Add(-time.Hour))
	family := ids.New(refNow)
	h := sha256.Sum256([]byte(family.String()))
	e.exec(`INSERT INTO sessions (id, user_id, app, family_id, refresh_hash, expires_at, last_used_at, created_at)
	  VALUES ($1, $2, 'customer', $3, $4, $5, $6, $6)`,
		ids.UUID(ids.New(refNow)), ids.UUID(u), ids.UUID(family), h[:], refNow.Add(24*time.Hour), refNow)
	res, err := e.svc.ClaimCustomer(e.ctx, u, family, referrals.ClaimInput{Code: code})
	require.NoError(e.t, err)
	require.True(e.t, res.Created)
	return u, res.ReferralID
}

// buy completes a qualifying first order of the customer.
func (e *refEnv) buy(customer ids.ID) referrals.Outcome {
	e.t.Helper()
	store, _ := e.shop()
	o := referrals.Outcome{RefType: referrals.RefOrder, RefID: ids.New(refNow), CustomerID: customer, StoreID: store, EligiblePaise: 10_000}
	require.NoError(e.t, e.svc.OnPurchaseCompleted(e.ctx, o))
	return o
}

// sweep runs the sweep d after the clock's time and returns what it rewarded.
func (e *refEnv) sweep(d time.Duration) int64 {
	e.t.Helper()
	e.clk.Advance(d)
	out, err := e.svc.Sweep(e.ctx)
	require.NoError(e.t, err)
	return out["rewarded"]
}

// grant credits a customer n credits from platform_marketing.
func (e *refEnv) grant(c ids.ID, n int64) {
	e.t.Helper()
	_, err := e.cs.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, IdempotencyKey: "referral:grant:" + ids.New(refNow).String(),
		Legs: []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -n}, {Account: credits.Account{Kind: credits.CustomerWallet, Owner: c}, Amount: n}}})
	require.NoError(e.t, err)
}

// ledger is a snapshot of the money a reversal must not move.
type ledgerSnap struct{ txs, marketing, referrerWallet, refereeWallet, lots, lotsLeft int }

func (e *refEnv) snap(referrer, referee ids.ID) ledgerSnap {
	return ledgerSnap{
		txs: e.count(`SELECT count(*) FROM ledger_transactions`), marketing: e.marketing(),
		referrerWallet: e.balance(credits.CustomerWallet, referrer), refereeWallet: e.balance(credits.CustomerWallet, referee),
		lots:     e.count(`SELECT count(*) FROM credit_lots`),
		lotsLeft: e.count(`SELECT coalesce(sum(remaining), 0)::int FROM credit_lots`),
	}
}

type refRow struct {
	status                string
	referrerTx, refereeTx pgtype.UUID
	referrerCr, refereeCr int
}

func (e *refEnv) referral(id ids.ID) refRow {
	var r refRow
	e.scan(`SELECT status, referrer_transaction_id, referee_transaction_id, referrer_credits, referee_credits FROM referrals WHERE id = $1`,
		[]any{ids.UUID(id)}, &r.status, &r.referrerTx, &r.refereeTx, &r.referrerCr, &r.refereeCr)
	return r
}

// balance is an account's balance (0 when it has never been used).
func (e *refEnv) balance(kind credits.AccountKind, owner ids.ID) int {
	return e.count(`SELECT coalesce(sum(b.balance), 0)::int FROM account_balances b JOIN ledger_accounts a ON a.id = b.account_id
	  WHERE a.kind = $1 AND a.owner_id = $2`, string(kind), ids.UUID(owner))
}

func (e *refEnv) marketing() int {
	return e.count(`SELECT coalesce(sum(b.balance), 0)::int FROM account_balances b JOIN ledger_accounts a ON a.id = b.account_id
	  WHERE a.kind = 'platform_marketing'`)
}

func (e *refEnv) rewardTxs(ref ids.ID) int {
	return e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'referral_reward' AND ref_type = 'referral' AND ref_id = $1`, ids.UUID(ref))
}

// A qualifying purchase posts nothing in its own transaction: it enqueues
// one referral_reward job, which posts both sides once (each in its own
// transaction) to the referee's wallet (with a lot) and the referrer's
// wallet; a retry and a re-sweep post nothing new.
func TestReferralRewardsPostToCreditsDB(t *testing.T) {
	e := newRefEnv(t, false)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	o := e.buy(referee)

	require.Equal(t, "qualified", e.referral(ref).status)
	require.Zero(t, e.rewardTxs(ref), "the purchase posts nothing")
	pending := e.jobs.Pending()
	require.Len(t, pending, 1)
	require.Equal(t, referrals.KindReward, pending[0].Kind)
	require.Equal(t, "referral:"+ref.String(), pending[0].UniqueKey)
	require.NoError(t, e.svc.OnPurchaseCompleted(e.ctx, o))
	require.Len(t, e.jobs.Pending(), 1, "unique per referral")

	runs := e.runJobs()
	require.Len(t, runs, 1)
	require.Equal(t, map[string]int64{"posted": 2, "waiting": 0}, runs[0].Result.Summary)
	r := e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.True(t, r.referrerTx.Valid)
	require.True(t, r.refereeTx.Valid)
	require.Equal(t, 2, e.rewardTxs(ref))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, r.referrerCr, e.balance(credits.CustomerWallet, referrer))
	require.Equal(t, -(r.refereeCr + r.referrerCr), e.marketing())
	// Whole credits, as they are: 1 credit = ₹1 on both sides (01 §7).
	require.Equal(t, 1, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1 AND source_kind = 'referral_reward'
	  AND original = $2 AND remaining = $2 AND source_transaction_id = $3`, ids.UUID(referee), r.refereeCr, r.refereeTx))
	// The exact key per side, and the referral as the ref.
	for side, tx := range map[string]pgtype.UUID{referrals.SideReferrer: r.referrerTx, referrals.SideReferee: r.refereeTx} {
		var key, refType string
		var refID pgtype.UUID
		e.scan(`SELECT idempotency_key, ref_type, ref_id FROM ledger_transactions WHERE id = $1 AND kind = 'referral_reward'`, []any{tx}, &key, &refType, &refID)
		require.Equal(t, "referral:"+ref.String()+":"+side, key)
		require.Equal(t, referrals.RewardKey(ref, side), key)
		require.Equal(t, "referral", refType)
		require.Equal(t, ids.UUID(ref), refID)
	}

	// The same purchase again, the job again, and the sweep post nothing.
	require.NoError(t, e.svc.OnPurchaseCompleted(e.ctx, o))
	_, err := e.jobs.RunEvent(e.ctx, referrals.KindReward, referrals.RewardArgs{ReferralID: ref})
	require.NoError(t, err)
	require.Zero(t, e.sweep(time.Minute))
	require.Equal(t, 2, e.rewardTxs(ref))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, -(r.refereeCr + r.referrerCr), e.marketing())
}

// A store referrer's reward goes to its store_credit (no lot); the
// customer's to the wallet.
func TestReferralRewardStoreCreditDB(t *testing.T) {
	e := newRefEnv(t, false)
	store, code := e.shop()
	referee, ref := e.claim(code)
	e.buy(referee)
	e.runJobs()

	r := e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.Equal(t, r.referrerCr, e.balance(credits.StoreCredit, store))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Zero(t, e.count(`SELECT count(*) FROM credit_lots WHERE source_transaction_id = $1`, r.referrerTx))
}

// An identical posting already in the ledger is a duplicate: the side
// records the first transaction and nothing new posts.
func TestReferralRewardDuplicateDB(t *testing.T) {
	e := newRefEnv(t, false)
	_, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	r := e.referral(ref)
	reward := referrals.Reward{ReferralID: ref, Side: referrals.SideReferee, Credits: r.refereeCr,
		To: referrals.Beneficiary{Kind: referrals.BeneficiaryCustomer, ID: referee}, IdempotencyKey: referrals.RewardKey(ref, referrals.SideReferee)}
	first, posted, err := ReferralsLedger(e.cs).PostReward(e.ctx, reward)
	require.NoError(t, err)
	require.True(t, posted)
	// The credits ledger sees the same posting as a duplicate of the first.
	again, err := e.cs.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, RefType: "referral", RefID: ref,
		IdempotencyKey: reward.IdempotencyKey, Memo: "referral reward: referee",
		Legs: []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -int64(r.refereeCr)}, {Account: credits.Account{Kind: credits.CustomerWallet, Owner: referee}, Amount: int64(r.refereeCr)}}})
	require.NoError(t, err)
	require.True(t, again.Duplicate)
	require.Equal(t, first, again.TransactionID)
	before := e.count(`SELECT count(*) FROM ledger_transactions`)

	e.buy(referee)
	e.runJobs()
	r = e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.Equal(t, ids.UUID(first), r.refereeTx, "the referral stores the first transaction")
	require.Equal(t, before+1, e.count(`SELECT count(*) FROM ledger_transactions`), "only the referrer's side is new")
	require.Equal(t, 2, e.rewardTxs(ref))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM credit_lots WHERE customer_id = $1`, ids.UUID(referee)))
}

// A ledger that fails leaves the purchase and the qualification committed
// and the sides unposted with an attempt recorded; the sweep posts them
// once the backoff has passed, once.
func TestReferralRewardFailedPostSweepsDB(t *testing.T) {
	e := newRefEnv(t, true)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	e.flaky.fail = 2
	// The purchase's own transaction, with a write of its own.
	marker := ids.New(refNow)
	require.NoError(t, e.pool.InTx(e.ctx, func(ctx context.Context) error {
		tx, _ := db.TxFrom(ctx)
		if _, err := tx.Exec(ctx, `INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, details, status, created_at)
		  VALUES ($1, 'user', $2, 'test.purchase', 0, '{}', 'open', $3)`, ids.UUID(marker), ids.UUID(referee), refNow); err != nil {
			return err
		}
		store, _ := e.shop()
		return e.svc.OnPurchaseCompleted(ctx, referrals.Outcome{RefType: referrals.RefOrder, RefID: ids.New(refNow),
			CustomerID: referee, StoreID: store, EligiblePaise: 10_000})
	}))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM risk_flags WHERE id = $1`, ids.UUID(marker)), "the purchase committed")
	e.runJobs()
	r := e.referral(ref)
	require.Equal(t, "qualified", r.status)
	require.False(t, r.referrerTx.Valid)
	require.False(t, r.refereeTx.Valid)
	require.Zero(t, e.rewardTxs(ref))
	require.Zero(t, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, 1, e.count(`SELECT attempts FROM referrals WHERE id = $1`, ids.UUID(ref)))

	require.Zero(t, e.sweep(time.Minute), "not due yet")
	require.EqualValues(t, 1, e.sweep(10*time.Minute))
	r = e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.Equal(t, 2, e.rewardTxs(ref))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, r.referrerCr, e.balance(credits.CustomerWallet, referrer))
	require.Zero(t, e.sweep(time.Hour))
	require.Equal(t, 2, e.rewardTxs(ref))
}

// A real SQL error in a side's own transaction, after the ledger posted in
// it: that transaction rolls back, posting and all, the other side posts,
// the attempt is recorded, and the retry posts the side once.
func TestReferralRewardSQLErrorRollsBackTheSideDB(t *testing.T) {
	e := newRefEnv(t, true)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	e.flaky.sqlFail[referrals.SideReferrer] = 1
	e.buy(referee)
	e.runJobs()

	r := e.referral(ref)
	require.Equal(t, "qualified", r.status)
	require.False(t, r.referrerTx.Valid)
	require.True(t, r.refereeTx.Valid, "the other side still posts")
	require.Equal(t, 1, e.rewardTxs(ref), "the failed side's posting rolled back")
	require.Zero(t, e.count(`SELECT count(*) FROM ledger_transactions WHERE idempotency_key = $1`, referrals.RewardKey(ref, referrals.SideReferrer)))
	require.Zero(t, e.balance(credits.CustomerWallet, referrer))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, 1, e.count(`SELECT attempts FROM referrals WHERE id = $1`, ids.UUID(ref)))

	require.EqualValues(t, 1, e.sweep(11*time.Minute))
	r = e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.Equal(t, 2, e.rewardTxs(ref))
	require.Equal(t, r.referrerCr, e.balance(credits.CustomerWallet, referrer))
	require.Equal(t, -(r.refereeCr + r.referrerCr), e.marketing())
}

// D-166: a purchase reversed before either side posted puts the referral
// back to pending; the sweep posts nothing, and the next qualifying
// purchase pays.
func TestReferralReversedBeforePostingDB(t *testing.T) {
	e := newRefEnv(t, true)
	_, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	e.flaky.fail = 2
	o := e.buy(referee)
	e.runJobs()
	require.Equal(t, "qualified", e.referral(ref).status)

	require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, o.RefType, o.RefID))
	require.Equal(t, "pending", e.referral(ref).status)
	require.Zero(t, e.sweep(time.Hour))
	require.Zero(t, e.rewardTxs(ref))
	require.Zero(t, e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reversed_after_reward'`))

	e.buy(referee)
	e.runJobs()
	require.Equal(t, "rewarded", e.referral(ref).status)
	require.Equal(t, 2, e.rewardTxs(ref))
}

// 01 §7: a purchase reversed after both sides posted keeps the rewards and
// raises a risk flag; no money moves (wallets, lots, platform_marketing).
func TestReferralReversedAfterPostingDB(t *testing.T) {
	e := newRefEnv(t, false)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	o := e.buy(referee)
	e.runJobs()
	require.Equal(t, "rewarded", e.referral(ref).status)
	before := e.snap(referrer, referee)

	require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, o.RefType, o.RefID))
	require.Equal(t, "rewarded", e.referral(ref).status)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reversed_after_reward' AND subject_id = $1`, ids.UUID(ref)))
	require.Equal(t, before, e.snap(referrer, referee))
	require.Zero(t, e.sweep(time.Hour))
	require.Equal(t, before, e.snap(referrer, referee))
}

// D-169: a key the ledger already holds for other legs is refused for good.
// The side stays unposted under a referral.reward_refused flag, the sweep
// skips it (no retry loop), and clearing the flag lets the sweep try again.
func TestReferralRewardRefusedIsParkedDB(t *testing.T) {
	e := newRefEnv(t, true)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	// Something already posted this side's key with another amount.
	_, err := e.cs.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, RefType: "referral", RefID: ref,
		IdempotencyKey: referrals.RewardKey(ref, referrals.SideReferee),
		Legs:           []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -1}, {Account: credits.Account{Kind: credits.CustomerWallet, Owner: referee}, Amount: 1}}})
	require.NoError(t, err)
	flags := func(status string) int {
		return e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reward_refused' AND subject_type = 'referral'
		  AND subject_id = $1 AND status = $2`, ids.UUID(ref), status)
	}

	e.buy(referee)
	e.runJobs()
	r := e.referral(ref)
	require.Equal(t, "qualified", r.status)
	require.True(t, r.referrerTx.Valid, "the other side still pays")
	require.False(t, r.refereeTx.Valid)
	require.Equal(t, r.referrerCr, e.balance(credits.CustomerWallet, referrer))
	require.Equal(t, 1, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, 1, flags("open"))

	for range 3 {
		require.Zero(t, e.sweep(time.Hour))
	}
	require.Equal(t, 1, flags("open"), "the sweep does not retry a refused posting")
	require.Equal(t, 2, e.rewardTxs(ref))

	// A person clears the flag without fixing the cause: one more try, one
	// more flag, still no loop.
	e.exec(`UPDATE risk_flags SET status = 'cleared', reviewed_at = $2 WHERE subject_id = $1 AND rule = 'referral.reward_refused'`, ids.UUID(ref), refNow)
	require.Zero(t, e.sweep(time.Minute))
	require.Equal(t, 1, flags("open"))
	require.Equal(t, 1, flags("cleared"))
	require.Zero(t, e.sweep(time.Hour))
	require.Equal(t, 1, flags("open"))
}

// A store beneficiary the ledger refuses (its key already used for other
// legs) is parked the same way; the customer side still pays.
func TestReferralRewardStoreRefusedDB(t *testing.T) {
	e := newRefEnv(t, false)
	store, code := e.shop()
	referee, ref := e.claim(code)
	_, err := e.cs.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, RefType: "referral", RefID: ref,
		IdempotencyKey: referrals.RewardKey(ref, referrals.SideReferrer),
		Legs:           []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -1}, {Account: credits.Account{Kind: credits.StoreCredit, Owner: store}, Amount: 1}}})
	require.NoError(t, err)

	e.buy(referee)
	e.runJobs()
	r := e.referral(ref)
	require.Equal(t, "qualified", r.status)
	require.False(t, r.referrerTx.Valid)
	require.True(t, r.refereeTx.Valid)
	require.Equal(t, 1, e.balance(credits.StoreCredit, store), "only the earlier posting")
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reward_refused' AND subject_id = $1
	  AND details->'sides' ? 'referrer' AND status = 'open'`, ids.UUID(ref)))
	require.Zero(t, e.sweep(time.Hour))
	require.Equal(t, 1, e.balance(credits.StoreCredit, store))
}

// An unknown beneficiary kind is refused, never posted.
func TestReferralsLedgerRefusesUnknownBeneficiary(t *testing.T) {
	_, posted, err := ReferralsLedger(nil).PostReward(context.Background(), referrals.Reward{To: referrals.Beneficiary{Kind: "bank"}, Credits: 1})
	require.ErrorIs(t, err, referrals.ErrRewardRefused)
	require.False(t, posted)
}

// D-169: a referee's first purchase at store S, the referrer's own order at
// S and the referrer's earlier reward posting run at the same time; none
// fails (no 40P01), since a purchase never posts a reward and each posting
// is one lock call in its own transaction.
func TestReferralRewardConcurrentWithReferrersOrderDB(t *testing.T) {
	e := newRefEnv(t, false)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	e.grant(referrer, 1_000)
	// What the referrer's orders spent and earned back as cashback.
	var captured, cashback atomic.Int64
	purchase := func(customer, store ids.ID, spend int) error {
		// credit_holds.order_id is a foreign key to orders (D-211).
		ref := credits.Ref{Type: credits.RefOrder, ID: e.tdb.InsertOrder(e.t, ids.New(refNow), customer, store)}
		if spend > 0 { // checkout
			if err := e.pool.InTx(e.ctx, func(ctx context.Context) error {
				_, err := e.cs.Hold(ctx, credits.HoldRequest{Ref: ref, CustomerID: customer, StoreID: store, Credits: spend, EligiblePaise: 10_000})
				return err
			}); err != nil {
				return err
			}
		}
		// Collected: the credits settle and the referrals hook runs, in one
		// transaction, as orders does.
		return e.pool.InTx(e.ctx, func(ctx context.Context) error {
			done, err := e.cs.Complete(ctx, credits.Completion{Ref: ref, CustomerID: customer, StoreID: store, EligiblePaise: 10_000})
			if err != nil {
				return err
			}
			if customer == referrer {
				captured.Add(int64(done.Captured))
				cashback.Add(int64(done.Cashback))
			}
			return e.svc.OnPurchaseCompleted(ctx, referrals.Outcome{RefType: referrals.RefOrder, RefID: ref.ID,
				CustomerID: customer, StoreID: store, EligiblePaise: 10_000, CreditsUsed: spend})
		})
	}

	const rounds = 20
	var refs []ids.ID
	for i := range rounds {
		store, _ := e.shop()
		// A cashback rate, so the referee's Complete locks its wallet, held
		// and the store's credit (the purchase's one lock call).
		e.exec(`UPDATE stores SET cashback_bp = 500 WHERE id = $1`, ids.UUID(store))
		referee, ref := e.claim(code)
		refs = append(refs, ref)
		fns := []func() error{
			func() error { return purchase(referee, store, 0) },
			func() error { return purchase(referrer, store, 2) },
		}
		if i > 0 {
			prev := refs[i-1]
			fns = append(fns, func() error { _, err := e.svc.PostRewards(e.ctx, prev); return err })
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, len(fns))
		for k, fn := range fns {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[k] = fn()
			}()
		}
		close(start)
		wg.Wait()
		for k, err := range errs {
			require.NoError(t, err, "round %d, call %d", i, k)
		}
	}
	_, err := e.svc.PostRewards(e.ctx, refs[len(refs)-1])
	require.NoError(t, err)
	total := 0
	for _, ref := range refs {
		r := e.referral(ref)
		require.Equal(t, "rewarded", r.status)
		require.Equal(t, 2, e.rewardTxs(ref))
		total += r.referrerCr
	}
	require.Positive(t, captured.Load(), "the referrer's orders spent credits")
	require.Equal(t, 1_000+total-int(captured.Load())+int(cashback.Load()), e.balance(credits.CustomerWallet, referrer))
}

// review: one side posted and the other waiting (a store referrer with no
// live store) when the purchase is reversed. The posted reward stays, so the
// referral stays qualified with one flag (01 §7, D-166), and the waiting
// side is still paid once the store is live.
func TestReferralReversedWithASideWaitingDB(t *testing.T) {
	e := newRefEnv(t, false)
	store, code := e.shop()
	e.exec(`UPDATE stores SET status = 'paused' WHERE id = $1`, ids.UUID(store))
	referee, ref := e.claim(code)
	o := e.buy(referee)
	e.runJobs()
	r := e.referral(ref)
	require.Equal(t, "qualified", r.status)
	require.False(t, r.referrerTx.Valid, "the store side waits")
	require.True(t, r.refereeTx.Valid)

	require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, referrals.RefOrder, o.RefID))
	require.Equal(t, "qualified", e.referral(ref).status)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reversed_after_reward' AND subject_id = $1`, ids.UUID(ref)))

	e.exec(`UPDATE stores SET status = 'live' WHERE id = $1`, ids.UUID(store))
	require.EqualValues(t, 1, e.sweep(time.Hour))
	r = e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.Equal(t, 2, e.rewardTxs(ref))
	require.Equal(t, r.referrerCr, e.balance(credits.StoreCredit, store))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reversed_after_reward' AND subject_id = $1`, ids.UUID(ref)))
}

// review: a side marked done without a posting (a deleted referrer) is not a
// posted reward. Reversed while the other side waits, the qualification is
// revoked, the done mark cleared, and nobody is paid for the reversed sale.
func TestReferralReversedWithAVoidSideDB(t *testing.T) {
	e := newRefEnv(t, true)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	e.exec(`UPDATE users SET status = 'deleted', deleted_at = $2 WHERE id = $1`, ids.UUID(referrer), refNow)
	e.flaky.fail = 1 // the referee's posting (the referrer's is void, no posting)
	o := e.buy(referee)
	e.runJobs()
	require.Equal(t, 1, e.count(`SELECT count(*) FROM referrals WHERE id = $1 AND status = 'qualified'
	  AND referrer_rewarded_at IS NOT NULL AND referrer_transaction_id IS NULL AND referee_rewarded_at IS NULL`, ids.UUID(ref)))

	require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, referrals.RefOrder, o.RefID))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM referrals WHERE id = $1 AND status = 'pending'
	  AND referrer_rewarded_at IS NULL AND referee_rewarded_at IS NULL AND next_attempt_at IS NULL`, ids.UUID(ref)))
	require.Zero(t, e.count(`SELECT count(*) FROM risk_flags WHERE subject_id = $1`, ids.UUID(ref)))
	require.Zero(t, e.sweep(time.Hour))
	require.Zero(t, e.rewardTxs(ref))
	require.Zero(t, e.balance(credits.CustomerWallet, referee))
}

// review: the job and the sweep post the same referral at the same moment.
// Each side posts exactly once.
func TestReferralRewardJobAndSweepRaceDB(t *testing.T) {
	e := newRefEnv(t, false)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	total := 0
	for i := range 10 {
		referee, ref := e.claim(code)
		e.buy(referee)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var runs []platformjobs.FakeRun
		var sweepErr error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; runs = e.jobs.RunDue(e.ctx) }()
		go func() { defer wg.Done(); <-start; _, sweepErr = e.svc.Sweep(e.ctx) }()
		close(start)
		wg.Wait()
		require.NoError(t, sweepErr, "round %d", i)
		for _, r := range runs {
			require.Equal(t, platformjobs.StatusSucceeded, r.Status, "round %d: %v", i, r.Err)
		}
		r := e.referral(ref)
		require.Equal(t, "rewarded", r.status, "round %d", i)
		require.Equal(t, 2, e.rewardTxs(ref), "round %d", i)
		require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
		total += r.referrerCr
	}
	require.Equal(t, total, e.balance(credits.CustomerWallet, referrer))
}

// review: the beneficiary is deleted (wallet forfeited, user marked deleted,
// one transaction) after the side read it and before the ledger locked the
// wallet. The recheck under the lock rolls the posting back and the retry
// voids the side, so no lot lands in a forfeited wallet.
func TestReferralRewardBeneficiaryDeletedWhilePostingDB(t *testing.T) {
	e := newRefEnv(t, true)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	e.grant(referee, 5)
	e.flaky.before[referrals.SideReferee] = func() {
		require.NoError(t, e.pool.InTx(e.ctx, func(ctx context.Context) error {
			if err := e.cs.OnAccountDelete(ctx, referee); err != nil {
				return err
			}
			tx, _ := db.TxFrom(ctx)
			_, err := tx.Exec(ctx, `UPDATE users SET status = 'deleted', deleted_at = $2 WHERE id = $1`, ids.UUID(referee), refNow)
			return err
		}))
	}
	e.buy(referee)
	e.runJobs()

	r := e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.True(t, r.referrerTx.Valid)
	require.False(t, r.refereeTx.Valid, "the deleted referee's side is void")
	require.Zero(t, e.count(`SELECT count(*) FROM ledger_transactions WHERE idempotency_key = $1`, referrals.RewardKey(ref, referrals.SideReferee)))
	require.Zero(t, e.balance(credits.CustomerWallet, referee))
	require.Zero(t, e.count(`SELECT coalesce(sum(remaining), 0)::int FROM credit_lots WHERE customer_id = $1`, ids.UUID(referee)))
	require.Equal(t, r.referrerCr, e.balance(credits.CustomerWallet, referrer))
}

// review: a referral whose pass errors (here its refused flag cannot be
// written) does not stop the sweep. The others post, the error comes back
// at the end, and the failing referral backs off instead of heading every
// batch.
func TestRewardSweepGoesOnAfterOneFailsDB(t *testing.T) {
	e := newRefEnv(t, false)
	_, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	bad, badRef := e.claim(code)
	_, err := e.cs.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, RefType: "referral", RefID: badRef,
		IdempotencyKey: referrals.RewardKey(badRef, referrals.SideReferee),
		Legs:           []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -1}, {Account: credits.Account{Kind: credits.CustomerWallet, Owner: bad}, Amount: 1}}})
	require.NoError(t, err)
	e.exec(`CREATE FUNCTION tz_test_no_flag() RETURNS trigger LANGUAGE plpgsql AS $$
	  BEGIN IF NEW.subject_id = '` + ids.UUID(badRef).String() + `'::uuid THEN RAISE EXCEPTION 'test: no flag'; END IF; RETURN NEW; END $$`)
	e.exec(`CREATE TRIGGER tz_test_no_flag BEFORE INSERT ON risk_flags FOR EACH ROW EXECUTE FUNCTION tz_test_no_flag()`)
	e.buy(bad)
	good, goodRef := e.claim(code)
	e.buy(good)

	e.clk.Advance(time.Minute)
	out, err := e.svc.Sweep(e.ctx)
	require.Error(t, err)
	require.EqualValues(t, 1, out["rewarded"])
	require.EqualValues(t, 1, out["waiting"])
	require.Equal(t, "rewarded", e.referral(goodRef).status)
	require.Equal(t, "qualified", e.referral(badRef).status)
	require.Equal(t, 1, e.count(`SELECT attempts FROM referrals WHERE id = $1`, ids.UUID(badRef)))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM referrals WHERE id = $1 AND next_attempt_at > $2`, ids.UUID(badRef), e.clk.Now()))

	e.clk.Advance(time.Minute)
	out, err = e.svc.Sweep(e.ctx)
	require.NoError(t, err, "not due again yet")
	require.Zero(t, out["rewarded"]+out["waiting"])
}

// review: one referral.reward_refused flag per referral. A retried job adds
// nothing; a second refused side is added to the same flag.
func TestReferralRewardRefusedFlagOnceDB(t *testing.T) {
	e := newRefEnv(t, false)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	for side, owner := range map[string]ids.ID{referrals.SideReferrer: referrer, referrals.SideReferee: referee} {
		_, err := e.cs.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, RefType: "referral", RefID: ref,
			IdempotencyKey: referrals.RewardKey(ref, side),
			Legs:           []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -1}, {Account: credits.Account{Kind: credits.CustomerWallet, Owner: owner}, Amount: 1}}})
		require.NoError(t, err)
	}
	e.buy(referee)
	e.runJobs()
	for range 2 { // the job retried, and run by hand
		_, err := e.jobs.RunEvent(e.ctx, referrals.KindReward, referrals.RewardArgs{ReferralID: ref})
		require.NoError(t, err)
	}
	require.Equal(t, 1, e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reward_refused' AND subject_id = $1`, ids.UUID(ref)))
	var sides string
	e.scan(`SELECT details->>'sides' FROM risk_flags WHERE rule = 'referral.reward_refused' AND subject_id = $1`, []any{ids.UUID(ref)}, &sides)
	require.JSONEq(t, `["referrer", "referee"]`, sides)
	require.Equal(t, "qualified", e.referral(ref).status)
}
