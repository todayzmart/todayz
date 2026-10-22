package money

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyBP(t *testing.T) {
	cases := []struct {
		name   string
		amount Paise
		rate   BP
		r      Rounding
		want   Paise
	}{
		{"exact", 10000, 500, Floor, 500},
		{"E2E-02 cashback: 5% of ₹270 in whole paise", 27000, 500, Floor, 1350},
		{"E2E-02 cashback in credits: 5% of 270 credits floors to 13", 270, 500, Floor, 13},
		{"half floors", 270, 500, Floor, 13},
		{"half ceils", 270, 500, Ceil, 14},
		{"half up", 270, 500, HalfUp, 14},
		{"half even to even (13.5 -> 14)", 270, 500, HalfEven, 14},
		{"half even to even (12.5 -> 12)", 250, 500, HalfEven, 12},
		{"below half", 1, 4999, HalfUp, 0},
		{"above half", 1, 5001, HalfUp, 1},
		{"exact half up", 1, 5000, HalfUp, 1},
		{"exact half even down", 1, 5000, HalfEven, 0},
		{"zero rate", 99999, 0, Ceil, 0},
		{"100 percent", 12345, Whole, Floor, 12345},
		{"negative floors away from zero", -270, 500, Floor, -14},
		{"negative ceils toward zero", -270, 500, Ceil, -13},
		{"negative half up away from zero", -270, 500, HalfUp, -14},
		{"negative half even", -250, 500, HalfEven, -12},
		{"tiny ceil", 1, 1, Ceil, 1},
		{"tiny floor", 1, 1, Floor, 0},
		{"no 64-bit overflow in the intermediate", Paise(math.MaxInt64 / 2), 2000, Floor, Paise(math.MaxInt64 / 2 / 5)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ApplyBP(c.amount, c.rate, c.r)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestApplyBPOverflowAndBadRounding(t *testing.T) {
	_, err := ApplyBP(Paise(math.MaxInt64), 20000, Floor)
	require.ErrorIs(t, err, ErrOverflow)
	_, err = ApplyBP(100, 100, Rounding(99))
	require.ErrorIs(t, err, ErrRounding)
	_, err = Ratio(1, 2, Rounding(0))
	require.ErrorIs(t, err, ErrRounding)
}

func TestAddSubOverflow(t *testing.T) {
	s, err := Add(150, 250)
	require.NoError(t, err)
	require.Equal(t, Paise(400), s)

	_, err = Add(math.MaxInt64, 1)
	require.ErrorIs(t, err, ErrOverflow)
	_, err = Add(math.MinInt64, -1)
	require.ErrorIs(t, err, ErrOverflow)

	d, err := Sub(100, 250)
	require.NoError(t, err)
	require.Equal(t, Paise(-150), d)
	_, err = Sub(math.MinInt64, 1)
	require.ErrorIs(t, err, ErrOverflow)
	_, err = Sub(math.MaxInt64, -1)
	require.ErrorIs(t, err, ErrOverflow)

	total, err := Sum(100, 200, 300)
	require.NoError(t, err)
	require.Equal(t, Paise(600), total)
	_, err = Sum(math.MaxInt64, 1)
	require.ErrorIs(t, err, ErrOverflow)
}

func TestMulIntAndRupees(t *testing.T) {
	p, err := MulInt(4999, 3)
	require.NoError(t, err)
	require.Equal(t, Paise(14997), p)
	p, err = MulInt(4999, 0)
	require.NoError(t, err)
	require.Equal(t, Paise(0), p)
	_, err = MulInt(math.MaxInt64, 2)
	require.ErrorIs(t, err, ErrOverflow)

	r, err := Rupees(300)
	require.NoError(t, err)
	require.Equal(t, Paise(30000), r)
	_, err = Rupees(math.MaxInt64)
	require.ErrorIs(t, err, ErrOverflow)
}

func TestRatio(t *testing.T) {
	cases := []struct {
		num, den int64
		r        Rounding
		want     BP
	}{
		{3, 10, Floor, 3000},
		{1, 3, Floor, 3333},
		{1, 3, Ceil, 3334},
		{2, 3, HalfUp, 6667},
		{0, 7, HalfUp, 0},
		{7, 7, Floor, Whole},
	}
	for _, c := range cases {
		got, err := Ratio(c.num, c.den, c.r)
		require.NoError(t, err)
		require.Equal(t, c.want, got)
	}
	_, err := Ratio(1, 0, Floor)
	require.Error(t, err)
}

func TestAllocate(t *testing.T) {
	cases := []struct {
		name    string
		amount  Paise
		weights []int64
		want    []Paise
	}{
		{"even", 300, []int64{1, 1, 1}, []Paise{100, 100, 100}},
		{"remainder to the earliest tie", 100, []int64{1, 1, 1}, []Paise{34, 33, 33}},
		{"largest remainder wins", 100, []int64{1, 2}, []Paise{33, 67}},
		{"zero weight gets nothing", 10, []int64{0, 3, 0, 1}, []Paise{0, 8, 0, 2}},
		{"zero amount", 0, []int64{5, 5}, []Paise{0, 0}},
		{"price-weighted discount", 1000, []int64{4999, 2500, 1}, []Paise{667, 333, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Allocate(c.amount, c.weights)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
			total, err := Sum(got...)
			require.NoError(t, err)
			require.Equal(t, c.amount, total, "parts always add up")
		})
	}
	_, err := Allocate(10, []int64{0, 0})
	require.Error(t, err)
	_, err = Allocate(10, []int64{-1, 2})
	require.Error(t, err)
	_, err = Allocate(-10, []int64{1})
	require.Error(t, err)
}

func TestString(t *testing.T) {
	require.Equal(t, "₹12.34", Paise(1234).String())
	require.Equal(t, "-₹0.05", Paise(-5).String())
	require.Equal(t, "₹0.00", Paise(0).String())
	require.NotPanics(t, func() { _ = Paise(math.MinInt64).String() })
	require.Equal(t, "half_even", HalfEven.String())
}

// TestMulDivRoundingIncrementOverflow: a quotient of 2^64-1 that rounds up
// would wrap to 0; it must report overflow. 253921 * 145295143558111 is
// 2^65-1, so halving it gives 2^64-1 remainder 1.
func TestMulDivRoundingIncrementOverflow(t *testing.T) {
	for _, r := range []Rounding{HalfUp, Ceil} {
		_, ok := mulDiv(253921, 145295143558111, 2, r)
		require.False(t, ok, r)
		_, ok = mulDiv(-253921, 145295143558111, 2, r)
		require.False(t, ok, r)
	}
	_, ok := mulDiv(253921, 145295143558111, 2, Floor)
	require.False(t, ok, "2^64-1 does not fit an int64 either")
}
