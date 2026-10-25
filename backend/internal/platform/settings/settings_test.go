package settings

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/money"
)

func fakeLoader(vals map[Key]Value, calls *int, fail *bool) Loader {
	return func(context.Context) (map[Key]Value, error) {
		*calls++
		if fail != nil && *fail {
			return nil, errors.New("db down")
		}
		cp := make(map[Key]Value, len(vals))
		for k, v := range vals {
			cp[k] = v
		}
		return cp, nil
	}
}

func row(k Key, u Unit, raw string) Value { return Value{Key: k, Unit: u, Raw: json.RawMessage(raw)} }

var sample = map[Key]Value{
	OrderAcceptTimeoutMin:      row(OrderAcceptTimeoutMin, UnitMinutes, "10"),
	PosReversalWindowHours:     row(PosReversalWindowHours, UnitHours, "24"),
	CreditExpiryDays:           row(CreditExpiryDays, UnitDays, "365"),
	NoshowStrikes:              row(NoshowStrikes, UnitCount, "3"),
	CreditMaxPerOrder:          row(CreditMaxPerOrder, UnitCredits, "30"),
	InsightWantedNearbyRadiusM: row(InsightWantedNearbyRadiusM, UnitMeters, "2000"),
	SearchWText:                row(SearchWText, UnitBP, "4500"),
	CreditMinOrderValuePaise:   row(CreditMinOrderValuePaise, UnitPaise, "10000"),
	FeatureDeliveryEnabled:     row(FeatureDeliveryEnabled, UnitBool, "true"),
	NotifyWhatsappEnabled:      row(NotifyWhatsappEnabled, UnitBool, "false"),
	AiModelFast:                row(AiModelFast, UnitText, `"claude-haiku-5-5"`),
	"broken_count":             row("broken_count", UnitCount, `"x"`),
}

func TestTypedGetters(t *testing.T) {
	ctx := context.Background()
	calls := 0
	c := NewCache(fakeLoader(sample, &calls, nil), Options{})

	n, err := c.Int(ctx, NoshowStrikes)
	require.NoError(t, err)
	require.Equal(t, int64(3), n)
	n, err = c.Int(ctx, CreditMaxPerOrder)
	require.NoError(t, err)
	require.Equal(t, int64(30), n)
	n, err = c.Int(ctx, InsightWantedNearbyRadiusM)
	require.NoError(t, err)
	require.Equal(t, int64(2000), n)

	bp, err := c.BP(ctx, SearchWText)
	require.NoError(t, err)
	require.Equal(t, money.BP(4500), bp)

	p, err := c.Paise(ctx, CreditMinOrderValuePaise)
	require.NoError(t, err)
	require.Equal(t, money.Paise(10000), p)

	d, err := c.Duration(ctx, OrderAcceptTimeoutMin)
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, d)
	d, err = c.Duration(ctx, PosReversalWindowHours)
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, d)
	d, err = c.Duration(ctx, CreditExpiryDays)
	require.NoError(t, err)
	require.Equal(t, 365*24*time.Hour, d)

	b, err := c.Bool(ctx, FeatureDeliveryEnabled)
	require.NoError(t, err)
	require.True(t, b)
	b, err = c.Bool(ctx, NotifyWhatsappEnabled)
	require.NoError(t, err)
	require.False(t, b)

	s, err := c.String(ctx, AiModelFast)
	require.NoError(t, err)
	require.Equal(t, "claude-haiku-5-5", s)

	require.Equal(t, 1, calls, "one load serves every read inside the TTL")
}

func TestUnitAndKeyErrors(t *testing.T) {
	ctx := context.Background()
	calls := 0
	c := NewCache(fakeLoader(sample, &calls, nil), Options{})

	_, err := c.Int(ctx, "no_such_key")
	require.ErrorIs(t, err, ErrUnknownKey)
	_, err = c.Bool(ctx, "no_such_key")
	require.ErrorIs(t, err, ErrUnknownKey)

	for name, f := range map[string]func() error{
		"Int on paise":      func() error { _, err := c.Int(ctx, CreditMinOrderValuePaise); return err },
		"Int on bp":         func() error { _, err := c.Int(ctx, SearchWText); return err },
		"Paise on bp":       func() error { _, err := c.Paise(ctx, SearchWText); return err },
		"BP on count":       func() error { _, err := c.BP(ctx, NoshowStrikes); return err },
		"Duration on count": func() error { _, err := c.Duration(ctx, NoshowStrikes); return err },
		"Bool on text":      func() error { _, err := c.Bool(ctx, AiModelFast); return err },
		"String on bool":    func() error { _, err := c.String(ctx, FeatureDeliveryEnabled); return err },
	} {
		require.ErrorIs(t, f(), ErrWrongUnit, name)
	}
	_, err = c.Int(ctx, "broken_count")
	require.ErrorIs(t, err, ErrBadValue)
}

// A minutes value past what time.Duration holds would wrap; a negative one would put every
// deadline in the past (W3-05 review).
func TestDurationOutOfRange(t *testing.T) {
	ctx := context.Background()
	calls := 0
	c := NewCache(fakeLoader(map[Key]Value{
		"huge_min":  row("huge_min", UnitMinutes, "200000000"),
		"neg_min":   row("neg_min", UnitMinutes, "-1"),
		"huge_days": row("huge_days", UnitDays, "200000"),
		"edge_min":  row("edge_min", UnitMinutes, "153722867"),
	}, &calls, nil), Options{})

	for _, k := range []Key{"huge_min", "neg_min", "huge_days"} {
		_, err := c.Duration(ctx, k)
		require.ErrorIs(t, err, ErrBadValue, k)
	}
	d, err := c.Duration(ctx, "edge_min")
	require.NoError(t, err, "the largest whole minute time.Duration holds")
	require.Positive(t, d)
}

func TestTTLAndInvalidate(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calls := 0
	fail := false
	vals := map[Key]Value{NoshowStrikes: row(NoshowStrikes, UnitCount, "3")}
	c := NewCache(fakeLoader(vals, &calls, &fail), Options{TTL: time.Minute, Now: func() time.Time { return now }})

	_, err := c.Int(ctx, NoshowStrikes)
	require.NoError(t, err)
	now = now.Add(59 * time.Second)
	_, _ = c.Int(ctx, NoshowStrikes)
	require.Equal(t, 1, calls)

	now = now.Add(time.Second)
	vals[NoshowStrikes] = row(NoshowStrikes, UnitCount, "4")
	n, _ := c.Int(ctx, NoshowStrikes)
	require.Equal(t, int64(4), n, "reloads after the TTL")
	require.Equal(t, 2, calls)

	vals[NoshowStrikes] = row(NoshowStrikes, UnitCount, "5")
	c.Invalidate()
	n, _ = c.Int(ctx, NoshowStrikes)
	require.Equal(t, int64(5), n, "reloads after Invalidate")
	require.Equal(t, int64(3), c.Reloads())

	// A failed reload serves the last good copy.
	fail = true
	c.Invalidate()
	n, err = c.Int(ctx, NoshowStrikes)
	require.NoError(t, err)
	require.Equal(t, int64(5), n)

	// With nothing cached, a failed load is an error.
	empty := NewCache(fakeLoader(vals, &calls, &fail), Options{})
	_, err = empty.Int(ctx, NoshowStrikes)
	require.Error(t, err)
}

// TestReloadFailureBacksOff (W3): a failing database costs one attempt per
// backoff step (1 s, 2 s, 4 s … up to the TTL), stale values keep being
// served, and a success or a NOTIFY ends the backoff.
func TestReloadFailureBacksOff(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calls := 0
	fail := false
	vals := map[Key]Value{NoshowStrikes: row(NoshowStrikes, UnitCount, "3")}
	c := NewCache(fakeLoader(vals, &calls, &fail), Options{TTL: 5 * time.Second, Now: func() time.Time { return now }})
	get := func() int64 {
		t.Helper()
		n, err := c.Int(ctx, NoshowStrikes)
		require.NoError(t, err, "stale values are served")
		return n
	}
	require.Equal(t, int64(3), get())
	require.Equal(t, 1, calls)

	fail = true
	c.Invalidate()
	for range 100 {
		require.Equal(t, int64(3), get())
	}
	require.Equal(t, 2, calls, "one attempt, then the backoff")

	for _, step := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second} {
		now = now.Add(step - time.Millisecond)
		before := calls
		get()
		require.Equal(t, before, calls, "still backing off (%v)", step)
		now = now.Add(time.Millisecond)
		get()
		require.Equal(t, before+1, calls, "retried after %v", step)
	}

	// The database is back; a NOTIFY ends the backoff at once.
	fail = false
	vals[NoshowStrikes] = row(NoshowStrikes, UnitCount, "4")
	c.Invalidate()
	require.Equal(t, int64(4), get())

	// A later failure starts the backoff again from 1 s.
	fail = true
	now = now.Add(5 * time.Second)
	before := calls
	get()
	now = now.Add(time.Second)
	get()
	require.Equal(t, before+2, calls)
}

// blockingLoader blocks the n-th and later calls until release closes.
type blockingLoader struct {
	mu      sync.Mutex
	calls   int
	from    int
	entered chan struct{}
	release chan struct{}
	val     string
}

func (b *blockingLoader) load(context.Context) (map[Key]Value, error) {
	b.mu.Lock()
	b.calls++
	n, v := b.calls, b.val
	b.mu.Unlock()
	if n >= b.from {
		b.entered <- struct{}{}
		<-b.release
	}
	return map[Key]Value{NoshowStrikes: row(NoshowStrikes, UnitCount, v)}, nil
}

func (b *blockingLoader) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// TestOneReloaderOthersServeStale (W3): while one caller reloads against a
// slow database, every other caller is served the held values at once, and
// no second load starts.
func TestOneReloaderOthersServeStale(t *testing.T) {
	ctx := context.Background()
	b := &blockingLoader{from: 2, entered: make(chan struct{}, 1), release: make(chan struct{}), val: "3"}
	c := NewCache(b.load, Options{})
	n, err := c.Int(ctx, NoshowStrikes)
	require.NoError(t, err)
	require.Equal(t, int64(3), n)

	b.mu.Lock()
	b.val = "4"
	b.mu.Unlock()
	c.Invalidate()
	reloader := make(chan int64, 1)
	go func() {
		n, _ := c.Int(ctx, NoshowStrikes)
		reloader <- n
	}()
	<-b.entered // the reloader is inside the (blocked) load

	served := make(chan struct{})
	go func() {
		defer close(served)
		var wg sync.WaitGroup
		for range 50 {
			wg.Go(func() {
				n, err := c.Int(ctx, NoshowStrikes)
				require.NoError(t, err)
				require.Equal(t, int64(3), n, "the held value")
			})
		}
		wg.Wait()
	}()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("callers blocked behind the reload")
	}
	require.Equal(t, 2, b.count(), "only one reload")

	close(b.release)
	require.Equal(t, int64(4), <-reloader)
	n, _ = c.Int(ctx, NoshowStrikes)
	require.Equal(t, int64(4), n)
	require.Equal(t, 2, b.count())
}

// TestFirstLoadIsSingleFlight: concurrent first reads wait for one load.
func TestFirstLoadIsSingleFlight(t *testing.T) {
	ctx := context.Background()
	b := &blockingLoader{from: 1, entered: make(chan struct{}, 1), release: make(chan struct{}), val: "3"}
	c := NewCache(b.load, Options{})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			n, err := c.Int(ctx, NoshowStrikes)
			require.NoError(t, err)
			require.Equal(t, int64(3), n)
		})
	}
	<-b.entered
	close(b.release)
	wg.Wait()
	require.Equal(t, 1, b.count())
}

// TestInvalidateDuringLoadIsKept: a NOTIFY that arrives while a load runs
// makes the next read load again (the running load may predate the change).
func TestInvalidateDuringLoadIsKept(t *testing.T) {
	ctx := context.Background()
	b := &blockingLoader{from: 2, entered: make(chan struct{}, 1), release: make(chan struct{}), val: "3"}
	c := NewCache(b.load, Options{})
	_, err := c.Int(ctx, NoshowStrikes)
	require.NoError(t, err)
	c.Invalidate()
	done := make(chan struct{})
	go func() { defer close(done); _, _ = c.Int(ctx, NoshowStrikes) }()
	<-b.entered
	c.Invalidate()
	close(b.release)
	<-done
	go func() { <-b.entered }()
	_, _ = c.Int(ctx, NoshowStrikes)
	require.Equal(t, 3, b.count(), "reloaded again after the mid-load NOTIFY")
}
