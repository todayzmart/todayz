package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
)

func ist(y int, mo time.Month, d, h, m int) time.Time {
	return time.Date(y, mo, d, h, m, 0, 0, clock.IST).UTC()
}

func TestScheduleNextPrev(t *testing.T) {
	cases := []struct {
		expr       string
		at         time.Time
		next, prev time.Time
	}{
		{"*/5 * * * *", ist(2026, 10, 23, 10, 2), ist(2026, 10, 23, 10, 5), ist(2026, 10, 23, 10, 0)},
		{"*/5 * * * *", ist(2026, 10, 23, 10, 5), ist(2026, 10, 23, 10, 10), ist(2026, 10, 23, 10, 5)},
		{"0 2 * * *", ist(2026, 10, 23, 10, 0), ist(2026, 10, 24, 2, 0), ist(2026, 10, 23, 2, 0)},
		{"0 2 * * *", ist(2026, 10, 23, 1, 59), ist(2026, 10, 23, 2, 0), ist(2026, 10, 22, 2, 0)},
		{"30 23 31 12 *", ist(2026, 6, 1, 0, 0), ist(2026, 12, 31, 23, 30), ist(2025, 12, 31, 23, 30)},
		{"0 0 1 * *", ist(2026, 10, 23, 0, 0), ist(2026, 11, 1, 0, 0), ist(2026, 10, 1, 0, 0)},
		{"15 9 * * 1-5", ist(2026, 10, 24, 12, 0), ist(2026, 10, 26, 9, 15), ist(2026, 10, 23, 9, 15)}, // Sat → Mon; Fri
		{"0 9 * * 7", ist(2026, 10, 23, 0, 0), ist(2026, 10, 25, 9, 0), ist(2026, 10, 18, 9, 0)},       // 7 is Sunday
		{"0 0 29 2 *", ist(2026, 3, 1, 0, 0), ist(2028, 2, 29, 0, 0), ist(2024, 2, 29, 0, 0)},
		{"0 6,18 * * *", ist(2026, 10, 23, 12, 0), ist(2026, 10, 23, 18, 0), ist(2026, 10, 23, 6, 0)},
		{"10-20/5 * * * *", ist(2026, 10, 23, 10, 21), ist(2026, 10, 23, 11, 10), ist(2026, 10, 23, 10, 20)},
		{"0 0 13 * 5", ist(2026, 10, 10, 0, 0), ist(2026, 10, 13, 0, 0), ist(2026, 10, 9, 0, 0)}, // dom OR dow: Tue 13th, Fri 9th
	}
	for _, c := range cases {
		s, err := ParseSchedule(c.expr)
		require.NoError(t, err, c.expr)
		require.Equal(t, c.next, s.Next(c.at), "%s next after %s", c.expr, c.at.In(clock.IST))
		require.Equal(t, c.prev, s.Prev(c.at), "%s prev at %s", c.expr, c.at.In(clock.IST))
	}
}

func TestScheduleSecondsAreIgnored(t *testing.T) {
	s, err := ParseSchedule("*/5 * * * *")
	require.NoError(t, err)
	at := ist(2026, 10, 23, 10, 5).Add(42 * time.Second)
	require.Equal(t, ist(2026, 10, 23, 10, 5), s.Prev(at))
	require.Equal(t, ist(2026, 10, 23, 10, 10), s.Next(at))
}

func TestScheduleInterval(t *testing.T) {
	five, _ := ParseSchedule("*/5 * * * *")
	require.Equal(t, 5*time.Minute, five.Interval(ist(2026, 10, 23, 10, 2)))
	daily, _ := ParseSchedule("0 2 * * *")
	require.Equal(t, 24*time.Hour, daily.Interval(ist(2026, 10, 23, 10, 2)))
	twice, _ := ParseSchedule("0 6,18 * * *")
	require.Equal(t, 12*time.Hour, twice.Interval(ist(2026, 10, 23, 7, 0)))
}

func TestParseScheduleRefuses(t *testing.T) {
	for _, expr := range []string{
		"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *",
		"* * * 13 *", "* * * * 8", "*/0 * * * *", "5-1 * * * *", "a * * * *",
		"1,,2 * * * *", "@daily", "0 0 30 2 *",
	} {
		_, err := ParseSchedule(expr)
		require.Error(t, err, "%q", expr)
	}
}
