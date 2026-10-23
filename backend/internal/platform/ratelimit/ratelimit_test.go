package ratelimit

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
)

var t0 = time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)

func TestBucket(t *testing.T) {
	clk := clock.NewFake(t0)
	l := New(clk, map[string]Rule{"g": {PerMinute: 60, Burst: 3}})

	for i := range 3 {
		r := l.Allow("g", "k")
		require.True(t, r.Allowed, i)
		require.Equal(t, 3, r.Limit)
		require.Equal(t, 2-i, r.Remaining)
	}
	r := l.Allow("g", "k")
	require.False(t, r.Allowed, "the burst is spent")
	require.Equal(t, time.Second, r.RetryAfter, "one token a second")
	require.Equal(t, 3*time.Second, r.Reset)

	require.True(t, l.Allow("g", "other").Allowed, "keys have their own buckets")

	clk.Advance(time.Second)
	require.True(t, l.Allow("g", "k").Allowed, "refilled one token")
	require.False(t, l.Allow("g", "k").Allowed)

	clk.Advance(time.Hour)
	r = l.Allow("g", "k")
	require.True(t, r.Allowed)
	require.Equal(t, 2, r.Remaining, "refill is capped at the burst")
}

func TestUnlimitedGroups(t *testing.T) {
	l := New(clock.NewFake(t0), map[string]Rule{"off": {}})
	for range 100 {
		require.True(t, l.Allow("none", "k").Allowed)
		require.True(t, l.Allow("off", "k").Allowed)
	}
	require.Zero(t, l.Len(), "an unlimited group keeps no buckets")
}

func TestClockGoingBack(t *testing.T) {
	clk := clock.NewFake(t0)
	l := New(clk, map[string]Rule{"g": {PerMinute: 60, Burst: 1}})
	require.True(t, l.Allow("g", "k").Allowed)
	clk.Set(t0.Add(-time.Hour)) // the local test clock may move back (D-80)
	require.False(t, l.Allow("g", "k").Allowed, "no refill from a negative interval")
}

func TestSweepBoundsMemory(t *testing.T) {
	clk := clock.NewFake(t0)
	l := New(clk, map[string]Rule{"g": {PerMinute: 60, Burst: 2}})
	for i := range 50 {
		l.Allow("g", fmt.Sprint(i))
	}
	require.Equal(t, 50, l.Len())
	clk.Advance(2 * time.Minute) // every bucket is full again
	l.Allow("g", "new")
	require.Equal(t, 1, l.Len(), "full buckets are dropped")
	require.Equal(t, 0, indexNUL("\x00a"))
	require.Equal(t, 3, indexNUL("abc"))
}

// Past MaxKeys with no bucket full, a sweep frees nothing; it must not run
// on every request, only once per MinSweepGap.
func TestSweepBackOff(t *testing.T) {
	clk := clock.NewFake(t0)
	l := New(clk, map[string]Rule{"g": {PerMinute: 1, Burst: 2}})
	for i := range MaxKeys {
		l.Allow("g", strconv.Itoa(i))
	}
	l.mu.Lock()
	base := l.sweeps
	l.mu.Unlock()

	for i := range 1000 {
		l.Allow("g", "more"+strconv.Itoa(i)) // above MaxKeys, nothing to drop
	}
	require.Equal(t, MaxKeys+1000, l.Len(), "nothing was full, nothing dropped")
	l.mu.Lock()
	require.LessOrEqual(t, l.sweeps-base, 1, "at most one sweep in the same second")
	base = l.sweeps
	l.mu.Unlock()

	clk.Advance(MinSweepGap - time.Millisecond)
	l.Allow("g", "x")
	l.mu.Lock()
	require.Equal(t, base, l.sweeps, "not before the gap")
	l.mu.Unlock()
	clk.Advance(time.Millisecond)
	l.Allow("g", "y")
	l.mu.Lock()
	require.Equal(t, base+1, l.sweeps, "again once the gap has passed")
	l.mu.Unlock()

	clk.Set(t0.Add(-time.Hour)) // the clock moved back: sweeping resumes at once
	l.Allow("g", "z")
	l.mu.Lock()
	require.Equal(t, base+2, l.sweeps)
	l.mu.Unlock()
}

func TestDefaultRulesCoverEveryGroup(t *testing.T) {
	for _, g := range []string{GroupAuth, GroupMe, GroupBiz, GroupAdmin, GroupPublic} {
		r, ok := DefaultRules[g]
		require.True(t, ok, g)
		require.Positive(t, r.PerMinute, g)
		require.Positive(t, r.Burst, g)
	}
}

func TestConcurrent(t *testing.T) {
	l := New(clock.NewFake(t0), map[string]Rule{"g": {PerMinute: 1, Burst: 100}})
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if l.Allow("g", "k").Allowed {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 100, allowed, "exactly the burst, under contention")
}
