package payouts

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	api "github.com/todayz/backend/gen/api/admin-payouts"
	"github.com/todayz/backend/gen/sqlc/payoutsdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

func TestClosePeriodIsTheISTMonthBefore(t *testing.T) {
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		// The schedule's tick, 1 July 00:30 IST, is still 30 June in UTC.
		{time.Date(2026, 7, 1, 0, 30, 0, 0, clock.IST), "2026-06"},
		{time.Date(2026, 6, 30, 19, 0, 0, 0, time.UTC), "2026-06"}, // 1 July 00:30 IST
		{time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC), "2026-05"}, // 30 June 23:30 IST
		{time.Date(2026, 1, 1, 0, 30, 0, 0, clock.IST), "2025-12"},
		{time.Date(2026, 3, 31, 12, 0, 0, 0, clock.IST), "2026-02"},
		// The run's period start (the UTC instant of 1 July 00:00 IST).
		{clock.StartOfMonthIST(time.Date(2026, 7, 1, 0, 30, 0, 0, clock.IST)), "2026-06"},
	} {
		require.Equal(t, c.want, periodKey(periodDate(ClosePeriod(c.at))), c.at)
	}
}

func TestMonthBoundsAreIST(t *testing.T) {
	p, err := parsePeriod("x", "2026-06")
	require.NoError(t, err)
	from, to := monthBounds(p)
	require.Equal(t, time.Date(2026, 5, 31, 18, 30, 0, 0, time.UTC), from)
	require.Equal(t, time.Date(2026, 6, 30, 18, 30, 0, 0, time.UTC), to)
	for _, bad := range []string{"", "June", "2026-13", "2026-6-1", "26-06"} {
		_, err := parsePeriod("query.period", bad)
		require.Error(t, err, bad)
	}
}

func TestINR(t *testing.T) {
	for p, want := range map[int]string{0: "0.00", 5: "0.05", 50: "0.50", 100: "1.00", 267000: "2670.00", -3000: "-30.00", -7: "-0.07", 123456: "1234.56"} {
		require.Equal(t, want, inr(p), p)
	}
}

func TestSafeCell(t *testing.T) {
	for in, want := range map[string]string{
		"":         "",
		"Lakshmi":  "Lakshmi",
		"=SUM(A1)": "'=SUM(A1)",
		"+91":      "'+91",
		"-5":       "'-5",
		"@cmd":     "'@cmd",
		"\tx":      "'\tx",
		"\rx":      "'\rx",
		"a=b":      "a=b",
		"'already": "'already",
		"x =y":     "x =y",
	} {
		require.Equal(t, want, safeCell(in), in)
	}
}

// A negative amount in a numeric column stays a number; the same text in a
// text column is escaped.
func TestStatementCSVKeepsNumbersRaw(t *testing.T) {
	b := statementCSV("2026-06", []api.StatementStore{{
		StoreId: "s1", StoreName: "-Minus Mart", Entries: 2,
		Totals: api.StatementTotals{ReversalsPaise: -1100, ClosingPaise: -1100},
	}})
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, []string{"2026-06", "s1", "'-Minus Mart", "0.00", "0.00", "0.00", "0.00", "-11.00", "0.00", "0.00", "-11.00", "2"}, rows[1])
}

func TestHoldReasonOrder(t *testing.T) {
	require.Equal(t, "", causes{Checked: true}.reason())
	require.False(t, causes{Checked: true}.open())
	// D-274: an unchecked shop is held, whatever its KYC says.
	require.Equal(t, holdUnchecked, causes{}.reason())
	require.Equal(t, holdUnchecked, causes{DetailsMissing: true, RiskFlag: true}.reason())
	require.Equal(t, holdDetails, causes{Checked: true, DetailsMissing: true, RiskFlag: true}.reason())
	require.Equal(t, holdRisk, causes{Checked: true, RiskFlag: true}.reason())
	require.Equal(t, holdSuspended, causes{Suspended: true, RiskFlag: true}.reason())
	require.True(t, causes{Checked: true, RiskFlag: true}.open())
}

func TestTies(t *testing.T) {
	ok := totals{Opening: -10, CreditsIn: 30, ReferralIn: 5, CashbackOut: 11, Reversals: -2, Adjustments: 3, PayoutsOut: 4, Closing: 11}
	require.True(t, ok.ties())
	ok.Closing++
	require.False(t, ok.ties())
}

func TestPayable(t *testing.T) {
	a, b, c := ids.New(time.Unix(1, 0)), ids.New(time.Unix(2, 0)), ids.New(time.Unix(3, 0))
	st := func(id ids.ID, closing int64) storeStatement { return storeStatement{StoreID: id, Closing: closing} }
	bal := func(id ids.ID, n int64) payoutsdb.StoreAccountsRow {
		return payoutsdb.StoreAccountsRow{StoreID: ids.UUID(id), Balance: n}
	}
	sum := func(ls []Line) (n int64) {
		for _, l := range ls {
			n += l.Credits
		}
		return n
	}

	// Each store pays min(closing, now).
	ls := payable([]storeStatement{st(a, 24), st(b, 50)}, []payoutsdb.StoreAccountsRow{bal(a, 19), bal(b, 50)}, 74)
	require.Equal(t, []Line{{a, 19}, {b, 50}}, ls)

	// A negative store is carried against the others: the business is paid
	// at most its net closing, taken in store id order.
	ls = payable([]storeStatement{st(a, -30), st(b, 20), st(c, 40)}, []payoutsdb.StoreAccountsRow{bal(a, -30), bal(b, 20), bal(c, 40)}, 30)
	require.EqualValues(t, 30, sum(ls))
	require.Equal(t, []Line{{b, 20}, {c, 10}}, ls)

	// Activity since the close lowers it; nothing payable is no lines.
	require.Empty(t, payable([]storeStatement{st(a, 20)}, []payoutsdb.StoreAccountsRow{bal(a, -1)}, 20))
	require.Empty(t, payable([]storeStatement{st(a, -5)}, []payoutsdb.StoreAccountsRow{bal(a, -5)}, -5))
	require.Empty(t, payable(nil, nil, 0))

	// The current total caps it too (a store with no statement line went negative since).
	ls = payable([]storeStatement{st(a, 50)}, []payoutsdb.StoreAccountsRow{bal(a, 50), bal(b, -45)}, 50)
	require.Equal(t, []Line{{a, 5}}, ls)
}

func TestCleanReason(t *testing.T) {
	r, err := cleanReason("  July payout \n")
	require.NoError(t, err)
	require.Equal(t, "July payout", r)
	for _, bad := range []string{"", "   ", "a\x00b", "tab\there", strings.Repeat("x", maxReason+1)} {
		_, err := cleanReason(bad)
		require.Error(t, err, "%q", bad)
	}
	_, err = cleanReason(strings.Repeat("x", maxReason))
	require.NoError(t, err)
}

func TestUTRPattern(t *testing.T) {
	// UPI RRN 12, NEFT UTR 16, RTGS UTR 22.
	for _, ok := range []string{"123456789012", "HDFCN52026102500", "HDFCR52026102512345678"} {
		require.True(t, utrPattern.MatchString(ok), ok)
	}
	for _, bad := range []string{"", "AXIS123456", "12345678901", "axis12345678", "AXIS-1234567", "AXIS 1234567", strings.Repeat("A", 23)} {
		require.False(t, utrPattern.MatchString(bad), bad)
	}
}
