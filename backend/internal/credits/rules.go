package credits

import (
	"sort"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// The pure rules (01 §7). The database service and the Fake both decide
// with these, so the property tests on the Fake test the same arithmetic.

// floorPaise is the least an eligible amount may be after credits: ₹1 (01
// §7 "can never go below ₹1"; a fixed rule, not a setting).
const floorPaise = 100

// paisePerCredit: 1 credit = ₹1 (01 §7, fixed).
const paisePerCredit = 100

// allow decides how many of requested credits a checkout may use, given the
// customer's available balance and the cap usage. The limits are checked in
// order: the credit minimum, the store's daily cap, the customer's monthly
// cap, then the per-order max and ₹1 floor against the balance. A blocking
// limit (minimum, caps) is reported even when nothing was requested, so a
// quote can say why before the customer asks (D-76).
func allow(r Rules, requested int, eligiblePaise int64, available int, u usage) Allowance {
	a := Allowance{Available: max(available, 0)}
	switch {
	case eligiblePaise < r.MinOrderPaise:
		a.Limit = LimitBelowMinimum
		return a
	case u.StoreDay >= r.StoreRedemptionsPerDay:
		a.Limit = LimitStoreCap
		return a
	case u.CustomerMonth >= r.OrdersPerMonth:
		a.Limit = LimitMonthlyCap
		return a
	}
	ceiling := max(min(int64(r.MaxPerOrder), (eligiblePaise-floorPaise)/paisePerCredit), 0)
	a.Max = int(min(ceiling, int64(a.Available)))
	if requested <= 0 {
		return a
	}
	a.Allowed = min(requested, a.Max)
	if a.Allowed < requested {
		if int64(a.Available) < min(int64(requested), ceiling) {
			a.Limit = LimitNoBalance
		} else {
			a.Limit = LimitMaxExceeded
		}
	}
	return a
}

// keep is how many of open held credits a re-cap against eligiblePaise
// keeps (D-28): the per-order max and the ₹1 floor, nothing under the
// credit minimum. The caps are not counted again: the hold already counts.
func keep(r Rules, open int, eligiblePaise int64) int {
	if eligiblePaise < r.MinOrderPaise {
		return 0
	}
	ceiling := max(min(int64(r.MaxPerOrder), (eligiblePaise-floorPaise)/paisePerCredit), 0)
	return int(min(int64(open), ceiling))
}

// cashback is floor(base × bp / 1 000 000) credits (01 §7): base is in
// paise, bp in basis points, so paise × bp / 10 000 is paise of cashback and
// a further / 100 is credits.
func cashback(basePaise int64, bp int) int {
	if basePaise <= 0 || bp <= 0 {
		return 0
	}
	return int(basePaise * int64(bp) / 1_000_000)
}

// expiresAt is when a lot earned at earned expires: usable through IST day
// X+N, expiring at 00:00 IST on day X+N+1 (01 §7). IST has no DST, so days
// are 24 h.
func expiresAt(earned time.Time, days int) time.Time {
	return clock.StartOfDayIST(earned).Add(time.Duration(days+1) * 24 * time.Hour)
}

// due reports whether a lot expiring at exp has expired at now.
func due(exp, now time.Time) bool { return !now.Before(exp) }

// lotState is a lot as the allocation rules see it.
type lotState struct {
	ID        ids.ID
	Remaining int
	ExpiresAt time.Time
}

// portion is part of one lot.
type portion struct {
	Lot       ids.ID
	N         int
	ExpiresAt time.Time
}

func idLess(a, b ids.ID) bool { return a.Compare(b) < 0 }

// takeFIFO takes n credits from the lots' remaining, earliest expires_at
// first (then id), skipping lots that are due. ok is false when the lots
// hold fewer than n spendable credits; then nothing is taken.
func takeFIFO(lots []lotState, n int, now time.Time) (out []portion, ok bool) {
	ls := append([]lotState(nil), lots...)
	sort.Slice(ls, func(i, j int) bool {
		if !ls[i].ExpiresAt.Equal(ls[j].ExpiresAt) {
			return ls[i].ExpiresAt.Before(ls[j].ExpiresAt)
		}
		return idLess(ls[i].ID, ls[j].ID)
	})
	for _, l := range ls {
		if n == 0 {
			break
		}
		if l.Remaining <= 0 || due(l.ExpiresAt, now) {
			continue
		}
		k := min(l.Remaining, n)
		out = append(out, portion{Lot: l.ID, N: k, ExpiresAt: l.ExpiresAt})
		n -= k
	}
	if n > 0 {
		return nil, false
	}
	return out, true
}

// takeLatest picks n credits of a hold's held portions to release on a
// re-cap: latest expires_at first (then id, descending), so the credits
// that stay held are the oldest. ok is false when the portions hold fewer.
func takeLatest(held []portion, n int) (out []portion, ok bool) {
	ps := append([]portion(nil), held...)
	sort.Slice(ps, func(i, j int) bool {
		if !ps[i].ExpiresAt.Equal(ps[j].ExpiresAt) {
			return ps[i].ExpiresAt.After(ps[j].ExpiresAt)
		}
		return idLess(ps[j].Lot, ps[i].Lot)
	})
	for _, p := range ps {
		if n == 0 {
			break
		}
		if p.N <= 0 {
			continue
		}
		k := min(p.N, n)
		out = append(out, portion{Lot: p.Lot, N: k, ExpiresAt: p.ExpiresAt})
		n -= k
	}
	if n > 0 {
		return nil, false
	}
	return out, true
}

// splitDue splits released portions into those that go back to their lot
// and those whose lot is due, which expire at once (D-27).
func splitDue(ps []portion, now time.Time) (back, expired []portion, backN, expiredN int) {
	for _, p := range ps {
		if due(p.ExpiresAt, now) {
			expired = append(expired, p)
			expiredN += p.N
		} else {
			back = append(back, p)
			backN += p.N
		}
	}
	return back, expired, backN, expiredN
}

func sumPortions(ps []portion) int {
	n := 0
	for _, p := range ps {
		n += p.N
	}
	return n
}
