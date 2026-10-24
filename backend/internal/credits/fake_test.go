package credits

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// propertyChecks is CR-13's 10k runs; -short runs fewer.
func propertyChecks(t *testing.T, n int) {
	t.Helper()
	if testing.Short() {
		n = max(n/20, 50)
	}
	f := flag.Lookup("rapid.checks")
	require.NotNil(t, f)
	old := f.Value.String()
	require.NoError(t, f.Value.Set(fmt.Sprint(n)))
	t.Cleanup(func() { _ = f.Value.Set(old) })
}

// ledgerMachine drives one ledger with random operations. Holder and
// Poster are the interfaces under test; check verifies the invariants.
type ledgerMachine struct {
	ctx       context.Context
	h         Holder
	p         Poster
	clk       *clock.Fake
	rules     Rules
	expire    func(context.Context) error
	forfeit   func(context.Context, ids.ID) error
	check     func() error
	customers []ids.ID
	stores    []ids.ID
	refs      []Ref
	keys      []string
	holdTried map[Ref]bool
}

func newMachine(t *rapid.T, gen *ids.Generator, now time.Time) []ids.ID {
	n := rapid.IntRange(1, 3).Draw(t, "n")
	out := make([]ids.ID, n)
	for i := range out {
		out[i] = gen.New(now)
	}
	return out
}

func (m *ledgerMachine) actions() map[string]func(*rapid.T) {
	return map[string]func(*rapid.T){
		"credit": func(t *rapid.T) {
			c := rapid.SampledFrom(m.customers).Draw(t, "customer")
			n := int64(rapid.IntRange(1, 150).Draw(t, "credits"))
			key := rapid.SampledFrom(m.keys).Draw(t, "key")
			_, err := m.p.Post(m.ctx, Posting{Kind: TxReferralReward, IdempotencyKey: "credit:" + key + c.String(),
				Legs: []Leg{{Account{Kind: PlatformMarketing}, -n}, {Account{Kind: CustomerWallet, Owner: c}, n}}})
			if err != nil {
				t.Fatalf("credit: %v", err)
			}
		},
		"debit": func(t *rapid.T) {
			c := rapid.SampledFrom(m.customers).Draw(t, "customer")
			n := int64(rapid.IntRange(1, 80).Draw(t, "credits"))
			key := rapid.SampledFrom(m.keys).Draw(t, "key")
			_, err := m.p.Post(m.ctx, Posting{Kind: TxAdminAdjustment, IdempotencyKey: "debit:" + key + c.String(),
				Legs: []Leg{{Account{Kind: CustomerWallet, Owner: c}, -n}, {Account{Kind: PlatformAdjustments}, n}}})
			if err != nil && err != ErrInsufficient {
				t.Fatalf("debit: %v", err)
			}
		},
		"hold": func(t *rapid.T) {
			req := HoldRequest{
				Ref:        rapid.SampledFrom(m.refs).Draw(t, "ref"),
				CustomerID: rapid.SampledFrom(m.customers).Draw(t, "customer"),
				StoreID:    rapid.SampledFrom(m.stores).Draw(t, "store"),
				Credits:    rapid.IntRange(1, 40).Draw(t, "credits"),
				// Around the minimum, and up to ₹600.
				EligiblePaise: int64(rapid.IntRange(9000, 60000).Draw(t, "eligible")),
			}
			first := !m.holdTried[req.Ref]
			m.holdTried[req.Ref] = true
			a, err := m.h.Hold(m.ctx, req)
			if err != nil {
				t.Fatalf("hold: %v", err)
			}
			if first && a.Allowed > 0 && a.Limit == LimitNone {
				// Invariant 5 for a fresh hold.
				if req.EligiblePaise < m.rules.MinOrderPaise || req.Credits > m.rules.MaxPerOrder ||
					int64(req.Credits)*100 > req.EligiblePaise-100 {
					t.Fatalf("invariant 5: held %d on %d paise", req.Credits, req.EligiblePaise)
				}
			}
		},
		"recap": func(t *rapid.T) {
			eligible := int64(rapid.IntRange(9000, 60000).Draw(t, "eligible"))
			n, err := m.h.Recap(m.ctx, rapid.SampledFrom(m.refs).Draw(t, "ref"), eligible)
			if err != nil {
				t.Fatalf("recap: %v", err)
			}
			if n > 0 && (eligible < m.rules.MinOrderPaise || n > m.rules.MaxPerOrder || int64(n)*100 > eligible-100) {
				t.Fatalf("invariant 5: kept %d on %d paise", n, eligible)
			}
		},
		"release": func(t *rapid.T) {
			if _, err := m.h.Release(m.ctx, rapid.SampledFrom(m.refs).Draw(t, "ref")); err != nil {
				t.Fatalf("release: %v", err)
			}
		},
		"complete": func(t *rapid.T) {
			c := Completion{
				Ref:           rapid.SampledFrom(m.refs).Draw(t, "ref"),
				CustomerID:    rapid.SampledFrom(m.customers).Draw(t, "customer"),
				StoreID:       rapid.SampledFrom(m.stores).Draw(t, "store"),
				EligiblePaise: int64(rapid.IntRange(10000, 60000).Draw(t, "eligible")),
			}
			// A completion for another customer's or store's hold is refused.
			if _, err := m.h.Complete(m.ctx, c); err != nil && !errors.Is(err, ErrInvalid) {
				t.Fatalf("complete: %v", err)
			}
		},
		"expire": func(t *rapid.T) {
			if err := m.expire(m.ctx); err != nil {
				t.Fatalf("expire: %v", err)
			}
		},
		"advance": func(t *rapid.T) {
			m.clk.Advance(time.Duration(rapid.IntRange(1, 24*20).Draw(t, "hours")) * time.Hour)
		},
		"forfeit": func(t *rapid.T) {
			if rapid.IntRange(0, 9).Draw(t, "rare") != 0 {
				return
			}
			// Held credits block the deletion (an order is still open).
			if err := m.forfeit(m.ctx, rapid.SampledFrom(m.customers).Draw(t, "customer")); err != nil && !errors.Is(err, ErrHeld) {
				t.Fatalf("forfeit: %v", err)
			}
		},
		"": func(t *rapid.T) {
			if err := m.check(); err != nil {
				t.Fatal(err)
			}
		},
	}
}

func newLedgerMachine(t *rapid.T, ctx context.Context, clk *clock.Fake, rules Rules) *ledgerMachine {
	gen := ids.NewGenerator()
	now := clk.Now()
	m := &ledgerMachine{ctx: ctx, clk: clk, rules: rules, holdTried: map[Ref]bool{}}
	m.customers = newMachine(t, gen, now)
	m.stores = newMachine(t, gen, now)
	for range rapid.IntRange(2, 8).Draw(t, "refs") {
		m.refs = append(m.refs, Ref{Type: rapid.SampledFrom([]RefType{RefOrder, RefBillClaim}).Draw(t, "refType"), ID: gen.New(now)})
	}
	m.keys = []string{"a", "b", "c", "d", "e", "f"}
	return m
}

// CR-13: random operation sequences on the Fake keep invariants 1–8.
// Short expiry and low caps make expiry, caps and holds on expired lots
// common.
func TestFakeInvariantsProperty(t *testing.T) {
	propertyChecks(t, 10000)
	rapid.Check(t, func(t *rapid.T) {
		clk := clock.NewFake(time.Date(2026, 1, 31, 18, 0, 0, 0, time.UTC))
		rules := Rules{
			MaxPerOrder: rapid.IntRange(5, 40).Draw(t, "max"), MinOrderPaise: 10000,
			ExpiryDays:     rapid.IntRange(0, 40).Draw(t, "expiry"),
			OrdersPerMonth: rapid.IntRange(1, 6).Draw(t, "month"), StoreRedemptionsPerDay: rapid.IntRange(1, 6).Draw(t, "day"),
			ReminderDays: 7,
		}
		f := NewFake(clk, rules)
		m := newLedgerMachine(t, context.Background(), clk, rules)
		for _, s := range m.stores {
			f.CashbackBP[s] = rapid.SampledFrom([]int{0, 50, 500, 2000}).Draw(t, "bp")
		}
		m.h, m.p, m.expire, m.forfeit, m.check = f, f, f.Expire, f.OnAccountDelete, f.Check
		t.Repeat(m.actions())
	})
}

// The Fake on its own: hold, release to the same lots, capture and
// cashback, forfeiture.
func TestFakeScenarios(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 6, 30, 0, 0, time.UTC)
	clk := clock.NewFake(now)
	f := NewFake(clk, defaultRules)
	g := ids.NewGenerator()
	cust, shop := g.New(now), g.New(now)
	f.CashbackBP[shop] = 500
	_, err := f.Post(ctx, Posting{Kind: TxReferralReward, IdempotencyKey: "r1",
		Legs: []Leg{{Account{Kind: PlatformMarketing}, -100}, {wallet(cust), 100}}})
	require.NoError(t, err)

	o1 := Ref{Type: RefOrder, ID: g.New(now)}
	a, err := f.Hold(ctx, HoldRequest{Ref: o1, CustomerID: cust, StoreID: shop, Credits: 30, EligiblePaise: 25000})
	require.NoError(t, err)
	require.Equal(t, 30, a.Allowed)
	require.EqualValues(t, 70, f.Balance(wallet(cust)))
	require.EqualValues(t, 30, f.Balance(held(cust)))

	n, err := f.Release(ctx, o1)
	require.NoError(t, err)
	require.Equal(t, 30, n)
	require.EqualValues(t, 100, f.Balance(wallet(cust)))
	require.Len(t, f.lots, 1, "CR-25: no new lot on release")

	o2 := Ref{Type: RefOrder, ID: g.New(now)}
	_, err = f.Hold(ctx, HoldRequest{Ref: o2, CustomerID: cust, StoreID: shop, Credits: 30, EligiblePaise: 30000})
	require.NoError(t, err)
	done, err := f.Complete(ctx, Completion{Ref: o2, CustomerID: cust, StoreID: shop, EligiblePaise: 30000})
	require.NoError(t, err)
	require.Equal(t, Completed{Captured: 30, Cashback: 13}, done, "CR-23")
	require.EqualValues(t, 30-13, f.Balance(store(shop)))
	again, err := f.Complete(ctx, Completion{Ref: o2, CustomerID: cust, StoreID: shop, EligiblePaise: 30000})
	require.NoError(t, err)
	require.Equal(t, done, again)

	require.NoError(t, f.OnAccountDelete(ctx, cust))
	require.NoError(t, f.OnAccountDelete(ctx, cust))
	require.EqualValues(t, 0, f.Balance(wallet(cust)))
	require.EqualValues(t, 83, f.Balance(expiredAcct))
	require.NoError(t, f.Check())
}
