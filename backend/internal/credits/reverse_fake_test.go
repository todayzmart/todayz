package credits

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// The Fake behaves as reverse_db_test.go's service (D-132).
func TestFakeReverse(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 6, 30, 0, 0, time.UTC)
	newFake := func() (*Fake, *clock.Fake, *ids.Generator, ids.ID, ids.ID) {
		clk := clock.NewFake(start)
		f := NewFake(clk, defaultRules)
		g := ids.NewGenerator()
		cust, shop := g.New(start), g.New(start)
		f.CashbackBP[shop] = 500
		return f, clk, g, cust, shop
	}
	grant := func(t *testing.T, f *Fake, g *ids.Generator, c ids.ID, n int64) {
		t.Helper()
		_, err := f.Post(ctx, Posting{Kind: TxReferralReward, IdempotencyKey: "referral:" + g.New(start).String(),
			Legs: []Leg{{Account{Kind: PlatformMarketing}, -n}, {wallet(c), n}}})
		require.NoError(t, err)
	}
	ref := func(g *ids.Generator, ty RefType) Ref { return Ref{Type: ty, ID: g.New(start)} }

	t.Run("restores lots, due part expires", func(t *testing.T) {
		f, clk, g, cust, shop := newFake()
		grant(t, f, g, cust, 20)
		clk.Advance(30 * 24 * time.Hour)
		grant(t, f, g, cust, 20)
		expiry := time.Date(2027, 6, 2, 0, 0, 0, 0, clock.IST)
		clk.Set(expiry.Add(-time.Hour))
		claim, sale := ref(g, RefBillClaim), ref(g, RefPOSSale)
		a, err := f.Hold(ctx, HoldRequest{Ref: claim, CustomerID: cust, StoreID: shop, Credits: 30, EligiblePaise: 100000})
		require.NoError(t, err)
		require.Equal(t, 30, a.Allowed)
		done, err := f.Complete(ctx, Completion{Ref: claim, CustomerID: cust, StoreID: shop, CashbackBP: bp(0), RefuseReleased: true})
		require.NoError(t, err)
		require.Equal(t, Completed{Captured: 30}, done)
		done, err = f.Complete(ctx, Completion{Ref: sale, CustomerID: cust, StoreID: shop, EligiblePaise: 100000, CashbackBP: bp(500)})
		require.NoError(t, err)
		require.Equal(t, Completed{Cashback: 50}, done)

		clk.Set(expiry)
		r := Reversal{Ref: sale, Spend: claim, CustomerID: cust, StoreID: shop}
		got, err := f.Reverse(ctx, r)
		require.NoError(t, err)
		require.Equal(t, Reversed{Restored: 10, Expired: 20, ClawedBack: 50}, got)
		require.EqualValues(t, 20, f.Balance(wallet(cust)))
		require.EqualValues(t, 0, f.Balance(store(shop)))
		require.EqualValues(t, 20, f.Balance(expiredAcct))
		again, err := f.Reverse(ctx, r)
		require.NoError(t, err)
		require.Equal(t, got, again)
		require.EqualValues(t, 20, f.Balance(wallet(cust)))
		_, err = f.Complete(ctx, Completion{Ref: sale, CustomerID: cust, StoreID: shop, EligiblePaise: 100000, CashbackBP: bp(500)})
		require.ErrorIs(t, err, ErrReversed)
		require.NoError(t, f.Check())
	})

	t.Run("clawback shortfall", func(t *testing.T) {
		for _, tc := range []struct {
			spend int
			want  Reversed
		}{{30, Reversed{ClawedBack: 20, Shortfall: 30}}, {50, Reversed{Shortfall: 50}}} {
			f, _, g, cust, shop := newFake()
			other := g.New(start)
			sale := ref(g, RefPOSSale)
			_, err := f.Complete(ctx, Completion{Ref: sale, CustomerID: cust, StoreID: shop, EligiblePaise: 100000, CashbackBP: bp(500)})
			require.NoError(t, err)
			for left := tc.spend; left > 0; left -= 30 {
				o, n := ref(g, RefOrder), min(left, 30)
				_, err := f.Hold(ctx, HoldRequest{Ref: o, CustomerID: cust, StoreID: other, Credits: n, EligiblePaise: 100000})
				require.NoError(t, err)
				_, err = f.Complete(ctx, Completion{Ref: o, CustomerID: cust, StoreID: other, EligiblePaise: 100000})
				require.NoError(t, err)
			}
			r := Reversal{Ref: sale, CustomerID: cust, StoreID: shop}
			got, err := f.Reverse(ctx, r)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.EqualValues(t, -int64(tc.want.Shortfall), f.Balance(store(shop)))
			again, err := f.Reverse(ctx, r)
			require.NoError(t, err)
			require.Equal(t, tc.want, again)
			require.NoError(t, f.Check())
		}
	})

	t.Run("edges", func(t *testing.T) {
		f, _, g, cust, shop := newFake()
		grant(t, f, g, cust, 30)
		got, err := f.Reverse(ctx, Reversal{Ref: ref(g, RefPOSSale), Spend: ref(g, RefBillClaim), CustomerID: cust, StoreID: shop})
		require.NoError(t, err)
		require.Equal(t, Reversed{}, got)
		claim, sale := ref(g, RefBillClaim), ref(g, RefPOSSale)
		_, err = f.Hold(ctx, HoldRequest{Ref: claim, CustomerID: cust, StoreID: shop, Credits: 20, EligiblePaise: 50000})
		require.NoError(t, err)
		_, err = f.Reverse(ctx, Reversal{Ref: sale, Spend: claim, CustomerID: cust, StoreID: shop})
		require.ErrorIs(t, err, ErrHoldOpen)
		_, err = f.Hold(ctx, HoldRequest{Ref: ref(g, RefPOSSale), CustomerID: cust, StoreID: shop, Credits: 5, EligiblePaise: 50000})
		require.ErrorIs(t, err, ErrInvalid)

		// RefuseReleased on a released claim: refused, nothing posted.
		_, err = f.Release(ctx, claim)
		require.NoError(t, err)
		_, err = f.Complete(ctx, Completion{Ref: claim, CustomerID: cust, StoreID: shop, EligiblePaise: 50000, CashbackBP: bp(500), RefuseReleased: true})
		require.ErrorIs(t, err, ErrReleased)
		require.EqualValues(t, 30, f.Balance(wallet(cust)))
		require.EqualValues(t, 0, f.Balance(store(shop)))

		// The sale's cashback posts once however often it is asked for.
		first, err := f.Complete(ctx, Completion{Ref: sale, CustomerID: cust, StoreID: shop, EligiblePaise: 64000, CashbackBP: bp(500)})
		require.NoError(t, err)
		require.Equal(t, Completed{Cashback: 32}, first)
		again, err := f.Complete(ctx, Completion{Ref: sale, CustomerID: cust, StoreID: shop, EligiblePaise: 64000, CashbackBP: bp(500)})
		require.NoError(t, err)
		require.Equal(t, first, again)
		require.EqualValues(t, 62, f.Balance(wallet(cust)))
		require.NoError(t, f.Check())
	})
}
