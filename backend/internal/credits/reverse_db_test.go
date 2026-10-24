package credits

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// D-132: a counter sale's credits and cashback. The spend is a bill claim's
// hold; the cashback and the reversal are on the sale's own ref.

func (e *denv) sale() Ref     { return Ref{Type: RefPOSSale, ID: e.gen.New(e.clk.Now())} }
func (e *denv) claimRef() Ref { return Ref{Type: RefBillClaim, ID: e.gen.New(e.clk.Now())} }

func bp(n int) *int { return &n }

func (e *denv) completeWith(c Completion) (Completed, error) {
	var out Completed
	err := e.tx(func(ctx context.Context) error {
		var err error
		out, err = e.svc.Complete(ctx, c)
		return err
	})
	return out, err
}

func (e *denv) reverse(r Reversal) (Reversed, error) {
	var out Reversed
	err := e.tx(func(ctx context.Context) error {
		var err error
		out, err = e.svc.Reverse(ctx, r)
		return err
	})
	return out, err
}

func (e *denv) txCount(key string) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM ledger_transactions WHERE idempotency_key = $1`, key)
}

// capture is the bill claim's capture as POS asks for it: no cashback on the
// claim, and never a released hold.
func (e *denv) capture(claim Ref, c, s ids.ID) (Completed, error) {
	return e.completeWith(Completion{Ref: claim, CustomerID: c, StoreID: s, CashbackBP: bp(0), RefuseReleased: true})
}

// cashback is the sale's cashback on the paid amount at the store's rate.
func (e *denv) cashback(sale Ref, c, s ids.ID, paid int64, rate int) (Completed, error) {
	return e.completeWith(Completion{Ref: sale, CustomerID: c, StoreID: s, EligiblePaise: paid, CashbackBP: bp(rate)})
}

// D-08, D-74: the spent credits go back to their lots, the part of a lot due
// by now to platform_expired; the cashback is clawed back from the restored
// balance.
func TestReverseRestoresLotsDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 500)
	e.grant(cust, 20) // lot A, due 2027-06-02
	e.clk.Advance(30 * 24 * time.Hour)
	e.grant(cust, 20) // lot B, a month later
	expiry := time.Date(2027, 6, 2, 0, 0, 0, 0, clock.IST)
	e.clk.Set(expiry.Add(-time.Hour))

	claim, sale := e.claimRef(), e.sale()
	require.Equal(t, 30, e.hold(claim, cust, shop, 30, 100000).Allowed, "20 from A, 10 from B")
	done, err := e.capture(claim, cust, shop)
	require.NoError(t, err)
	require.Equal(t, Completed{Captured: 30}, done)
	done, err = e.cashback(sale, cust, shop, 100000, 500)
	require.NoError(t, err)
	require.Equal(t, Completed{Cashback: 50}, done)
	require.EqualValues(t, 60, e.balance(wallet(cust)))
	require.EqualValues(t, 30-50, e.balance(store(shop)))
	e.clean()

	e.clk.Set(expiry) // lot A is due now
	got, err := e.reverse(Reversal{Ref: sale, Spend: claim, CustomerID: cust, StoreID: shop})
	require.NoError(t, err)
	require.Equal(t, Reversed{Restored: 10, Expired: 20, ClawedBack: 50}, got)
	require.EqualValues(t, 20, e.balance(wallet(cust)))
	require.EqualValues(t, 0, e.balance(store(shop)))
	require.EqualValues(t, 20, e.balance(Account{Kind: PlatformExpired}))
	require.Equal(t, 1, e.txCount(sale.key("reverse")))
	require.Equal(t, 1, e.txCount(sale.key("clawback")))
	e.clean()

	// A repeat posts nothing and returns the first result.
	again, err := e.reverse(Reversal{Ref: sale, Spend: claim, CustomerID: cust, StoreID: shop})
	require.NoError(t, err)
	require.Equal(t, got, again)
	require.Equal(t, 1, e.txCount(sale.key("reverse")))
	require.Equal(t, 1, e.txCount(sale.key("clawback")))
	require.EqualValues(t, 20, e.balance(wallet(cust)))

	// The reversed sale takes no more cashback, the captured claim no capture.
	_, err = e.cashback(sale, cust, shop, 100000, 500)
	require.ErrorIs(t, err, ErrReversed)
	again2, err := e.capture(claim, cust, shop)
	require.NoError(t, err, "the claim's ref is not the reversed one")
	require.Equal(t, Completed{Captured: 30}, again2, "a captured hold replays its capture")

	lines, err := e.svc.WalletLines(e.ctx, cust, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, lines.Items)
	e.clean()
}

// D-74: the clawback stops at the available balance; the store absorbs the
// rest. With nothing left to claw back the reversal still marks the sale.
func TestReverseClawbackShortfallDB(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spend int // credits spent elsewhere after the cashback
		want  Reversed
	}{
		{"partial", 30, Reversed{ClawedBack: 20, Shortfall: 30}},
		{"none left", 50, Reversed{ClawedBack: 0, Shortfall: 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newDBEnv(t)
			cust, shop, other := e.user(), e.store("Lakshmi Traders", 500), e.store("Ravi Stores", 0)
			sale := e.sale()
			_, err := e.cashback(sale, cust, shop, 100000, 500)
			require.NoError(t, err)
			for left := tc.spend; left > 0; left -= 30 {
				o := e.order()
				n := min(left, 30)
				require.Equal(t, n, e.hold(o, cust, other, n, 100000).Allowed)
				require.Equal(t, n, e.complete(o, cust, other, 100000).Captured)
			}
			got, err := e.reverse(Reversal{Ref: sale, CustomerID: cust, StoreID: shop})
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.EqualValues(t, 0, e.balance(wallet(cust)))
			require.EqualValues(t, -int64(tc.want.Shortfall), e.balance(store(shop)), "the store absorbs the shortfall")
			require.Equal(t, 1, e.txCount(sale.key("reverse")))
			e.clean()

			again, err := e.reverse(Reversal{Ref: sale, CustomerID: cust, StoreID: shop})
			require.NoError(t, err)
			require.Equal(t, tc.want, again)
			require.Equal(t, 1, e.txCount(sale.key("reverse")))
			_, err = e.cashback(sale, cust, shop, 100000, 500)
			require.ErrorIs(t, err, ErrReversed)
			e.clean()
		})
	}
}

// A sale with no credits and no cashback reverses to nothing; a reversal
// naming another customer or store, or an open hold, is refused.
func TestReverseEdgesDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 500)
	e.grant(cust, 30)

	got, err := e.reverse(Reversal{Ref: e.sale(), Spend: e.claimRef(), CustomerID: cust, StoreID: shop})
	require.NoError(t, err)
	require.Equal(t, Reversed{}, got)
	require.Zero(t, e.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'reversal'`))

	claim, sale := e.claimRef(), e.sale()
	require.Equal(t, 20, e.hold(claim, cust, shop, 20, 50000).Allowed)
	_, err = e.reverse(Reversal{Ref: sale, Spend: claim, CustomerID: cust, StoreID: shop})
	require.ErrorIs(t, err, ErrHoldOpen)
	_, err = e.capture(claim, cust, shop)
	require.NoError(t, err)
	_, err = e.cashback(sale, cust, shop, 48000, 500)
	require.NoError(t, err)
	_, err = e.reverse(Reversal{Ref: sale, Spend: claim, CustomerID: e.user(), StoreID: shop})
	require.ErrorIs(t, err, ErrInvalid, "another customer")
	_, err = e.reverse(Reversal{Ref: sale, Spend: claim, CustomerID: cust, StoreID: e.store("Ravi Stores", 0)})
	require.ErrorIs(t, err, ErrInvalid, "another store")
	_, err = e.reverse(Reversal{Ref: sale, CustomerID: cust})
	require.ErrorIs(t, err, ErrInvalid)

	// Hold refuses the sale's own ref: it has no hold.
	err = e.tx(func(ctx context.Context) error {
		_, err := e.svc.Hold(ctx, HoldRequest{Ref: e.sale(), CustomerID: cust, StoreID: shop, Credits: 5, EligiblePaise: 50000})
		return err
	})
	require.ErrorIs(t, err, ErrInvalid)
	e.clean()
}

// RefuseReleased: capturing a released claim is refused and posts nothing,
// cashback included.
func TestCompleteRefuseReleasedDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 500)
	e.grant(cust, 30)
	claim := e.claimRef()
	require.Equal(t, 30, e.hold(claim, cust, shop, 30, 50000).Allowed)
	require.Equal(t, 30, e.release(claim))
	_, err := e.completeWith(Completion{Ref: claim, CustomerID: cust, StoreID: shop, EligiblePaise: 50000, CashbackBP: bp(500), RefuseReleased: true})
	require.ErrorIs(t, err, ErrReleased)
	require.Zero(t, e.txCount(claim.key("cashback")))
	require.Zero(t, e.txCount(claim.key("capture")))
	require.EqualValues(t, 30, e.balance(wallet(cust)))
	require.EqualValues(t, 0, e.balance(store(shop)))
	e.clean()
}

// A replayed sync or a retried job asks for the sale's cashback again: it
// posts once.
func TestPOSCashbackOnceDB(t *testing.T) {
	e := newDBEnv(t)
	cust, shop := e.user(), e.store("Lakshmi Traders", 500)
	sale := e.sale()
	first, err := e.cashback(sale, cust, shop, 64000, 500)
	require.NoError(t, err)
	require.Equal(t, Completed{Cashback: 32}, first)
	for range 3 {
		again, err := e.cashback(sale, cust, shop, 64000, 500)
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
	require.Equal(t, 1, e.txCount(sale.key("cashback")))
	require.EqualValues(t, 32, e.balance(wallet(cust)))
	e.clean()
}

// Reverse racing the sale's cashback: whichever locks the accounts first,
// the customer ends with no net cashback and the spent credits back once.
func TestReverseRacesCompleteDB(t *testing.T) {
	e := newDBEnv(t)
	shop := e.store("Lakshmi Traders", 500)
	for round := range 8 {
		cust := e.user()
		e.grant(cust, 30)
		claim, sale := e.claimRef(), e.sale()
		require.Equal(t, 20, e.hold(claim, cust, shop, 20, 50000).Allowed)
		_, err := e.capture(claim, cust, shop)
		require.NoError(t, err)

		var wg sync.WaitGroup
		start := make(chan struct{})
		var cbErr, revErr error
		var rev Reversed
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, cbErr = e.cashback(sale, cust, shop, 48000, 500)
		}()
		go func() {
			defer wg.Done()
			<-start
			rev, revErr = e.reverse(Reversal{Ref: sale, Spend: claim, CustomerID: cust, StoreID: shop})
		}()
		close(start)
		wg.Wait()
		require.NoError(t, revErr, "round %d", round)
		switch {
		case cbErr == nil:
			require.Equal(t, Reversed{Restored: 20, ClawedBack: 24}, rev, "round %d: cashback first", round)
		case errors.Is(cbErr, ErrReversed):
			require.Equal(t, Reversed{Restored: 20}, rev, "round %d: reversal first", round)
		default:
			require.NoError(t, cbErr, "round %d", round)
		}
		require.EqualValues(t, 30, e.balance(wallet(cust)), "round %d", round)
	}
	require.EqualValues(t, 0, e.balance(store(shop)))
	e.clean()
}
