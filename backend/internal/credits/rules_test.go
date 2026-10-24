package credits

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// defaultRules are the 01 §17 defaults.
var defaultRules = Rules{MaxPerOrder: 30, MinOrderPaise: 10000, ExpiryDays: 365,
	OrdersPerMonth: 10, StoreRedemptionsPerDay: 200, ReminderDays: 7}

// CR-02, CR-03, CR-10, CR-11, CR-21, CR-22: the allowance table.
func TestAllow(t *testing.T) {
	big := Rules{MaxPerOrder: 1000, MinOrderPaise: 10000, OrdersPerMonth: 10, StoreRedemptionsPerDay: 200}
	for _, tc := range []struct {
		name      string
		r         Rules
		requested int
		eligible  int64
		available int
		u         usage
		want      Allowance
	}{
		{"CR-01 30 of 100 on ₹250", defaultRules, 30, 25000, 100, usage{}, Allowance{Allowed: 30, Max: 30, Available: 100}},
		{"CR-02 31 is over the max", defaultRules, 31, 25000, 100, usage{}, Allowance{Allowed: 30, Max: 30, Available: 100, Limit: LimitMaxExceeded}},
		{"CR-02 ₹99 is below the minimum", defaultRules, 10, 9900, 100, usage{}, Allowance{Available: 100, Limit: LimitBelowMinimum}},
		{"CR-03 ₹100 with 30", defaultRules, 30, 10000, 100, usage{}, Allowance{Allowed: 30, Max: 30, Available: 100}},
		{"CR-03 ₹25 is below the minimum", defaultRules, 5, 2500, 100, usage{}, Allowance{Available: 100, Limit: LimitBelowMinimum}},
		{"CR-21 9999 paise", defaultRules, 1, 9999, 100, usage{}, Allowance{Available: 100, Limit: LimitBelowMinimum}},
		{"CR-21 10000 paise", defaultRules, 1, 10000, 100, usage{}, Allowance{Allowed: 1, Max: 30, Available: 100}},
		{"CR-21 10001 paise", defaultRules, 1, 10001, 100, usage{}, Allowance{Allowed: 1, Max: 30, Available: 100}},
		{"CR-21 ₹120 less a ₹21 offer", defaultRules, 1, 9900, 100, usage{}, Allowance{Available: 100, Limit: LimitBelowMinimum}},
		{"CR-22 max above the items keeps ₹1", big, 500, 15000, 1000, usage{}, Allowance{Allowed: 149, Max: 149, Available: 1000, Limit: LimitMaxExceeded}},
		{"CR-22 floor exactly", big, 149, 15000, 1000, usage{}, Allowance{Allowed: 149, Max: 149, Available: 1000}},
		{"balance short", defaultRules, 30, 25000, 12, usage{}, Allowance{Allowed: 12, Max: 12, Available: 12, Limit: LimitNoBalance}},
		{"no balance", defaultRules, 30, 25000, 0, usage{}, Allowance{Available: 0, Limit: LimitNoBalance}},
		{"nothing asked", defaultRules, 0, 25000, 100, usage{}, Allowance{Max: 30, Available: 100}},
		{"CR-10 monthly cap", defaultRules, 30, 25000, 100, usage{CustomerMonth: 10}, Allowance{Available: 100, Limit: LimitMonthlyCap}},
		{"CR-10 one under the monthly cap", defaultRules, 30, 25000, 100, usage{CustomerMonth: 9}, Allowance{Allowed: 30, Max: 30, Available: 100}},
		{"CR-11 store cap", defaultRules, 30, 25000, 100, usage{StoreDay: 200}, Allowance{Available: 100, Limit: LimitStoreCap}},
		{"CR-11 store cap shown with nothing asked", defaultRules, 0, 25000, 100, usage{StoreDay: 200}, Allowance{Available: 100, Limit: LimitStoreCap}},
		{"store cap before monthly cap", defaultRules, 30, 25000, 100, usage{StoreDay: 200, CustomerMonth: 10}, Allowance{Available: 100, Limit: LimitStoreCap}},
		{"minimum before caps", defaultRules, 30, 9999, 100, usage{StoreDay: 200}, Allowance{Available: 100, Limit: LimitBelowMinimum}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, allow(tc.r, tc.requested, tc.eligible, tc.available, tc.u))
		})
	}
}

// CR-22: whatever the settings, credits never take the eligible amount
// below ₹1, so the delivery fee is always paid in money.
func TestAllowNeverBelowOneRupee(t *testing.T) {
	for _, maxPer := range []int{1, 30, 99, 100, 5000} {
		for eligible := int64(10000); eligible <= 60000; eligible += 37 {
			a := allow(Rules{MaxPerOrder: maxPer, MinOrderPaise: 10000, OrdersPerMonth: 1, StoreRedemptionsPerDay: 1}, 1<<20, eligible, 1<<20, usage{})
			require.LessOrEqual(t, int64(a.Allowed)*100, eligible-100)
			require.LessOrEqual(t, a.Allowed, maxPer)
		}
	}
}

// CR-06, CR-07, CR-23: cashback is floored, 0 posts nothing.
func TestCashback(t *testing.T) {
	assert.Equal(t, 24, cashback(48700, 500), "CR-06: 5% of ₹487")
	assert.Equal(t, 0, cashback(48700, 0), "CR-07")
	assert.Equal(t, 13, cashback(30000-30*100, 500), "CR-23: (300 − 30) × 5%")
	assert.Equal(t, 0, cashback(1999, 500), "under one credit")
	assert.Equal(t, 0, cashback(-5, 500))
	assert.Equal(t, 400, cashback(200000, 2000), "20% of ₹2000")
}

// 01 §7: earned on day X, usable through X+N, expires 00:00 IST on X+N+1.
func TestExpiresAt(t *testing.T) {
	earned := time.Date(2026, 3, 10, 23, 59, 0, 0, clock.IST)
	assert.Equal(t, time.Date(2027, 3, 11, 0, 0, 0, 0, clock.IST).UTC(), expiresAt(earned, 365))
	early := time.Date(2026, 3, 10, 0, 0, 0, 0, clock.IST)
	assert.Equal(t, time.Date(2026, 3, 18, 0, 0, 0, 0, clock.IST).UTC(), expiresAt(early, 7))
	// 18:29 UTC on the 10th is still the 10th in IST; 18:30 is the 11th.
	assert.Equal(t, expiresAt(time.Date(2026, 3, 10, 18, 29, 0, 0, time.UTC), 1), time.Date(2026, 3, 12, 0, 0, 0, 0, clock.IST).UTC())
	assert.Equal(t, expiresAt(time.Date(2026, 3, 10, 18, 30, 0, 0, time.UTC), 1), time.Date(2026, 3, 13, 0, 0, 0, 0, clock.IST).UTC())
}

// CR-09: spending takes the oldest lots first and skips due lots.
func TestTakeFIFO(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	g := ids.NewGenerator()
	a, b, c, d := g.New(now), g.New(now), g.New(now), g.New(now)
	lots := []lotState{
		{ID: c, Remaining: 10, ExpiresAt: now.Add(72 * time.Hour)},
		{ID: a, Remaining: 20, ExpiresAt: now.Add(24 * time.Hour)},
		{ID: d, Remaining: 50, ExpiresAt: now.Add(-time.Hour)}, // due: never spent
		{ID: b, Remaining: 5, ExpiresAt: now.Add(24 * time.Hour)},
	}
	ps, ok := takeFIFO(lots, 28, now)
	require.True(t, ok)
	assert.Equal(t, []portion{
		{Lot: a, N: 20, ExpiresAt: now.Add(24 * time.Hour)},
		{Lot: b, N: 5, ExpiresAt: now.Add(24 * time.Hour)},
		{Lot: c, N: 3, ExpiresAt: now.Add(72 * time.Hour)},
	}, ps)
	_, ok = takeFIFO(lots, 36, now)
	assert.False(t, ok, "the due lot does not count")
}

// D-28: a re-cap releases the latest-expiring portions first.
func TestTakeLatest(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	g := ids.NewGenerator()
	l1, l2 := g.New(now), g.New(now)
	held := []portion{{Lot: l1, N: 20, ExpiresAt: now.Add(time.Hour)}, {Lot: l2, N: 10, ExpiresAt: now.Add(48 * time.Hour)}}
	ps, ok := takeLatest(held, 12)
	require.True(t, ok)
	assert.Equal(t, []portion{{Lot: l2, N: 10, ExpiresAt: now.Add(48 * time.Hour)}, {Lot: l1, N: 2, ExpiresAt: now.Add(time.Hour)}}, ps)
	back, expired, bn, en := splitDue(ps, now.Add(2*time.Hour))
	assert.Equal(t, 10, bn)
	assert.Equal(t, 2, en)
	assert.Len(t, back, 1)
	assert.Len(t, expired, 1)
}
