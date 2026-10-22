// Package money does rupee arithmetic in integer paise (D-03). There are no
// floats anywhere: amounts are int64 paise, rates are int64 basis points
// (1 bp = 0.01 %, 10000 bp = 100 %), and every division names its rounding.
package money

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
)

// Paise is an amount of money: 100 paise = ₹1.
type Paise int64

// BP is a rate in basis points: 10000 = 100 %.
type BP int64

// Whole is 100 % in basis points.
const Whole BP = 10000

// ErrOverflow is returned when a result does not fit in int64.
var ErrOverflow = errors.New("money: overflow")

// ErrRounding is returned for an unknown rounding mode.
var ErrRounding = errors.New("money: unknown rounding")

// Rounding says how a fractional paisa is resolved.
type Rounding int

const (
	// Floor rounds toward minus infinity.
	Floor Rounding = iota + 1
	// Ceil rounds toward plus infinity.
	Ceil
	// HalfUp rounds to the nearest paisa; an exact half goes away from zero.
	HalfUp
	// HalfEven rounds to the nearest paisa; an exact half goes to the even neighbour.
	HalfEven
)

func (r Rounding) valid() bool { return r >= Floor && r <= HalfEven }

func (r Rounding) String() string {
	switch r {
	case Floor:
		return "floor"
	case Ceil:
		return "ceil"
	case HalfUp:
		return "half_up"
	case HalfEven:
		return "half_even"
	}
	return fmt.Sprintf("rounding(%d)", int(r))
}

// Rupees returns r rupees as paise.
func Rupees(r int64) (Paise, error) {
	if r > math.MaxInt64/100 || r < math.MinInt64/100 {
		return 0, ErrOverflow
	}
	return Paise(r * 100), nil
}

// String formats an amount as "₹12.34" (or "-₹12.34"), for logs and tests only;
// user-facing formatting is the client's job.
func (p Paise) String() string {
	sign := ""
	u := uint64(p) // #nosec G115 -- the bit pattern; fixed below when negative
	if p < 0 {
		sign = "-"
		u = uint64(-(p + 1)) + 1 // #nosec G115 -- safe for MinInt64
	}
	return fmt.Sprintf("%s₹%d.%02d", sign, u/100, u%100)
}

// Add returns a + b, or ErrOverflow.
func Add(a, b Paise) (Paise, error) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, ErrOverflow
	}
	return s, nil
}

// Sub returns a - b, or ErrOverflow.
func Sub(a, b Paise) (Paise, error) {
	d := a - b
	if (b > 0 && d > a) || (b < 0 && d < a) {
		return 0, ErrOverflow
	}
	return d, nil
}

// Sum adds amounts, or returns ErrOverflow.
func Sum(xs ...Paise) (Paise, error) {
	var total Paise
	for _, x := range xs {
		var err error
		if total, err = Add(total, x); err != nil {
			return 0, err
		}
	}
	return total, nil
}

// MulInt returns p * n (for example a unit price times a quantity), or ErrOverflow.
func MulInt(p Paise, n int64) (Paise, error) {
	if p == 0 || n == 0 {
		return 0, nil
	}
	r, ok := mulDiv(int64(p), n, 1, Floor)
	if !ok {
		return 0, ErrOverflow
	}
	return Paise(r), nil
}

// ApplyBP returns amount × rate / 10000 with the given rounding: the
// cashback on a bill, a percentage discount, a share of sales.
// The intermediate product is 128-bit, so it never overflows on the way.
func ApplyBP(amount Paise, rate BP, r Rounding) (Paise, error) {
	if !r.valid() {
		return 0, ErrRounding
	}
	v, ok := mulDiv(int64(amount), int64(rate), int64(Whole), r)
	if !ok {
		return 0, ErrOverflow
	}
	return Paise(v), nil
}

// Ratio returns num / den as basis points with the given rounding (for example
// a store's expiry share). den must be positive.
func Ratio(num, den int64, r Rounding) (BP, error) {
	if den <= 0 {
		return 0, errors.New("money: ratio denominator must be positive")
	}
	if !r.valid() {
		return 0, ErrRounding
	}
	v, ok := mulDiv(num, int64(Whole), den, r)
	if !ok {
		return 0, ErrOverflow
	}
	return BP(v), nil
}

// Allocate splits amount across weights in proportion, by the largest-remainder
// method, so the parts always add up to amount exactly (for example a discount
// spread over cart lines). Ties go to the earlier weight. Weights must be
// non-negative with a positive sum; amount must be non-negative.
func Allocate(amount Paise, weights []int64) ([]Paise, error) {
	if amount < 0 {
		return nil, errors.New("money: allocate needs a non-negative amount")
	}
	var total int64
	for _, w := range weights {
		if w < 0 {
			return nil, errors.New("money: allocate needs non-negative weights")
		}
		if total > math.MaxInt64-w {
			return nil, ErrOverflow
		}
		total += w
	}
	if total == 0 {
		return nil, errors.New("money: allocate needs a positive total weight")
	}
	parts := make([]Paise, len(weights))
	rems := make([]uint64, len(weights))
	var given int64
	for i, w := range weights {
		hi, lo := bits.Mul64(uint64(amount), uint64(w)) // #nosec G115 -- amount and w are non-negative here
		q, rem := bits.Div64(hi, lo, uint64(total))     // hi < total because w <= total
		parts[i] = Paise(q)                             // #nosec G115 -- q <= amount, which fits int64
		rems[i] = rem
		given += int64(q) // #nosec G115 -- q <= amount, which fits int64
	}
	left := int64(amount) - given // < len(weights)
	for ; left > 0; left-- {
		best := -1
		for i := range rems {
			if weights[i] == 0 { // #nosec G602 -- rems and weights have the same length
				continue
			}
			if best == -1 || rems[i] > rems[best] {
				best = i
			}
		}
		parts[best]++
		rems[best] = 0
	}
	return parts, nil
}

// mulDiv returns a*b/d rounded by r, computed in 128 bits. d must be positive.
// ok is false when the result does not fit in int64.
func mulDiv(a, b, d int64, r Rounding) (int64, bool) {
	if d <= 0 || !r.valid() {
		return 0, false
	}
	neg := (a < 0) != (b < 0)
	ua, ub := abs(a), abs(b)
	hi, lo := bits.Mul64(ua, ub)
	if hi >= uint64(d) {
		return 0, false
	}
	q, rem := bits.Div64(hi, lo, uint64(d))
	if rem != 0 {
		up := false // round the magnitude up?
		switch r {
		case Floor:
			up = neg
		case Ceil:
			up = !neg
		case HalfUp:
			up = rem >= uint64(d)-rem
		case HalfEven:
			twice := uint64(d) - rem
			switch {
			case rem > twice:
				up = true
			case rem == twice:
				up = q%2 == 1
			}
		default:
			return 0, false
		}
		if up {
			if q == math.MaxUint64 { // the increment itself would wrap to 0
				return 0, false
			}
			q++
		}
	}
	if neg {
		if q > uint64(math.MaxInt64)+1 {
			return 0, false
		}
		return int64(-q), true // #nosec G115 -- bounded above
	}
	if q > math.MaxInt64 {
		return 0, false
	}
	return int64(q), true
}

func abs(x int64) uint64 {
	if x < 0 {
		return uint64(-(x + 1)) + 1 // #nosec G115 -- x < 0, so -(x+1) >= 0; safe for MinInt64
	}
	return uint64(x)
}
