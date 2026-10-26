package credits

import (
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// D-240: A-09 finds and adjusts a customer who has never had a ledger
// posting, so has no wallet account yet.

const referralAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// newCustomer is a user who has signed in to the customer app (a customer
// profile) and has no ledger account.
func (a *aenv) newCustomer() ids.ID {
	a.t.Helper()
	id := a.user()
	code := make([]byte, 6)
	for i := range code {
		code[i] = referralAlphabet[rand.IntN(len(referralAlphabet))] // #nosec G404 -- a test referral code
	}
	a.exec(`INSERT INTO customer_profiles (user_id, referral_code) VALUES ($1, $2)`, ids.UUID(id), string(code))
	return id
}

func (a *aenv) walletAccounts(customer ids.ID) int {
	return a.count(`SELECT count(*) FROM ledger_accounts WHERE kind = 'customer_wallet' AND owner_id = $1`, ids.UUID(customer))
}

func (a *aenv) adjustCustomer(p authn.Principal, customer ids.ID, paise int, key string) (string, string, string, error) {
	r, err := a.adm.CreateAdjustment(a.ctx, p, AdjustmentInput{
		CustomerID: customer.String(), AmountPaise: paise, Reason: "goodwill for a late pickup", IdempotencyKey: key,
	})
	if err != nil {
		return "", "", "", err
	}
	req, tx := "", ""
	if r.Request != nil {
		req = r.Request.Id
	}
	if r.TransactionId != nil {
		tx = *r.TransactionId
	}
	return string(r.Outcome), req, tx, nil
}

func TestLedgerAccountsUnopenedWalletDB(t *testing.T) {
	a := newAdjustEnv(t)
	c := a.newCustomer()

	res, err := a.adm.LedgerAccounts(a.ctx, c.String())
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	it := res.Items[0]
	require.Nil(t, it.Id, "no account yet")
	require.Equal(t, "customer_wallet", string(it.Kind))
	require.Equal(t, "customer", string(it.OwnerType))
	require.NotNil(t, it.OwnerId)
	require.Equal(t, c.String(), *it.OwnerId)
	require.Zero(t, it.BalancePaise)
	require.Zero(t, a.walletAccounts(c), "a search opens nothing")

	// A customer with a wallet is listed once, by the account.
	res, err = a.adm.LedgerAccounts(a.ctx, a.customer.String())
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.NotNil(t, res.Items[0].Id)
	require.Equal(t, a.account, *res.Items[0].Id)

	// A user who never used the customer app, and a deleted customer, are not listed.
	res, err = a.adm.LedgerAccounts(a.ctx, a.user().String())
	require.NoError(t, err)
	require.Empty(t, res.Items)
	gone := a.newCustomer()
	a.exec(`UPDATE users SET status = 'deleted', deleted_at = now() WHERE id = $1`, ids.UUID(gone))
	res, err = a.adm.LedgerAccounts(a.ctx, gone.String())
	require.NoError(t, err)
	require.Empty(t, res.Items)

	// By phone.
	h, err := pii.NewHasher(make([]byte, 32))
	require.NoError(t, err)
	adm, err := NewAdmin(AdminDeps{Ledger: a.svc, Audit: audit.New(db.New(a.tdb.Pool), a.clk, nil), Hash: h})
	require.NoError(t, err)
	const phone = "+919000000240"
	a.exec(`INSERT INTO phone_identities (id, user_id, phone_enc, phone_hash, verified_at) VALUES ($1, $2, '\x00', $3, $4)`,
		ids.UUID(a.gen.New(a.clk.Now())), ids.UUID(c), h.Phone(phone), a.clk.Now())
	res, err = adm.LedgerAccounts(a.ctx, "9000000240")
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.Nil(t, res.Items[0].Id)
	require.Equal(t, c.String(), *res.Items[0].OwnerId)
}

// An at-once adjustment by customer id opens the wallet in its own
// transaction and posts once; a replay of its key is the same transaction.
func TestAdjustmentOpensWalletDB(t *testing.T) {
	a := newAdjustEnv(t) // threshold ₹500
	c := a.newCustomer()

	out, req, tx, err := a.adjustCustomer(a.finance, c, 20000, "open-1")
	require.NoError(t, err)
	require.Equal(t, "posted", out)
	require.Empty(t, req)
	require.NotEmpty(t, tx)
	require.Equal(t, 1, a.walletAccounts(c))
	require.EqualValues(t, 200, a.balance(wallet(c)))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'credits.adjust' AND after->>'customerId' = $1`, c.String()))

	out, _, tx2, err := a.adjustCustomer(a.finance, c, 20000, "open-1")
	require.NoError(t, err)
	require.Equal(t, "posted", out)
	require.Equal(t, tx, tx2, "a replay answers the first transaction")
	require.EqualValues(t, 200, a.balance(wallet(c)))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))
	require.Equal(t, 1, a.walletAccounts(c))

	// The search now shows the account.
	res, err := a.adm.LedgerAccounts(a.ctx, c.String())
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.NotNil(t, res.Items[0].Id)
	require.EqualValues(t, 20000, res.Items[0].BalancePaise)

	// customerId on an open wallet uses it, and the day's sum (D-237) counts
	// the posting that opened it: ₹200 + ₹200 fits under ₹500, a third does not.
	out, _, _, err = a.adjustCustomer(a.finance, c, 20000, "")
	require.NoError(t, err)
	require.Equal(t, "posted", out)
	out, _, _, err = a.adjustCustomer(a.finance, c, 20000, "")
	require.NoError(t, err)
	require.Equal(t, "pending", out)
	require.EqualValues(t, 400, a.balance(wallet(c)))
	require.Equal(t, 1, a.walletAccounts(c))
}

// A request above the threshold opens nothing; its approval opens the wallet.
func TestAdjustmentUnopenedWalletApprovalDB(t *testing.T) {
	a := newAdjustEnv(t)
	c := a.newCustomer()

	out, req, _, err := a.adjustCustomer(a.finance, c, 60000, "big-1")
	require.NoError(t, err)
	require.Equal(t, "pending", out)
	require.Zero(t, a.walletAccounts(c), "a pending request opens nothing")

	// A replay of the request's key is the same request.
	_, req2, _, err := a.adjustCustomer(a.finance, c, 60000, "big-1")
	require.NoError(t, err)
	require.Equal(t, req, req2)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_adjustment_requests WHERE customer_id = $1`, ids.UUID(c)))

	rid, err := ids.Parse(req)
	require.NoError(t, err)
	r, err := a.adm.ApproveAdjustment(a.ctx, a.finance2, rid, nil)
	require.NoError(t, err)
	require.Equal(t, "approved", string(r.Outcome))
	require.Equal(t, 1, a.walletAccounts(c))
	require.EqualValues(t, 600, a.balance(wallet(c)))
}

func TestAdjustmentUnopenedWalletRefusalsDB(t *testing.T) {
	a := newAdjustEnv(t)
	c := a.newCustomer()

	// A debit of a wallet that holds nothing, at once or as a request.
	for _, paise := range []int{-20000, -60000} {
		_, _, _, err := a.adjustCustomer(a.finance, c, paise, "")
		require.Equal(t, "adjustment_insufficient_balance", problem.CodeOf(err), paise)
	}
	require.Zero(t, a.walletAccounts(c), "a refused debit opens nothing")

	gone := a.newCustomer()
	a.exec(`UPDATE users SET status = 'deleted', deleted_at = now() WHERE id = $1`, ids.UUID(gone))
	for name, who := range map[string]ids.ID{
		"unknown":    a.gen.New(a.clk.Now()),
		"no profile": a.user(),
		"admin":      a.finance2.UserID,
		"deleted":    gone,
	} {
		_, _, _, err := a.adjustCustomer(a.finance, who, 20000, "")
		require.Equal(t, "validation_failed", problem.CodeOf(err), name)
		require.Zero(t, a.walletAccounts(who), name)
	}

	for name, in := range map[string]AdjustmentInput{
		"both":    {AccountID: a.account, CustomerID: c.String()},
		"neither": {},
		"bad id":  {CustomerID: "not-a-ulid"},
	} {
		in.AmountPaise, in.Reason = 20000, "goodwill"
		_, err := a.adm.CreateAdjustment(a.ctx, a.finance, in)
		require.Equal(t, "validation_failed", problem.CodeOf(err), name)
	}
	require.Zero(t, a.walletAccounts(c))
	require.Zero(t, a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'admin_adjustment'`))
}

// Concurrent first adjustments: one admin's still respect the day's sum
// (D-237) and open one wallet; different admins' (different lock keys) open
// one wallet between them and all post.
func TestAdjustmentUnopenedWalletConcurrentDB(t *testing.T) {
	a := newAdjustEnv(t) // threshold ₹500
	run := func(n int, who func() authn.Principal, c ids.ID) map[string]int {
		outs := make(chan string, n)
		var wg sync.WaitGroup
		for range n {
			p := who()
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, _, _, err := a.adjustCustomer(p, c, 20000, "")
				if err != nil {
					outs <- "error: " + problem.CodeOf(err)
					return
				}
				outs <- out
			}()
		}
		wg.Wait()
		close(outs)
		got := map[string]int{}
		for out := range outs {
			got[out]++
		}
		return got
	}

	c := a.newCustomer()
	got := run(6, func() authn.Principal { return a.finance }, c)
	require.Equal(t, map[string]int{"posted": 2, "pending": 4}, got)
	require.Equal(t, 1, a.walletAccounts(c))
	require.EqualValues(t, 400, a.balance(wallet(c)))

	c2 := a.newCustomer()
	got = run(4, a.admin, c2)
	require.Equal(t, map[string]int{"posted": 4}, got)
	require.Equal(t, 1, a.walletAccounts(c2))
	require.EqualValues(t, 800, a.balance(wallet(c2)))
}
