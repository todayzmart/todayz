package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/credits"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/pos"
)

// The POS sale over the real credits ledger and referrals (W2-04 with W2-01
// and W2-03, D-132): no recording seams, so posDeps wires what the binary
// wires. Codes, claim tokens and phones stay in variables, as in
// pos_http_test.go.

// ledgerShop is the orders fixture with pos on the real ledger; the
// customer holds n credits.
type ledgerShop struct {
	*posShop
	custID pgtype.UUID
}

func newLedgerShop(t *testing.T, n int64) *ledgerShop {
	t.Helper()
	posSeams = nil
	p := &posShop{orderShop: newOrderShop(t)}
	return &ledgerShop{posShop: p, custID: p.grantCredits(n)}
}

// balance is an account's balance, 0 when it has none yet.
func (p *ledgerShop) balance(kind credits.AccountKind, owner any) int {
	return p.a.count(`SELECT COALESCE((SELECT b.balance FROM account_balances b JOIN ledger_accounts a ON a.id = b.account_id
	  WHERE a.kind = $1 AND a.owner_id = $2), 0)::int`, string(kind), owner)
}

func (p *ledgerShop) wallet() int { return p.balance(credits.CustomerWallet, p.custID) }

func (p *ledgerShop) store() int { return p.balance(credits.StoreCredit, uuidOf(p.t, p.sid)) }

func (p *ledgerShop) txs(kind, refID string) int {
	return p.a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = $1 AND ref_id = $2`, kind, uuidOf(p.t, refID))
}

func (p *ledgerShop) holdStatus(claimID string) string {
	var s string
	require.NoError(p.t, p.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT status FROM credit_holds WHERE bill_claim_id = $1`, uuidOf(p.t, claimID)).Scan(&s))
	return s
}

// confirmed is a bill for atta×1 (₹290) whose claim the customer confirmed
// with 10 credits.
func (p *ledgerShop) confirmed() (bill, claim, token string) {
	p.t.Helper()
	bill = p.newID()
	claim, token = p.claim(p.cashier, bill, 29000)
	c := p.confirm(p.cust, token, 10)
	require.Equal(p.t, http.StatusOK, c.code, "confirm: status %d", c.code)
	require.EqualValues(p.t, 10, c.body["creditsUsed"])
	require.Equal(p.t, "held", p.holdStatus(claim))
	return bill, claim, token
}

// ledgerSweeper is the worker's pos service (jobs.PosJobDeps, real ledger)
// on clk.
func (p *ledgerShop) ledgerSweeper(clk clock.Clock) *pos.Service {
	p.t.Helper()
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	pd, err := jobs.PosJobDeps(jobs.Deps{Pool: p.a.tdb.Pool, Clock: clk, Config: p.cfg, Log: log,
		Settings: settings.NewCache(settings.PGLoader(p.a.tdb.Pool), settings.Options{Log: log})}, nil)
	require.NoError(p.t, err)
	svc, err := pos.New(pd)
	require.NoError(p.t, err)
	return svc
}

// A bill QR sale with credits: confirm holds them, the sale binds the claim
// and captures the hold, the cashback posts on what was paid, and the
// balance moves once however often the sale is replayed. Reversing it gives
// the credits back and takes the cashback back.
func TestPOSLedgerBillQRDB(t *testing.T) {
	p := newLedgerShop(t, 50)
	p.setCashback(500)
	bill, claim, tok := p.confirmed()
	require.Equal(t, 40, p.wallet(), "10 held")

	body := p.qrSale(bill, claim)
	s := p.sell(p.cashier, body)
	require.Equal(t, http.StatusOK, s.code, s.raw)
	sale := s.body["id"].(string)
	require.EqualValues(t, 10, s.body["creditsUsed"])
	require.EqualValues(t, 14, s.body["cashbackCredits"], "5% of the ₹280 paid")
	require.Equal(t, "captured", p.holdStatus(claim))
	require.Equal(t, 54, p.wallet())
	require.Equal(t, 10-14, p.store(), "the spent credits in, the cashback out")

	// A retry of the same bill (a new idempotency key) replays the sale.
	again := p.sell(p.cashier, body)
	require.Equal(t, http.StatusOK, again.code, again.raw)
	require.Equal(t, sale, again.body["id"])
	require.Equal(t, 1, p.txs("spend_capture", claim))
	require.Equal(t, 1, p.txs("cashback", sale))
	require.Equal(t, 54, p.wallet())

	r := p.reverse(p.manager, sale)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 2, p.txs("reversal", sale), "reverse: and clawback:")
	require.Equal(t, 50, p.wallet(), "the 10 back, the 14 taken back")
	require.Equal(t, 0, p.store())
	r = p.reverse(p.manager, sale)
	require.NotEqual(t, http.StatusOK, r.code, "a reversed sale is not reversed again")
	require.Equal(t, 2, p.txs("reversal", sale))
	require.Equal(t, 50, p.wallet())
	p.noSecrets(tok)
}

// A confirmed claim nobody rang expires: the worker's sweep releases its
// hold, and the sale after it is refused and spends nothing.
func TestPOSLedgerSweepReleasesDB(t *testing.T) {
	p := newLedgerShop(t, 50)
	bill, claim, tok := p.confirmed()
	p.advance(121 * time.Second)
	fr, err := p.runner.RunScheduled(context.Background(), jobs.KindPosBillClaimExpiry)
	require.NoError(t, err)
	require.Equal(t, platformjobs.StatusSucceeded, fr.Status, "%v", fr.Err)
	require.EqualValues(t, 1, fr.Result.Summary["released"])
	require.Equal(t, "released", p.holdStatus(claim))
	require.Equal(t, 50, p.wallet())

	r := p.sell(p.cashier, p.qrSale(bill, claim))
	require.Equal(t, "claim_expired", problemCode(r), r.raw)
	require.Zero(t, p.txs("spend_capture", claim))
	require.Equal(t, 50, p.wallet())
	p.noSecrets(tok)
}

// A hold the ledger released while its claim still looks live (the sweep's
// release committed, the claim row did not move yet) is never captured: the
// sale is claim_expired and leaves nothing, cashback included.
func TestPOSLedgerCaptureAfterReleaseDB(t *testing.T) {
	p := newLedgerShop(t, 50)
	p.setCashback(500)
	bill, claim, tok := p.confirmed()
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ledger := creditsService(&deps{DB: db.New(p.a.tdb.Pool), Clock: p.a.clk, Config: p.cfg, Log: log,
		Settings: settings.NewCache(settings.PGLoader(p.a.tdb.Pool), settings.Options{Log: log})})
	_, err := ledger.Release(context.Background(), credits.Ref{Type: credits.RefBillClaim, ID: idOf(t, claim)})
	require.NoError(t, err)
	require.Equal(t, 50, p.wallet())

	r := p.sell(p.cashier, p.qrSale(bill, claim))
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "claim_expired", problemCode(r))
	require.Zero(t, p.a.count(`SELECT count(*) FROM pos_sales WHERE client_id = $1`, uuidOf(t, bill)))
	require.Zero(t, p.txs("spend_capture", claim))
	require.Zero(t, p.a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'cashback' AND ref_type = 'pos_sale'`))
	require.Equal(t, 50, p.wallet())
	require.Equal(t, "claimed", p.getClaim(claim).body["status"], "rolled back with the sale")
	p.noSecrets(tok)
}

// A sync entry sent twice (the device retried) applies once: one sale, one
// cashback.
func TestPOSLedgerSyncReplayDB(t *testing.T) {
	p := newLedgerShop(t, 50)
	p.setCashback(500)
	secret := p.seed(p.cust)
	now := p.a.clk.Now()
	link := map[string]any{"method": "counter_code", "phone": custPhone, "code": testTOTP(secret, now), "clockOffsetMs": 0}
	e := p.saleEntry(1, now, link, line{p.atta, 1})
	r := p.sync(p.cashier, e)
	require.Equal(t, []string{"applied"}, statuses(r), "sync: status %d", r.code)
	require.Equal(t, []any{"linked"}, linkStatuses(r))
	sale := results(r)[0]["saleId"].(string)
	require.Equal(t, 1, p.txs("cashback", sale))
	require.Equal(t, 50+14, p.wallet(), "5% of ₹290, whole credits")

	r = p.sync(p.cashier, e)
	require.Equal(t, http.StatusOK, r.code, "replay: status %d", r.code)
	require.Equal(t, 1, p.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1`, uuidOf(t, p.sid)))
	require.Equal(t, 1, p.txs("cashback", sale))
	require.Equal(t, 50+14, p.wallet())
	p.noSecrets(custPhone)
}

// A referred customer's linked sale of at least ref_qualifying_min_order_paise
// (₹100) qualifies the referral (a smaller one leaves it pending), and
// reversing that sale before any reward posted sends it back to pending.
func TestPOSLedgerReferralDB(t *testing.T) {
	p := newLedgerShop(t, 50)
	ref := ids.New(p.a.clk.Now())
	p.a.exec(`INSERT INTO referrals (id, kind, referrer_type, referrer_id, referee_type, referee_id, code, source,
	    referrer_credits, referee_credits, created_at, updated_at)
	  VALUES ($1, 'retailer_customer', 'store', $2, 'customer', $3, 'ABCDEF', 'code', 20, 20, $4, $4)`,
		ids.UUID(ref), uuidOf(t, p.sid), p.custID, p.a.clk.Now())
	status := func() (string, pgtype.UUID) {
		var s string
		var q pgtype.UUID
		require.NoError(t, p.a.tdb.OwnerPool.QueryRow(context.Background(),
			`SELECT status, qualifying_ref_id FROM referrals WHERE id = $1`, ids.UUID(ref)).Scan(&s, &q))
		return s, q
	}
	secret := p.seed(p.cust)
	var sales []string
	for _, qty := range []int{3, 4} { // ₹75, ₹100
		p.a.clk.Advance(61 * time.Second) // a new step: a code is used once
		bill := p.newID()
		r := p.verifyCode(p.cashier, custPhone, testTOTP(secret, p.a.clk.Now()), bill)
		require.Equal(t, http.StatusOK, r.code, "verify: status %d", r.code)
		s := p.codeSaleOf(bill, line{p.salt, qty})
		require.Equal(t, http.StatusOK, s.code, s.raw)
		sales = append(sales, s.body["id"].(string))
		st, q := status()
		if qty == 3 {
			require.Equal(t, "pending", st, "₹75 is below the minimum")
			require.False(t, q.Valid)
			continue
		}
		require.Equal(t, "qualified", st)
		require.Equal(t, uuidOf(t, sales[1]), q)
	}
	require.Zero(t, p.a.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'referral_reward' AND idempotency_key LIKE 'referral:' || $1 || '%'`,
		ref.String()), "no reward posts inside the sale")

	r := p.reverse(p.manager, sales[1])
	require.Equal(t, http.StatusOK, r.code, r.raw)
	st, q := status()
	require.Equal(t, "pending", st, "revoked before any reward posted")
	require.False(t, q.Valid)
	p.noSecrets(custPhone)
}

// codeSaleOf is a counter_code sale of lines for a verified bill.
func (p *ledgerShop) codeSaleOf(bill string, lines ...line) resp {
	body := p.saleBody(bill, lines...)
	body["link"] = map[string]any{"method": "counter_code"}
	return p.sell(p.cashier, body)
}

// The bind racing the sweep, as an API whose clock is a second short of the
// claim's expiry and a worker whose clock is past it: whichever takes the
// claim first wins, the other sees it taken, nothing deadlocks, and the
// customer's credits are spent or given back exactly once.
func TestPOSLedgerBindRacesSweepDB(t *testing.T) {
	p := newLedgerShop(t, 100)
	worker := clock.NewFake(p.a.clk.Now())
	sweeper := p.ledgerSweeper(worker)
	wallet, won := 100, map[string]int{}
	// The sale goes through HTTP and is slower than the sweep, so the sweep
	// starts later round by round: the first rounds race it head on, the
	// last lets the sale bind first.
	delays := []time.Duration{0, 2, 5, 10, 20, 40, 80, 500}
	for round, delay := range delays {
		bill, claim, _ := p.confirmed()
		p.advance(119 * time.Second)
		worker.Set(p.a.clk.Now().Add(2 * time.Second))
		var (
			s     resp
			sw    pos.Sweep
			swErr error
		)
		parallel(
			func() { s = p.sell(p.cashier, p.qrSale(bill, claim)) },
			func() {
				time.Sleep(delay * time.Millisecond)
				sw, swErr = sweeper.ExpireClaims(context.Background())
			},
		)
		require.NoError(t, swErr, "round %d", round)
		switch s.code {
		case http.StatusOK:
			require.Equal(t, pos.Sweep{}, sw, "round %d: the sale took the claim", round)
			require.Equal(t, "captured", p.holdStatus(claim), "round %d", round)
			require.Equal(t, 1, p.txs("spend_capture", claim), "round %d", round)
			wallet -= 10
			won["sale"]++
		default:
			won["sweep"]++
			require.Equal(t, "claim_expired", problemCode(s), "round %d: %s", round, s.raw)
			require.Equal(t, pos.Sweep{Expired: 1, Released: 1}, sw, "round %d", round)
			require.Equal(t, "released", p.holdStatus(claim), "round %d", round)
			require.Zero(t, p.txs("spend_capture", claim), "round %d", round)
		}
		require.Equal(t, wallet, p.wallet(), "round %d", round)
		p.advance(2 * time.Second)
	}
	t.Logf("winners: %v", won)
	require.Positive(t, won["sweep"], "the head-on rounds")
	require.Positive(t, won["sale"], "the round the sweep starts late")
}
