package jobs

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
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
	pool  *db.DB
	flaky *flakyPoster
	codes int
}

// flakyPoster fails the next fail postings, as a ledger that is down.
type flakyPoster struct {
	p    credits.Poster
	fail int
}

func (f *flakyPoster) Post(ctx context.Context, p credits.Posting) (credits.Posted, error) {
	if f.fail > 0 {
		f.fail--
		return credits.Posted{}, errors.New("ledger down")
	}
	return f.p.Post(ctx, p)
}

// newRefEnv builds the service with ReferralsService, or, flaky, with the
// same ledger behind a poster that can fail.
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
	if !flaky {
		e.svc, err = ReferralsService(d, nil)
		require.NoError(t, err)
		return e
	}
	cs, err := CreditsService(d, nil)
	require.NoError(t, err)
	h, err := pii.NewHasher(make([]byte, pii.KeySize))
	require.NoError(t, err)
	e.flaky = &flakyPoster{p: cs}
	e.svc, err = referrals.New(referrals.Deps{DB: e.pool, Clock: e.clk, Settings: e.cache, Hasher: h, Ledger: ReferralsLedger(e.flaky),
		PublicURL: "https://api.todayz.localhost", CustomerApp: "https://app.todayz.localhost", BusinessApp: "https://business.todayz.localhost"})
	require.NoError(t, err)
	return e
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

func (e *refEnv) sweep() int64 {
	e.t.Helper()
	out, err := e.svc.Sweep(e.ctx)
	require.NoError(e.t, err)
	return out["rewarded"]
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

// A qualifying purchase posts both sides once, to the referee's wallet (with
// a lot) and the referrer's wallet; a retry and a re-sweep post nothing new.
func TestReferralRewardsPostToCreditsDB(t *testing.T) {
	e := newRefEnv(t, false)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	o := e.buy(referee)

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
	require.Equal(t, 2, e.count(`SELECT count(*) FROM ledger_transactions WHERE id IN ($1, $2) AND idempotency_key LIKE 'referral:%'`, r.referrerTx, r.refereeTx))

	// The same purchase again, and the sweep, post nothing.
	require.NoError(t, e.svc.OnPurchaseCompleted(e.ctx, o))
	require.Zero(t, e.sweep())
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

	r := e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.Equal(t, r.referrerCr, e.balance(credits.StoreCredit, store))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Zero(t, e.count(`SELECT count(*) FROM credit_lots WHERE source_transaction_id = $1`, r.referrerTx))
}

// A ledger that fails leaves the purchase and the qualification committed
// and the sides unposted; the next sweep posts them, once.
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
	r := e.referral(ref)
	require.Equal(t, "qualified", r.status)
	require.False(t, r.referrerTx.Valid)
	require.False(t, r.refereeTx.Valid)
	require.Zero(t, e.rewardTxs(ref))
	require.Zero(t, e.balance(credits.CustomerWallet, referee))

	require.EqualValues(t, 1, e.sweep())
	r = e.referral(ref)
	require.Equal(t, "rewarded", r.status)
	require.Equal(t, 2, e.rewardTxs(ref))
	require.Equal(t, r.refereeCr, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, r.referrerCr, e.balance(credits.CustomerWallet, referrer))
	require.Zero(t, e.sweep())
	require.Equal(t, 2, e.rewardTxs(ref))
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
	require.Equal(t, "qualified", e.referral(ref).status)

	require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, o.RefType, o.RefID))
	require.Equal(t, "pending", e.referral(ref).status)
	require.Zero(t, e.sweep())
	require.Zero(t, e.rewardTxs(ref))
	require.Zero(t, e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reversed_after_reward'`))

	e.buy(referee)
	require.Equal(t, "rewarded", e.referral(ref).status)
	require.Equal(t, 2, e.rewardTxs(ref))
}

// D-169: a key the ledger already holds for other legs is refused for good.
// The side stays unposted under a referral.reward_refused flag, the sweep
// skips it (no retry loop), and clearing the flag lets the sweep try again.
func TestReferralRewardRefusedIsParkedDB(t *testing.T) {
	e := newRefEnv(t, true)
	referrer, code := e.customer(refNow.Add(-30 * 24 * time.Hour))
	referee, ref := e.claim(code)
	// Something already posted this side's key with another amount.
	cs, err := CreditsService(Deps{Pool: e.tdb.Pool, Clock: e.clk, Settings: e.cache}, nil)
	require.NoError(t, err)
	_, err = cs.Post(e.ctx, credits.Posting{Kind: credits.TxReferralReward, RefType: "referral", RefID: ref,
		IdempotencyKey: "referral:" + ref.String() + ":" + referrals.SideReferee,
		Legs:           []credits.Leg{{Account: credits.Account{Kind: credits.PlatformMarketing}, Amount: -1}, {Account: credits.Account{Kind: credits.CustomerWallet, Owner: referee}, Amount: 1}}})
	require.NoError(t, err)
	flags := func(status string) int {
		return e.count(`SELECT count(*) FROM risk_flags WHERE rule = 'referral.reward_refused' AND subject_type = 'referral'
		  AND subject_id = $1 AND status = $2`, ids.UUID(ref), status)
	}

	e.buy(referee)
	r := e.referral(ref)
	require.Equal(t, "qualified", r.status)
	require.True(t, r.referrerTx.Valid, "the other side still pays")
	require.False(t, r.refereeTx.Valid)
	require.Equal(t, r.referrerCr, e.balance(credits.CustomerWallet, referrer))
	require.Equal(t, 1, e.balance(credits.CustomerWallet, referee))
	require.Equal(t, 1, flags("open"))

	for range 3 {
		require.Zero(t, e.sweep())
	}
	require.Equal(t, 1, flags("open"), "the sweep does not retry a refused posting")
	require.Equal(t, 2, e.rewardTxs(ref))

	// A person clears the flag without fixing the cause: one more try, one
	// more flag, still no loop.
	e.exec(`UPDATE risk_flags SET status = 'cleared', reviewed_at = $2 WHERE subject_id = $1 AND rule = 'referral.reward_refused'`, ids.UUID(ref), refNow)
	require.Zero(t, e.sweep())
	require.Equal(t, 1, flags("open"))
	require.Equal(t, 1, flags("cleared"))
	require.Zero(t, e.sweep())
	require.Equal(t, 1, flags("open"))
}
