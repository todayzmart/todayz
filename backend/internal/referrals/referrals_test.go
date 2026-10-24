package referrals

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

// Referrals (08 SC-RF-01…20; 01 §8; D-07, D-08, D-13, D-26, D-32, D-72,
// D-160…D-169).

func TestMain(m *testing.M) { testkit.Main(m) }

var testNow = time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)

// fakeLedger stands in for W2-01's credits.Poster.
type fakeLedger struct {
	mu     sync.Mutex
	down   bool            // posted false
	err    error           // an error from the ledger
	fail   map[ids.ID]bool // referrals whose postings error
	posted []Reward
	keys   map[string]ids.ID
}

func (l *fakeLedger) PostReward(_ context.Context, r Reward) (ids.ID, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return ids.Zero, false, l.err
	}
	if l.fail[r.ReferralID] {
		return ids.Zero, false, errors.New("ledger: this posting fails")
	}
	if l.down {
		return ids.Zero, false, nil
	}
	if l.keys == nil {
		l.keys = map[string]ids.ID{}
	}
	if id, ok := l.keys[r.IdempotencyKey]; ok {
		return id, true, nil
	}
	id := ids.New(testNow)
	l.keys[r.IdempotencyKey] = id
	l.posted = append(l.posted, r)
	return id, true, nil
}

// fakeNotices records the notifications sent.
type fakeNotices struct {
	mu   sync.Mutex
	sent []notice.Notification
}

func (f *fakeNotices) Send(_ context.Context, _ pgx.Tx, n notice.Notification) (ids.ID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	return ids.New(testNow), nil
}

// fakeJobs stands in for the queue: it records referral_reward enqueues,
// unique per key while pending as River's are, and run posts them as the
// job does (internal/jobs registers the real kind).
type fakeJobs struct {
	mu       sync.Mutex
	pending  []RewardArgs
	keys     map[string]bool
	enqueues int
}

func (f *fakeJobs) EnqueueTx(_ context.Context, tx pgx.Tx, kind string, args any, o platformjobs.EnqueueOpts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if tx == nil || kind != KindReward || o.UniqueKey == "" {
		return errors.New("fakeJobs: unexpected enqueue")
	}
	f.enqueues++
	if f.keys == nil {
		f.keys = map[string]bool{}
	}
	if f.keys[o.UniqueKey] {
		return nil
	}
	f.keys[o.UniqueKey] = true
	f.pending = append(f.pending, args.(RewardArgs))
	return nil
}

func (f *fakeJobs) take() []RewardArgs {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pending
	f.pending, f.keys = nil, nil
	return p
}

type env struct {
	t       *testing.T
	tdb     *testkit.DB
	svc     *Service
	cache   *settings.Cache
	clk     *clock.Fake
	ledger  *fakeLedger
	notices *fakeNotices
	jobs    *fakeJobs
	ctx     context.Context
	codes   int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	e := &env{t: t, tdb: tdb, ctx: context.Background(), clk: clock.NewFake(testNow), ledger: &fakeLedger{}, notices: &fakeNotices{}, jobs: &fakeJobs{}}
	e.cache = settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{})
	h, err := pii.NewHasher(make([]byte, pii.KeySize))
	require.NoError(t, err)
	e.svc, err = New(Deps{
		DB: db.New(tdb.Pool), Clock: e.clk, Settings: e.cache, Hasher: h, Ledger: e.ledger, Notices: e.notices, Jobs: e.jobs,
		PublicURL: "https://api.todayz.localhost", CustomerApp: "https://app.todayz.localhost", BusinessApp: "https://business.todayz.localhost",
	})
	require.NoError(t, err)
	e.exec(`ALTER TABLE orders DISABLE TRIGGER trg_orders_guard`)
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err, sql)
}

func (e *env) setting(key, value string) {
	e.t.Helper()
	e.exec(`UPDATE platform_settings SET value = $2::jsonb WHERE key = $1`, key, value)
	e.cache.Invalidate()
}

// code is a fresh 6-character code.
func (e *env) code() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	e.codes++
	n, b := e.codes, []byte("T00000")
	for i := 5; i >= 1; i-- {
		b[i] = alphabet[n%len(alphabet)]
		n /= len(alphabet)
	}
	return string(b)
}

// customer is a customer who signed up at joined, with a code.
func (e *env) customer(joined time.Time) (ids.ID, string) {
	id, code := ids.New(joined), e.code()
	e.exec(`INSERT INTO users (id, created_at) VALUES ($1, $2)`, ids.UUID(id), joined)
	e.exec(`INSERT INTO customer_profiles (user_id, referral_code, created_at) VALUES ($1, $2, $3)`, ids.UUID(id), code, joined)
	return id, code
}

func (e *env) newCustomer() (ids.ID, string) { return e.customer(e.clk.Now().Add(-time.Hour)) }

func (e *env) phone(user ids.ID, number string) {
	h := sha256.Sum256([]byte(number))
	e.exec(`INSERT INTO phone_identities (id, user_id, phone_enc, phone_hash, verified_at) VALUES ($1, $2, '\x00', $3, $4)`,
		ids.UUID(ids.New(testNow)), ids.UUID(user), h[:], testNow)
}

// session is a session family for user on the device with this key ("" for none).
func (e *env) session(user ids.ID, deviceKey string, seen time.Time) ids.ID {
	family := ids.New(testNow)
	var dev any
	if deviceKey != "" {
		d := ids.New(testNow)
		e.exec(`INSERT INTO devices (id, user_id, device_key, last_seen_at) VALUES ($1, $2, $3, $4)`, ids.UUID(d), ids.UUID(user), deviceKey, seen)
		dev = ids.UUID(d)
	}
	h := sha256.Sum256([]byte(family.String()))
	e.exec(`INSERT INTO sessions (id, user_id, app, family_id, refresh_hash, expires_at, last_used_at, created_at, device_id)
	  VALUES ($1, $2, 'customer', $3, $4, $5, $6, $6, $7)`,
		ids.UUID(ids.New(testNow)), ids.UUID(user), ids.UUID(family), h[:], testNow.Add(24*time.Hour), testNow, dev)
	return family
}

func (e *env) business(status string, created time.Time) (ids.ID, string) {
	id, code := ids.New(created), e.code()
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code, status, created_at) VALUES ($1, 'Shop', $2, $3, $4)`,
		ids.UUID(id), code, status, created)
	return id, code
}

func (e *env) staff(biz, user ids.ID, role string) {
	e.exec(`INSERT INTO staff (id, business_id, user_id, role, status) VALUES ($1, $2, $3, $4, 'active')`,
		ids.UUID(ids.New(testNow)), ids.UUID(biz), ids.UUID(user), role)
}

// store is a live store of biz; its short code is its referral code.
func (e *env) store(biz ids.ID, status string, live time.Time) (ids.ID, string, string) {
	id, code := ids.New(live), e.code()
	slug := "shop-" + strings.ToLower(code)
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, status, live_at) VALUES ($1, $2, $3, $4, 'Shop', $5, $6)`,
		ids.UUID(id), ids.UUID(biz), slug, code, status, live)
	return id, code, slug
}

// owner is a new business with an owner (who has a phone and a session).
func (e *env) owner(status string) (biz, owner, session ids.ID, code string) {
	biz, code = e.business(status, e.clk.Now().Add(-time.Hour))
	owner, _ = e.newCustomer()
	e.staff(biz, owner, "owner")
	e.phone(owner, "owner-"+code)
	return biz, owner, e.session(owner, ids.New(testNow).String(), testNow), code
}

// shopWithStore is a verified business with a live store, as a referrer.
func (e *env) shopWithStore() (biz, store ids.ID, bizCode, storeCode, slug string) {
	biz, bizCode = e.business("verified", testNow.Add(-30*24*time.Hour))
	store, storeCode, slug = e.store(biz, "live", testNow.Add(-29*24*time.Hour))
	return
}

func (e *env) referral(id ids.ID) (status string, rewardedA, rewardedB bool) {
	e.t.Helper()
	var a, b *time.Time
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx,
		`SELECT status, referrer_rewarded_at, referee_rewarded_at FROM referrals WHERE id = $1`, ids.UUID(id)).Scan(&status, &a, &b))
	return status, a != nil, b != nil
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, sql, args...).Scan(&n))
	return n
}

func (e *env) flags(rule string) int {
	return e.count(`SELECT count(*) FROM risk_flags WHERE rule = $1`, rule)
}

func (e *env) claim(user ids.ID, in ClaimInput) (ClaimResult, error) {
	return e.svc.ClaimCustomer(e.ctx, user, e.session(user, "", testNow), in)
}

func (e *env) order(customer, store ids.ID, paise int64, credits int) Outcome {
	return Outcome{RefType: RefOrder, RefID: ids.New(testNow), CustomerID: customer, StoreID: store, EligiblePaise: paise, CreditsUsed: credits}
}

// complete records a completed purchase and then runs the reward jobs it
// enqueued, as the worker does once the purchase has committed.
func (e *env) complete(o Outcome) {
	e.t.Helper()
	require.NoError(e.t, e.svc.OnPurchaseCompleted(e.ctx, o))
	e.runJobs()
}

// runJobs runs the pending referral_reward jobs.
func (e *env) runJobs() {
	e.t.Helper()
	for _, a := range e.jobs.take() {
		_, err := e.svc.PostRewards(e.ctx, a.ReferralID)
		require.NoError(e.t, err)
	}
}

// sweep runs the sweep at the clock's time plus d.
func (e *env) sweep(d time.Duration) map[string]int64 {
	e.t.Helper()
	e.clk.Advance(d)
	out, err := e.svc.Sweep(e.ctx)
	require.NoError(e.t, err)
	return out
}

func requireProblem(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var p *problem.Error
	require.ErrorAs(t, err, &p)
	require.Equal(t, code, p.Code, err.Error())
}

// --- the three kinds -------------------------------------------------------------

// SC-RF-01/02/03/04: each kind claims, qualifies and pays both sides to the
// right account, with the amounts of the claim day.
func TestKinds(t *testing.T) {
	cases := []struct {
		name         string
		kind         string
		referrerType string // customer, store, business
		refereeType  string // customer, business
	}{
		{"customer refers customer", KindCustomerCustomer, partyCustomer, partyCustomer},
		{"store refers customer", KindRetailerCustomer, partyStore, partyCustomer},
		{"customer refers shop", KindAnyShop, partyCustomer, partyBusiness},
		{"business refers shop", KindAnyShop, partyBusiness, partyBusiness},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			// The referrer and where its reward goes.
			var code string
			var want Beneficiary
			switch c.referrerType {
			case partyCustomer:
				var id ids.ID
				id, code = e.customer(testNow.Add(-30 * 24 * time.Hour))
				want = Beneficiary{Kind: BeneficiaryCustomer, ID: id}
			case partyStore:
				biz, _, _, _, _ := e.shopWithStore()
				// A second, newer live store: the primary is the oldest (D-72).
				_, code, _ = e.store(biz, "live", testNow.Add(-time.Hour))
				var oldest pgtype.UUID
				require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT id FROM stores WHERE business_id = $1 ORDER BY live_at LIMIT 1`, ids.UUID(biz)).Scan(&oldest))
				want = Beneficiary{Kind: BeneficiaryStore, ID: mustID(t, oldest)}
			case partyBusiness:
				var st ids.ID
				_, st, code, _, _ = e.shopWithStore()
				want = Beneficiary{Kind: BeneficiaryStore, ID: st}
			}

			var res ClaimResult
			var err error
			var refereeTo Beneficiary
			var newStore ids.ID
			switch c.refereeType {
			case partyCustomer:
				u, _ := e.newCustomer()
				res, err = e.claim(u, ClaimInput{Code: strings.ToLower(code)})
				require.NoError(t, err)
				refereeTo = Beneficiary{Kind: BeneficiaryCustomer, ID: u}
				assert.Equal(t, statusPending, res.Status)
				// A big enough first order qualifies and pays (RF-05).
				_, buyStore, _, _, _ := e.shopWithStore()
				e.complete(e.order(u, buyStore, 10_000, 0))
			case partyBusiness:
				e.setting("ref_shop_qualifying_orders", "2")
				biz, owner, sess, _ := e.owner("draft")
				res, err = e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: code})
				require.NoError(t, err)
				assert.Equal(t, statusPending, res.Status)
				newStore, _, _ = e.store(biz, "live", testNow)
				refereeTo = Beneficiary{Kind: BeneficiaryStore, ID: newStore}
				e.exec(`UPDATE businesses SET status = 'verified' WHERE id = $1`, ids.UUID(biz))
				for range 2 {
					buyer, _ := e.newCustomer()
					e.complete(e.order(buyer, newStore, 500, 0)) // any amount counts for a shop
				}
			}
			assert.True(t, res.Created)
			assert.Equal(t, c.kind, res.Kind)
			assert.Equal(t, 100, res.RewardCredits)

			status, a, b := e.referral(res.ReferralID)
			assert.Equal(t, statusRewarded, status)
			assert.True(t, a)
			assert.True(t, b)
			require.Len(t, e.ledger.posted, 2)
			got := map[string]Reward{}
			for _, r := range e.ledger.posted {
				got[r.Side] = r
				assert.Equal(t, "referral:"+res.ReferralID.String()+":"+r.Side, r.IdempotencyKey)
			}
			assert.Equal(t, want, got[SideReferrer].To)
			assert.Equal(t, 30, got[SideReferrer].Credits)
			assert.Equal(t, refereeTo, got[SideReferee].To)
			assert.Equal(t, 100, got[SideReferee].Credits)
			assert.Len(t, e.notices.sent, 2, "both sides are told")
		})
	}
}

// RF-10: the reward amounts are those of the claim day.
func TestRewardSnapshot(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: code})
	require.NoError(t, err)
	e.setting("ref_customer_customer_referee_credits", "5")
	e.setting("ref_customer_customer_referrer_credits", "5")
	_, st, _, _, _ := e.shopWithStore()
	e.complete(e.order(u, st, 20_000, 0))
	status, _, _ := e.referral(res.ReferralID)
	require.Equal(t, statusRewarded, status)
	for _, r := range e.ledger.posted {
		assert.NotEqual(t, 5, r.Credits)
	}
}

// --- the claim's rules -----------------------------------------------------------

func TestClaimRefusals(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	biz, store, bizCode, storeCode, _ := e.shopWithStore()

	t.Run("own code is self", func(t *testing.T) {
		u, own := e.newCustomer()
		_, err := e.claim(u, ClaimInput{Code: own})
		requireProblem(t, err, genproblems.ReferralSelf)
	})
	t.Run("a store of a business you work for is self", func(t *testing.T) {
		u, _ := e.newCustomer()
		e.staff(biz, u, "cashier")
		_, err := e.claim(u, ClaimInput{Code: storeCode})
		requireProblem(t, err, genproblems.ReferralSelf)
	})
	t.Run("a business code is not a customer's to claim", func(t *testing.T) {
		u, _ := e.newCustomer()
		_, err := e.claim(u, ClaimInput{Code: bizCode})
		requireProblem(t, err, genproblems.ReferralNotEligible)
	})
	t.Run("unknown code", func(t *testing.T) {
		u, _ := e.newCustomer()
		_, err := e.claim(u, ClaimInput{Code: "ZZZZZZ"})
		requireProblem(t, err, genproblems.ReferralCodeUnknown)
	})
	t.Run("malformed code", func(t *testing.T) {
		u, _ := e.newCustomer()
		_, err := e.claim(u, ClaimInput{Code: "O0I1L"})
		requireProblem(t, err, genproblems.ValidationFailed)
	})
	t.Run("no code and no click", func(t *testing.T) {
		u, _ := e.newCustomer()
		_, err := e.claim(u, ClaimInput{})
		requireProblem(t, err, genproblems.ValidationFailed)
	})
	t.Run("outside the window of sign-up", func(t *testing.T) {
		u, _ := e.customer(testNow.Add(-8 * 24 * time.Hour))
		_, err := e.claim(u, ClaimInput{Code: code})
		requireProblem(t, err, genproblems.ReferralNotEligible)
	})
	t.Run("after the first completed order", func(t *testing.T) {
		u, _ := e.newCustomer()
		e.exec(`INSERT INTO orders (id, customer_id, store_id, fulfilment, status, subtotal_paise, items_after_offers_paise,
		  item_subtotal_paise, total_paise, code_enc, code_hmac, placed_at)
		  VALUES ($1, $2, $3, 'pickup', 'collected', 1000, 1000, 1000, 1000, '\x00', sha256($4::text::bytea), $5)`,
			ids.UUID(ids.New(testNow)), ids.UUID(u), ids.UUID(store), u.String(), testNow)
		_, err := e.claim(u, ClaimInput{Code: code})
		requireProblem(t, err, genproblems.ReferralNotEligible)
	})
	t.Run("a blocked account", func(t *testing.T) {
		u, _ := e.newCustomer()
		e.exec(`UPDATE users SET status = 'blocked' WHERE id = $1`, ids.UUID(u))
		_, err := e.claim(u, ClaimInput{Code: code})
		requireProblem(t, err, genproblems.ReferralNotEligible)
	})
	t.Run("once per account; the same claim again is 200", func(t *testing.T) {
		u, _ := e.newCustomer()
		first, err := e.claim(u, ClaimInput{Code: code})
		require.NoError(t, err)
		again, err := e.claim(u, ClaimInput{Code: code})
		require.NoError(t, err)
		assert.False(t, again.Created)
		assert.Equal(t, first.ReferralID, again.ReferralID)
		_, err = e.claim(u, ClaimInput{Code: storeCode})
		requireProblem(t, err, genproblems.ReferralAlreadyClaimed)
	})
	t.Run("a phone that was already referred", func(t *testing.T) {
		gone, _ := e.newCustomer()
		e.phone(gone, "+919800000001")
		_, err := e.claim(gone, ClaimInput{Code: code})
		require.NoError(t, err)
		// The first account is deleted; the phone signs up again.
		e.exec(`DELETE FROM phone_identities WHERE user_id = $1`, ids.UUID(gone))
		back, _ := e.newCustomer()
		e.phone(back, "+919800000001")
		_, err = e.claim(back, ClaimInput{Code: code})
		requireProblem(t, err, genproblems.ReferralAlreadyClaimed)
	})
	t.Run("As restricts the code's type", func(t *testing.T) {
		u, _ := e.newCustomer()
		_, err := e.claim(u, ClaimInput{Code: code, As: partyStore})
		requireProblem(t, err, genproblems.ReferralCodeUnknown)
		res, err := e.claim(u, ClaimInput{Code: storeCode, As: partyStore})
		require.NoError(t, err)
		assert.Equal(t, KindRetailerCustomer, res.Kind)
	})
}

func TestBusinessClaimRefusals(t *testing.T) {
	e := newEnv(t)
	_, _, bizCode, storeCode, _ := e.shopWithStore()

	t.Run("own code is self", func(t *testing.T) {
		biz, owner, sess, own := e.owner("draft")
		_, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: own})
		requireProblem(t, err, genproblems.ReferralSelf)
	})
	t.Run("a business sharing staff is self", func(t *testing.T) {
		biz, owner, sess, _ := e.owner("draft")
		other, otherCode := e.business("verified", testNow.Add(-30*24*time.Hour))
		e.staff(other, owner, "manager")
		_, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: otherCode})
		requireProblem(t, err, genproblems.ReferralSelf)
	})
	t.Run("the owner's own customer code is self", func(t *testing.T) {
		biz, owner, sess, _ := e.owner("draft")
		var ownCode string
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT referral_code FROM customer_profiles WHERE user_id = $1`, ids.UUID(owner)).Scan(&ownCode))
		_, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: ownCode})
		requireProblem(t, err, genproblems.ReferralSelf)
	})
	t.Run("a staff member's customer code is self", func(t *testing.T) {
		biz, owner, sess, _ := e.owner("draft")
		cashier, cashierCode := e.newCustomer()
		e.staff(biz, cashier, "cashier")
		_, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: cashierCode})
		requireProblem(t, err, genproblems.ReferralSelf)
	})
	t.Run("a store code is not a business's to claim", func(t *testing.T) {
		biz, owner, sess, _ := e.owner("draft")
		_, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: storeCode})
		requireProblem(t, err, genproblems.ReferralNotEligible)
	})
	t.Run("only before verification", func(t *testing.T) {
		biz, owner, sess, _ := e.owner("verified")
		_, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: bizCode})
		requireProblem(t, err, genproblems.ReferralNotEligible)
	})
	t.Run("submitted is still claimable", func(t *testing.T) {
		biz, owner, sess, _ := e.owner("submitted")
		res, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: bizCode})
		require.NoError(t, err)
		assert.Equal(t, KindAnyShop, res.Kind)
	})
	t.Run("the owner's phone counts once per business referral", func(t *testing.T) {
		biz, owner, sess, _ := e.owner("draft")
		_, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: bizCode})
		require.NoError(t, err)
		biz2, _ := e.business("draft", testNow.Add(-time.Hour))
		e.staff(biz2, owner, "owner")
		_, err = e.svc.ClaimBusiness(e.ctx, biz2, owner, sess, ClaimInput{Code: bizCode})
		requireProblem(t, err, genproblems.ReferralAlreadyClaimed)
	})
	t.Run("an unknown business is 404", func(t *testing.T) {
		_, owner, sess, _ := e.owner("draft")
		_, err := e.svc.ClaimBusiness(e.ctx, ids.New(testNow), owner, sess, ClaimInput{Code: bizCode})
		requireProblem(t, err, genproblems.NotFound)
	})
}

// --- the same-device rule (D-13) -------------------------------------------------

func TestSameDevice(t *testing.T) {
	e := newEnv(t)
	referrer, code := e.customer(testNow.Add(-30 * 24 * time.Hour))

	t.Run("the referrer's device", func(t *testing.T) {
		key := ids.New(testNow).String()
		e.session(referrer, key, testNow.Add(-200*24*time.Hour)) // at any time
		u, _ := e.newCustomer()
		res, err := e.svc.ClaimCustomer(e.ctx, u, e.session(u, key, testNow), ClaimInput{Code: code})
		require.NoError(t, err)
		assert.Equal(t, statusRejected, res.Status, "shown as rejected, the reason hidden")
		assert.Zero(t, res.RewardCredits)
		var st, reason string
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT status, reject_reason FROM referrals WHERE id = $1`, ids.UUID(res.ReferralID)).Scan(&st, &reason))
		assert.Equal(t, statusRejectedSameDevice, st)
		assert.Equal(t, "same_device_referrer", reason)
		// It never qualifies.
		_, store, _, _, _ := e.shopWithStore()
		e.complete(e.order(u, store, 50_000, 0))
		st, _, _ = e.referral(res.ReferralID)
		assert.Equal(t, statusRejectedSameDevice, st)
	})
	t.Run("another account on the device in the last 90 days", func(t *testing.T) {
		key := ids.New(testNow).String()
		other, _ := e.newCustomer()
		e.session(other, key, testNow.Add(-89*24*time.Hour))
		u, _ := e.newCustomer()
		res, err := e.svc.ClaimCustomer(e.ctx, u, e.session(u, key, testNow), ClaimInput{Code: code})
		require.NoError(t, err)
		assert.Equal(t, statusRejected, res.Status)
	})
	t.Run("an account older than 90 days does not count", func(t *testing.T) {
		key := ids.New(testNow).String()
		other, _ := e.newCustomer()
		e.session(other, key, testNow.Add(-91*24*time.Hour))
		u, _ := e.newCustomer()
		res, err := e.svc.ClaimCustomer(e.ctx, u, e.session(u, key, testNow), ClaimInput{Code: code})
		require.NoError(t, err)
		assert.Equal(t, statusPending, res.Status)
	})
	t.Run("a store's staff device", func(t *testing.T) {
		biz, _, _, storeCode, _ := e.shopWithStore()
		cashier, _ := e.newCustomer()
		e.staff(biz, cashier, "cashier")
		key := ids.New(testNow).String()
		e.session(cashier, key, testNow.Add(-24*time.Hour))
		u, _ := e.newCustomer()
		res, err := e.svc.ClaimCustomer(e.ctx, u, e.session(u, key, testNow), ClaimInput{Code: storeCode})
		require.NoError(t, err)
		assert.Equal(t, statusRejected, res.Status)
	})
	t.Run("a device carrying 3 accounts raises a risk flag", func(t *testing.T) {
		key := ids.New(testNow).String()
		before := e.flags("referral.device_accounts")
		a, _ := e.newCustomer()
		e.session(a, key, testNow.Add(-100*24*time.Hour))
		b, _ := e.newCustomer()
		e.session(b, key, testNow.Add(-100*24*time.Hour))
		u, _ := e.newCustomer()
		res, err := e.svc.ClaimCustomer(e.ctx, u, e.session(u, key, testNow), ClaimInput{Code: code})
		require.NoError(t, err)
		assert.Equal(t, statusPending, res.Status, "old accounts: not rejected, only flagged")
		assert.Equal(t, before+1, e.flags("referral.device_accounts"))
	})
}

// --- attribution (D-32) ----------------------------------------------------------

func cookieValue(t *testing.T, setCookie string) string {
	t.Helper()
	c, err := http.ParseSetCookie(setCookie)
	require.NoError(t, err)
	require.Equal(t, CookieName, c.Name)
	assert.Equal(t, "/v1", c.Path)
	assert.True(t, c.Secure)
	assert.True(t, c.HttpOnly)
	assert.Positive(t, c.MaxAge)
	return c.Value
}

func TestOpen(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	biz, store, bizCode, storeCode, slug := e.shopWithStore()
	_ = biz

	t.Run("an unknown code records nothing", func(t *testing.T) {
		l, err := e.svc.Open(e.ctx, Click{Code: "ZZZZZZ"})
		require.NoError(t, err)
		assert.Equal(t, "https://app.todayz.localhost/", l.Location)
		assert.Empty(t, l.SetCookie)
		assert.Zero(t, e.count(`SELECT count(*) FROM referral_clicks WHERE code = 'ZZZZZZ'`))
	})
	t.Run("a customer link", func(t *testing.T) {
		l, err := e.svc.Open(e.ctx, Click{Code: code, Kind: "c", IP: "203.0.113.9", UserAgent: "test"})
		require.NoError(t, err)
		assert.Equal(t, "https://app.todayz.localhost/", l.Location)
		v := cookieValue(t, l.SetCookie)
		c, err := http.ParseSetCookie(l.SetCookie)
		require.NoError(t, err)
		assert.Equal(t, 7*24*3600, c.MaxAge, "the cookie lives as long as the window")
		var ipHash []byte
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT ip_hash FROM referral_clicks WHERE id = $1`, ids.UUID(mustParse(t, v))).Scan(&ipHash))
		assert.Len(t, ipHash, 32, "the ip is a keyed hash, never the value")
		// First touch: a live cookie is kept.
		l2, err := e.svc.Open(e.ctx, Click{Code: bizCode, Kind: "b", Cookie: v})
		require.NoError(t, err)
		assert.Empty(t, l2.SetCookie)
		assert.Equal(t, "https://business.todayz.localhost/", l2.Location)
		// An expired one is replaced.
		e.clk.Advance(8 * 24 * time.Hour)
		l3, err := e.svc.Open(e.ctx, Click{Code: code, Cookie: v})
		require.NoError(t, err)
		assert.NotEmpty(t, l3.SetCookie)
		e.clk.Set(testNow)
	})
	t.Run("a standee lands on the store", func(t *testing.T) {
		standee := ids.New(testNow)
		e.clk.Set(testNow.Add(-2 * time.Hour)) // the click comes before sign-up
		l, err := e.svc.Open(e.ctx, Click{Code: storeCode, Standee: standee.String()})
		e.clk.Set(testNow)
		require.NoError(t, err)
		assert.Equal(t, "https://app.todayz.localhost/s/"+slug+"?s="+standee.String(), l.Location)
		v := cookieValue(t, l.SetCookie)
		var landing string
		var st *[16]byte
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT landing, standee_id FROM referral_clicks WHERE id = $1`, ids.UUID(mustParse(t, v))).Scan(&landing, &st))
		assert.Equal(t, "store", landing)
		assert.NotNil(t, st)
		// The standee click attributes a retailer_customer referral.
		u, _ := e.newCustomer()
		res, err := e.claim(u, ClaimInput{Cookie: v})
		require.NoError(t, err)
		assert.Equal(t, KindRetailerCustomer, res.Kind)
		var source string
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT source FROM referrals WHERE id = $1`, ids.UUID(res.ReferralID)).Scan(&source))
		assert.Equal(t, sourceStandee, source)
		_ = store
	})
}

func mustID(t *testing.T, u pgtype.UUID) ids.ID {
	t.Helper()
	id, ok := ids.FromUUID(u)
	require.True(t, ok)
	return id
}

func mustParse(t *testing.T, s string) ids.ID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}

func TestAttribution(t *testing.T) {
	e := newEnv(t)
	_, first := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, second := e.customer(testNow.Add(-30 * 24 * time.Hour))

	click := func(code string) string {
		l, err := e.svc.Open(e.ctx, Click{Code: code})
		require.NoError(t, err)
		return cookieValue(t, l.SetCookie)
	}

	t.Run("a live click beats a typed code (first touch)", func(t *testing.T) {
		e.clk.Set(testNow.Add(-2 * time.Hour))
		v := click(first)
		e.clk.Set(testNow)
		u, _ := e.newCustomer()
		res, err := e.claim(u, ClaimInput{Code: second, Cookie: v})
		require.NoError(t, err)
		var code, source string
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT code, source FROM referrals WHERE id = $1`, ids.UUID(res.ReferralID)).Scan(&code, &source))
		assert.Equal(t, first, code)
		assert.Equal(t, sourceLink, source)
		// The same click again is the same claim.
		again, err := e.claim(u, ClaimInput{Cookie: v})
		require.NoError(t, err)
		assert.False(t, again.Created)
	})
	t.Run("a click older than the window before sign-up falls back to the code", func(t *testing.T) {
		e.clk.Set(testNow.Add(-9 * 24 * time.Hour))
		v := click(first)
		e.clk.Set(testNow)
		u, _ := e.newCustomer()
		res, err := e.claim(u, ClaimInput{Code: second, Cookie: v})
		require.NoError(t, err)
		var code string
		require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT code FROM referrals WHERE id = $1`, ids.UUID(res.ReferralID)).Scan(&code))
		assert.Equal(t, second, code)
	})
	t.Run("a click after sign-up does not attribute", func(t *testing.T) {
		u, _ := e.customer(testNow.Add(-2 * time.Hour))
		v := click(first)
		_, err := e.claim(u, ClaimInput{Cookie: v})
		requireProblem(t, err, genproblems.ReferralNotEligible)
	})
	t.Run("a stale click and no code", func(t *testing.T) {
		e.clk.Set(testNow.Add(-9 * 24 * time.Hour))
		v := click(first)
		e.clk.Set(testNow)
		u, _ := e.newCustomer()
		_, err := e.claim(u, ClaimInput{Cookie: v})
		requireProblem(t, err, genproblems.ReferralNotEligible)
	})
}

// RF-08: the window is measured from the click to sign-up, read from settings.
func TestAttributionWindow(t *testing.T) {
	e := newEnv(t)
	_, clicked := e.customer(testNow.Add(-30 * 24 * time.Hour))
	joined := testNow.Add(-time.Hour)
	day := 24 * time.Hour
	attributed := func(before time.Duration) bool {
		t.Helper()
		e.clk.Set(joined.Add(-before))
		l, err := e.svc.Open(e.ctx, Click{Code: clicked})
		require.NoError(t, err)
		e.clk.Set(testNow)
		u, _ := e.customer(joined)
		_, err = e.claim(u, ClaimInput{Cookie: cookieValue(t, l.SetCookie)})
		if err != nil {
			requireProblem(t, err, genproblems.ReferralNotEligible)
			return false
		}
		return true
	}
	assert.True(t, attributed(7*day-time.Minute), "6 d 23 h 59 m")
	assert.False(t, attributed(7*day+time.Second), "7 d + 1 s")
	e.setting("ref_attribution_window_days", "3")
	assert.False(t, attributed(4*day), "the setting changes the window")
	assert.True(t, attributed(2*day))
}

// --- qualification ---------------------------------------------------------------

func TestQualifyMinimum(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, store, _, _, _ := e.shopWithStore()
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: code})
	require.NoError(t, err)

	// ₹80 is below ₹100; ₹120 with 30 credits used is ₹90: both leave it
	// pending (RF-03, D-164).
	e.complete(e.order(u, store, 8_000, 0))
	e.complete(e.order(u, store, 12_000, 30))
	st, _, _ := e.referral(res.ReferralID)
	assert.Equal(t, statusPending, st)
	// A phone_unverified POS sale never qualifies (D-07).
	sale := Outcome{RefType: RefPOSSale, RefID: ids.New(testNow), CustomerID: u, StoreID: store, EligiblePaise: 50_000, LinkMethod: LinkPhoneUnverified}
	e.complete(sale)
	st, _, _ = e.referral(res.ReferralID)
	assert.Equal(t, statusPending, st)
	// A bill_qr sale of exactly ₹100 does.
	sale = Outcome{RefType: RefPOSSale, RefID: ids.New(testNow), CustomerID: u, StoreID: store, EligiblePaise: 10_000, LinkMethod: LinkBillQR}
	e.complete(sale)
	st, a, b := e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
	assert.True(t, a && b)
	var refType string
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT qualifying_ref_type FROM referrals WHERE id = $1`, ids.UUID(res.ReferralID)).Scan(&refType))
	assert.Equal(t, RefPOSSale, refType)
	// A later purchase changes nothing.
	e.complete(e.order(u, store, 90_000, 0))
	assert.Len(t, e.ledger.posted, 2)
}

func TestShopCounting(t *testing.T) {
	e := newEnv(t)
	e.setting("ref_shop_qualifying_orders", "3")
	refBiz, _, refCode, _, _ := e.shopWithStore()
	biz, owner, sess, _ := e.owner("submitted")
	res, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: refCode})
	require.NoError(t, err)
	store, _, _ := e.store(biz, "live", testNow)
	counted := func() int {
		return e.count(`SELECT count(*) FROM referral_shop_sales WHERE referral_id = $1`, ids.UUID(res.ReferralID))
	}

	// Not counted (D-167): the business's own staff, the referrer business's
	// staff, an unlinked or phone_unverified sale.
	e.complete(e.order(owner, store, 10_000, 0))
	refStaff, _ := e.newCustomer()
	e.staff(refBiz, refStaff, "manager")
	e.complete(e.order(refStaff, store, 10_000, 0))
	e.complete(Outcome{RefType: RefPOSSale, RefID: ids.New(testNow), StoreID: store, EligiblePaise: 10_000, LinkMethod: LinkBillQR})
	buyer, _ := e.newCustomer()
	e.complete(Outcome{RefType: RefPOSSale, RefID: ids.New(testNow), CustomerID: buyer, StoreID: store, EligiblePaise: 10_000, LinkMethod: LinkPhoneUnverified})
	assert.Zero(t, counted())

	// Counted once per order.
	o := e.order(buyer, store, 100, 0)
	e.complete(o)
	e.complete(o)
	assert.Equal(t, 1, counted())
	for range 2 {
		b, _ := e.newCustomer()
		e.complete(e.order(b, store, 100, 0))
	}
	assert.Equal(t, 3, counted())
	// Not verified yet: still pending, nothing posted.
	st, _, _ := e.referral(res.ReferralID)
	assert.Equal(t, statusPending, st)
	assert.Empty(t, e.ledger.posted)

	// Verification comes later: the sweep qualifies and enqueues the
	// reward, which the job pays.
	e.exec(`UPDATE businesses SET status = 'verified' WHERE id = $1`, ids.UUID(biz))
	out, err := e.svc.Sweep(e.ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, out["shops_qualified"])
	st, _, _ = e.referral(res.ReferralID)
	assert.Equal(t, statusQualified, st)
	assert.Empty(t, e.ledger.posted, "the sweep's own transaction posts nothing")
	e.runJobs()
	st, a, b := e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
	assert.True(t, a && b)
}

func TestShopCountingSkipsCustomerReferrer(t *testing.T) {
	e := newEnv(t)
	e.setting("ref_shop_qualifying_orders", "1")
	referrer, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	biz, owner, sess, _ := e.owner("verified")
	e.exec(`UPDATE businesses SET status = 'draft' WHERE id = $1`, ids.UUID(biz))
	res, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: code})
	require.NoError(t, err)
	e.exec(`UPDATE businesses SET status = 'verified' WHERE id = $1`, ids.UUID(biz))
	store, _, _ := e.store(biz, "live", testNow)
	e.complete(e.order(referrer, store, 10_000, 0))
	st, _, _ := e.referral(res.ReferralID)
	assert.Equal(t, statusPending, st, "the referrer's own purchase does not count")
	b, _ := e.newCustomer()
	e.complete(e.order(b, store, 10_000, 0))
	st, _, _ = e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
}

// A new shop qualifies on different customers, not purchases (D-168).
func TestShopCountingDistinctCustomers(t *testing.T) {
	e := newEnv(t)
	e.setting("ref_shop_qualifying_orders", "3")
	_, _, refCode, _, _ := e.shopWithStore()
	biz, owner, sess, _ := e.owner("verified")
	e.exec(`UPDATE businesses SET status = 'draft' WHERE id = $1`, ids.UUID(biz))
	res, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: refCode})
	require.NoError(t, err)
	e.exec(`UPDATE businesses SET status = 'verified' WHERE id = $1`, ids.UUID(biz))
	store, _, _ := e.store(biz, "live", testNow)
	counted := func() int {
		t.Helper()
		got, err := e.svc.Business(e.ctx, biz)
		require.NoError(t, err)
		require.NotNil(t, got.ReferredBy)
		return got.ReferredBy.OrdersCounted
	}

	// One buyer's three purchases and a linked sale count once.
	first, _ := e.newCustomer()
	for range 3 {
		e.complete(e.order(first, store, 10_000, 0))
	}
	e.complete(Outcome{RefType: RefPOSSale, RefID: ids.New(testNow), CustomerID: first, StoreID: store, EligiblePaise: 10_000, LinkMethod: LinkBillQR})
	assert.Equal(t, 1, counted())
	assert.Equal(t, 4, e.count(`SELECT count(*) FROM referral_shop_sales WHERE referral_id = $1`, ids.UUID(res.ReferralID)))
	second, _ := e.newCustomer()
	e.complete(e.order(second, store, 10_000, 0))
	e.complete(e.order(second, store, 10_000, 0))
	assert.Equal(t, 2, counted())
	st, _, _ := e.referral(res.ReferralID)
	assert.Equal(t, statusPending, st)
	assert.Empty(t, e.ledger.posted)

	// The third different customer qualifies it.
	third, _ := e.newCustomer()
	e.complete(e.order(third, store, 10_000, 0))
	assert.Equal(t, 3, counted())
	st, a, b := e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
	assert.True(t, a && b)
}

// A shop with no live store yet: its reward waits for one (D-72).
func TestShopRewardWaitsForAStore(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	biz, _, _, _, _ := e.shopWithStore()
	e.exec(`UPDATE stores SET status = 'paused' WHERE business_id = $1`, ids.UUID(biz))
	var storeCode string
	require.NoError(t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT short_code FROM stores WHERE business_id = $1`, ids.UUID(biz)).Scan(&storeCode))
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: storeCode})
	require.NoError(t, err)
	_ = code
	_, other, _, _, _ := e.shopWithStore()
	o := e.order(u, other, 10_000, 0)
	e.complete(o)
	st, a, b := e.referral(res.ReferralID)
	assert.Equal(t, statusQualified, st)
	assert.False(t, a, "the store side waits")
	assert.True(t, b, "the customer is paid now")
	// Reversed while the store side waits: one side posted, so the reward
	// stays (01 §7) and the waiting side is still paid later.
	require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, RefOrder, o.RefID))
	st, _, _ = e.referral(res.ReferralID)
	assert.Equal(t, statusQualified, st)
	assert.Equal(t, 1, e.flags("referral.reversed_after_reward"))
	e.exec(`UPDATE stores SET status = 'live' WHERE business_id = $1`, ids.UUID(biz))
	// The waiting side backs off: not due yet, then due.
	assert.Zero(t, e.sweep(time.Minute)["rewarded"])
	assert.EqualValues(t, 1, e.sweep(rewardRetryBase)["rewarded"])
	st, _, _ = e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
}

// --- the ledger seam ------------------------------------------------------------

func TestLedgerDownThenSweep(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, store, _, _, _ := e.shopWithStore()
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: code})
	require.NoError(t, err)

	e.ledger.err = errors.New("ledger down")
	e.complete(e.order(u, store, 10_000, 0)) // the purchase still succeeds
	st, a, b := e.referral(res.ReferralID)
	assert.Equal(t, statusQualified, st)
	assert.False(t, a || b)
	assert.Equal(t, []int{1, 10}, e.attempt(res.ReferralID), "one attempt, retried in 10 min")

	// Not due yet: the sweep leaves it alone.
	e.ledger.err, e.ledger.down = nil, true
	out := e.sweep(time.Minute)
	assert.Zero(t, out["rewarded"]+out["waiting"])
	// Due: posted false is a failure too, and backs off further.
	out = e.sweep(rewardRetryBase)
	assert.EqualValues(t, 1, out["waiting"])
	assert.Equal(t, []int{2, 20}, e.attempt(res.ReferralID))
	st, _, _ = e.referral(res.ReferralID)
	assert.Equal(t, statusQualified, st)

	e.ledger.down = false
	out = e.sweep(2 * rewardRetryBase)
	assert.EqualValues(t, 1, out["rewarded"])
	assert.Zero(t, out["waiting"])
	st, _, _ = e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
	assert.Len(t, e.ledger.posted, 2)
	// A further sweep posts nothing again.
	e.sweep(rewardRetryMax)
	assert.Len(t, e.ledger.posted, 2)
}

// attempt is a qualified referral's attempts and minutes until its next try.
func (e *env) attempt(id ids.ID) []int {
	e.t.Helper()
	var n int
	var next time.Time
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, `SELECT attempts, next_attempt_at FROM referrals WHERE id = $1`, ids.UUID(id)).Scan(&n, &next))
	return []int{n, int(next.Sub(e.clk.Now()).Minutes())}
}

func TestRewardBackoff(t *testing.T) {
	assert.Equal(t, 10*time.Minute, rewardBackoff(0))
	assert.Equal(t, 20*time.Minute, rewardBackoff(1))
	assert.Equal(t, 160*time.Minute, rewardBackoff(4))
	assert.Equal(t, 6*time.Hour, rewardBackoff(6))
	assert.Equal(t, 6*time.Hour, rewardBackoff(1000))
}

// The purchase never posts: it marks the referral qualified and enqueues one
// referral_reward job, unique per referral, in its own transaction (D-169).
func TestPurchaseEnqueuesTheReward(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, store, _, _, _ := e.shopWithStore()
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: code})
	require.NoError(t, err)

	pool := db.New(e.tdb.Pool)
	o := e.order(u, store, 10_000, 0)
	require.NoError(t, pool.InTx(e.ctx, func(ctx context.Context) error {
		if err := e.svc.OnPurchaseCompleted(ctx, o); err != nil {
			return err
		}
		return e.svc.OnPurchaseCompleted(ctx, o) // a repeat in the same transaction
	}))
	assert.Empty(t, e.ledger.posted, "the purchase posts nothing")
	st, _, _ := e.referral(res.ReferralID)
	assert.Equal(t, statusQualified, st)
	assert.Equal(t, []int{0, 0}, e.attempt(res.ReferralID), "due at once")
	require.Len(t, e.jobs.pending, 1)
	assert.Equal(t, res.ReferralID, e.jobs.pending[0].ReferralID)

	// PostRewards refuses to run inside a caller's transaction.
	err = pool.InTx(e.ctx, func(ctx context.Context) error {
		_, err := e.svc.PostRewards(ctx, res.ReferralID)
		return err
	})
	require.Error(t, err)

	e.runJobs()
	st, a, b := e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
	assert.True(t, a && b)
	require.Len(t, e.ledger.posted, 2)
	for _, r := range e.ledger.posted {
		assert.Equal(t, RewardKey(res.ReferralID, r.Side), r.IdempotencyKey)
		assert.Equal(t, "referral:"+res.ReferralID.String()+":"+r.Side, r.IdempotencyKey)
	}
	// A job run again (a retry) posts nothing more.
	got, err := e.svc.PostRewards(e.ctx, res.ReferralID)
	require.NoError(t, err)
	assert.Equal(t, RewardResult{Done: true}, got)
	assert.Len(t, e.ledger.posted, 2)
}

// Without a queue the sweep alone posts: the referral is due at once.
func TestNoQueueTheSweepPosts(t *testing.T) {
	e := newEnv(t)
	e.svc.jobs = nil
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, store, _, _, _ := e.shopWithStore()
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: code})
	require.NoError(t, err)
	e.complete(e.order(u, store, 10_000, 0))
	assert.Empty(t, e.ledger.posted)
	assert.EqualValues(t, 1, e.sweep(0)["rewarded"])
	st, _, _ := e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
}

// Stuck referrals back off and the sweep reads due rows oldest first, so
// more than a batch of stuck ones cannot starve fresh ones.
func TestStuckRewardsDoNotStarveFreshOnes(t *testing.T) {
	e := newEnv(t)
	e.svc.jobs = nil // the sweep alone posts
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, store, _, _, _ := e.shopWithStore()
	e.ledger.fail = map[ids.ID]bool{}
	stuck := sweepBatch + 20
	for range stuck {
		e.clk.Advance(time.Second)
		u, _ := e.newCustomer()
		res, err := e.claim(u, ClaimInput{Code: code})
		require.NoError(t, err)
		e.ledger.fail[res.ReferralID] = true
		e.complete(e.order(u, store, 10_000, 0))
	}
	var fresh []ids.ID
	for range 3 {
		e.clk.Advance(time.Second)
		u, _ := e.newCustomer()
		res, err := e.claim(u, ClaimInput{Code: code})
		require.NoError(t, err)
		e.complete(e.order(u, store, 10_000, 0))
		fresh = append(fresh, res.ReferralID)
	}

	// The first pass takes the oldest due batch: all stuck, all fail.
	out := e.sweep(0)
	assert.EqualValues(t, sweepBatch, out["waiting"])
	assert.Zero(t, out["rewarded"])
	// Those back off, so the next pass reaches the rest and the fresh ones.
	out = e.sweep(time.Minute)
	assert.EqualValues(t, stuck-sweepBatch, out["waiting"])
	assert.EqualValues(t, len(fresh), out["rewarded"])
	for _, id := range fresh {
		st, a, b := e.referral(id)
		assert.Equal(t, statusRewarded, st)
		assert.True(t, a && b)
	}
	// Nothing is due until the backoff passes.
	out = e.sweep(time.Minute)
	assert.Zero(t, out["waiting"]+out["rewarded"])
	assert.Equal(t, stuck, e.count(`SELECT count(*) FROM referrals WHERE status = 'qualified' AND attempts = 1`))
	assert.EqualValues(t, sweepBatch, e.sweep(rewardRetryBase)["waiting"])
}

// A deleted referrer is not paid; the side is closed without a posting.
func TestDeletedReferrerIsVoid(t *testing.T) {
	e := newEnv(t)
	referrer, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, store, _, _, _ := e.shopWithStore()
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: code})
	require.NoError(t, err)
	e.exec(`UPDATE users SET status = 'deleted', deleted_at = $2 WHERE id = $1`, ids.UUID(referrer), testNow)
	e.complete(e.order(u, store, 10_000, 0))
	st, a, b := e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
	assert.True(t, a && b)
	require.Len(t, e.ledger.posted, 1)
	assert.Equal(t, SideReferee, e.ledger.posted[0].Side)
}

// A blocked referrer is not paid while blocked; the sweep pays them once
// they are unblocked (D-168).
func TestBlockedReferrerWaits(t *testing.T) {
	e := newEnv(t)
	referrer, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, store, _, _, _ := e.shopWithStore()
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: code})
	require.NoError(t, err)
	e.exec(`UPDATE users SET status = 'blocked' WHERE id = $1`, ids.UUID(referrer))

	e.complete(e.order(u, store, 10_000, 0))
	st, a, b := e.referral(res.ReferralID)
	assert.Equal(t, statusQualified, st)
	assert.False(t, a, "the blocked referrer waits")
	assert.True(t, b, "the referee is paid now")
	require.Len(t, e.ledger.posted, 1)
	assert.Equal(t, SideReferee, e.ledger.posted[0].Side)

	// While blocked the sweep leaves it alone.
	out, err := e.svc.Sweep(e.ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 0, out["rewarded"])
	st, a, _ = e.referral(res.ReferralID)
	assert.Equal(t, statusQualified, st)
	assert.False(t, a)
	assert.Len(t, e.ledger.posted, 1)

	// Unblocked: the next sweep pays the referrer, once.
	e.exec(`UPDATE users SET status = 'active' WHERE id = $1`, ids.UUID(referrer))
	out, err = e.svc.Sweep(e.ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, out["rewarded"])
	st, a, b = e.referral(res.ReferralID)
	assert.Equal(t, statusRewarded, st)
	assert.True(t, a && b)
	require.Len(t, e.ledger.posted, 2)
	assert.Equal(t, SideReferrer, e.ledger.posted[1].Side)
	_, err = e.svc.Sweep(e.ctx)
	require.NoError(t, err)
	assert.Len(t, e.ledger.posted, 2)
}

// --- reversal (D-08) ------------------------------------------------------------

func TestReversal(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	_, store, _, _, _ := e.shopWithStore()

	t.Run("before the rewards post: back to pending", func(t *testing.T) {
		u, _ := e.newCustomer()
		res, err := e.claim(u, ClaimInput{Code: code})
		require.NoError(t, err)
		e.ledger.down = true
		o := e.order(u, store, 10_000, 0)
		e.complete(o)
		st, _, _ := e.referral(res.ReferralID)
		require.Equal(t, statusQualified, st)
		require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, RefOrder, o.RefID))
		st, _, _ = e.referral(res.ReferralID)
		assert.Equal(t, statusPending, st)
		e.ledger.down = false
	})
	t.Run("after the rewards post: they stay, a risk flag", func(t *testing.T) {
		u, _ := e.newCustomer()
		res, err := e.claim(u, ClaimInput{Code: code})
		require.NoError(t, err)
		o := e.order(u, store, 10_000, 0)
		e.complete(o)
		require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, RefOrder, o.RefID))
		st, _, _ := e.referral(res.ReferralID)
		assert.Equal(t, statusRewarded, st)
		assert.Equal(t, 1, e.flags("referral.reversed_after_reward"))
	})
	t.Run("a reversed sale is uncounted for a pending shop", func(t *testing.T) {
		e.setting("ref_shop_qualifying_orders", "5")
		_, _, refCode, _, _ := e.shopWithStore()
		biz, owner, sess, _ := e.owner("draft")
		res, err := e.svc.ClaimBusiness(e.ctx, biz, owner, sess, ClaimInput{Code: refCode})
		require.NoError(t, err)
		shop, _, _ := e.store(biz, "live", testNow)
		b, _ := e.newCustomer()
		o := e.order(b, shop, 100, 0)
		e.complete(o)
		require.Equal(t, 1, e.count(`SELECT count(*) FROM referral_shop_sales WHERE referral_id = $1`, ids.UUID(res.ReferralID)))
		require.NoError(t, e.svc.OnPurchaseReversed(e.ctx, RefOrder, o.RefID))
		assert.Zero(t, e.count(`SELECT count(*) FROM referral_shop_sales WHERE referral_id = $1`, ids.UUID(res.ReferralID)))
	})
}

// --- stats and deletion ---------------------------------------------------------

func TestStats(t *testing.T) {
	e := newEnv(t)
	referrer, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	biz, store, bizCode, storeCode, _ := e.shopWithStore()

	_, err := e.svc.Open(e.ctx, Click{Code: code})
	require.NoError(t, err)
	_, err = e.svc.Open(e.ctx, Click{Code: storeCode})
	require.NoError(t, err)
	paid, _ := e.newCustomer()
	_, err = e.claim(paid, ClaimInput{Code: code})
	require.NoError(t, err)
	e.complete(e.order(paid, store, 10_000, 0))
	waiting, _ := e.newCustomer()
	_, err = e.claim(waiting, ClaimInput{Code: code})
	require.NoError(t, err)
	viaStore, _ := e.newCustomer()
	_, err = e.claim(viaStore, ClaimInput{Code: storeCode})
	require.NoError(t, err)

	me, err := e.svc.Me(e.ctx, referrer)
	require.NoError(t, err)
	assert.Equal(t, code, me.Code)
	assert.Equal(t, "https://api.todayz.localhost/v1/public/ref/"+code+"?k=c", me.Link)
	assert.Equal(t, 1, me.Stats.Clicks)
	assert.Equal(t, 1, me.Stats.Pending)
	assert.Equal(t, 1, me.Stats.Rewarded)
	assert.EqualValues(t, 30, me.Stats.CreditsEarned)
	assert.EqualValues(t, 30, me.Rewards.ReferrerCredits)
	assert.EqualValues(t, 100, me.Rewards.RefereeCredits)
	assert.Nil(t, me.ReferredBy)

	mine, err := e.svc.Me(e.ctx, paid)
	require.NoError(t, err)
	require.NotNil(t, mine.ReferredBy)
	assert.EqualValues(t, statusRewarded, mine.ReferredBy.Status)

	bz, err := e.svc.Business(e.ctx, biz)
	require.NoError(t, err)
	assert.Equal(t, bizCode, bz.Code)
	assert.Equal(t, "https://api.todayz.localhost/v1/public/ref/"+bizCode+"?k=b", bz.Link)
	assert.Equal(t, 1, bz.Stats.Clicks, "the store's standee and links count for its business")
	assert.Equal(t, 1, bz.Stats.Pending)

	e.setting("ref_shop_qualifying_orders", "4")
	nb, owner, sess, _ := e.owner("draft")
	_, err = e.svc.ClaimBusiness(e.ctx, nb, owner, sess, ClaimInput{Code: bizCode})
	require.NoError(t, err)
	got, err := e.svc.Business(e.ctx, nb)
	require.NoError(t, err)
	require.NotNil(t, got.ReferredBy)
	assert.Equal(t, 0, got.ReferredBy.OrdersCounted)
	assert.Equal(t, 4, got.ReferredBy.OrdersNeeded)
	assert.False(t, got.ReferredBy.Verified)
}

func TestAccountDelete(t *testing.T) {
	e := newEnv(t)
	_, code := e.customer(testNow.Add(-30 * 24 * time.Hour))
	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Code: code})
	require.NoError(t, err)
	require.NoError(t, e.svc.OnAccountDelete(e.ctx, u))
	require.NoError(t, e.svc.OnAccountDelete(e.ctx, u), "idempotent")
	st, _, _ := e.referral(res.ReferralID)
	assert.Equal(t, statusRejected, st)
}

// Without the database, settings or the hash key every route answers 503;
// a missing clock or ledger is a wiring error.
func TestNotConfigured(t *testing.T) {
	svc, err := New(Deps{Clock: clock.NewFake(testNow), Ledger: &fakeLedger{}})
	require.NoError(t, err)
	_, err = svc.PostRewards(context.Background(), ids.New(testNow))
	require.Error(t, err)
	_, err = svc.Open(context.Background(), Click{Code: "ABCDEF"})
	requireProblem(t, err, genproblems.ServiceUnavailable)
	_, err = svc.ClaimCustomer(context.Background(), ids.New(testNow), ids.New(testNow), ClaimInput{Code: "ABCDEF"})
	requireProblem(t, err, genproblems.ServiceUnavailable)
	require.NoError(t, svc.OnPurchaseCompleted(context.Background(), Outcome{}), "the purchase hook is a no-op")
	_, err = New(Deps{Ledger: &fakeLedger{}})
	require.Error(t, err, "no clock")
	_, err = New(Deps{Clock: clock.NewFake(testNow)})
	require.Error(t, err, "no ledger")
}
