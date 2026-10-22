package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 22, 4, 30, 0, 0, time.UTC) // 10:00 IST

func TestFakeIsFrozenAndMoves(t *testing.T) {
	f := NewFake(t0)
	require.Equal(t, t0, f.Now())
	require.Equal(t, t0, f.Now(), "a fake does not move by itself")

	require.Equal(t, t0.Add(30*time.Minute), f.Advance(30*time.Minute))
	require.Equal(t, t0.Add(30*time.Minute), f.Now())

	f.Set(t0.Add(-time.Hour))
	require.Equal(t, t0.Add(-time.Hour), f.Now(), "Set can move backwards")
}

func TestFakeNormalisesToUTC(t *testing.T) {
	f := NewFake(t0.In(IST))
	require.Equal(t, time.UTC, f.Now().Location())
	f.Set(t0.In(IST))
	require.Equal(t, time.UTC, f.Now().Location())
}

func TestRealIsUTC(t *testing.T) {
	var c Clock = Real{}
	require.Equal(t, time.UTC, c.Now().Location())
	require.WithinDuration(t, time.Now(), c.Now(), time.Second)
}

func TestOffset(t *testing.T) {
	base := NewFake(t0)
	o := NewOffset(base)
	require.Equal(t, t0, o.Now())
	require.Nil(t, o.NowOrNil(), "zero offset defers to the database clock")

	require.Equal(t, 10*time.Minute, o.Advance(10*time.Minute))
	require.Equal(t, t0.Add(10*time.Minute), o.Now())
	require.NotNil(t, o.NowOrNil())
	require.Equal(t, t0.Add(10*time.Minute), *o.NowOrNil())

	base.Advance(time.Minute) // time keeps flowing under the offset
	require.Equal(t, t0.Add(11*time.Minute), o.Now())

	o.Set(t0.Add(2 * time.Hour))
	require.Equal(t, t0.Add(2*time.Hour), o.Now())
	require.Equal(t, 2*time.Hour-time.Minute, o.Offset())

	o.SetOffset(0)
	require.Nil(t, o.NowOrNil())
}

func TestOffsetDefaultsToReal(t *testing.T) {
	o := NewOffset(nil)
	require.WithinDuration(t, time.Now(), o.Now(), time.Second)
}

func TestISTBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		in         time.Time
		day, month time.Time
	}{
		{"morning IST", t0, time.Date(2026, 10, 21, 18, 30, 0, 0, time.UTC), time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC)},
		{"just before IST midnight", time.Date(2026, 10, 31, 18, 29, 59, 0, time.UTC), time.Date(2026, 10, 30, 18, 30, 0, 0, time.UTC), time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC)},
		{"IST midnight starts a new month", time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC), time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC), time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.day, StartOfDayIST(c.in))
			require.Equal(t, c.month, StartOfMonthIST(c.in))
		})
	}
}
