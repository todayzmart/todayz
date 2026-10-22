// Package clock is the only source of "now" in the backend (02 §2.5).
//
// Production code takes a Clock and never calls time.Now directly, so tests can
// run a 30-minute pickup hold in microseconds with a Fake, and local/test servers
// can move time with /v1/test/clock through an Offset.
package clock

import (
	"sync"
	"time"
)

// IST is India Standard Time. "Day" and "month" in caps and limits are IST
// calendar days and months (02 §2.5). A fixed zone avoids depending on tzdata.
var IST = time.FixedZone("IST", 5*3600+30*60)

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Real is the wall clock, in UTC.
type Real struct{}

// Now returns the current UTC time.
func (Real) Now() time.Time { return time.Now().UTC() }

// Fake is a frozen clock for tests. It only moves when Set or Advance is called.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake frozen at t (UTC).
func NewFake(t time.Time) *Fake { return &Fake{now: t.UTC()} }

// Now returns the frozen time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set moves the clock to t (forwards or backwards).
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}

// Advance moves the clock forward by d and returns the new time.
func (f *Fake) Advance(d time.Duration) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	return f.now
}

// Offset is a running clock shifted by an adjustable offset. Local and test
// servers use it behind /v1/test/clock: time keeps flowing, but can be pushed
// forward to fire timers. With a zero offset it behaves like its base.
//
// Offset also satisfies River's TimeGenerator (Now / NowOrNil), so the job
// queue sees the same shifted time as the handlers.
type Offset struct {
	mu     sync.RWMutex
	base   Clock
	offset time.Duration
}

// NewOffset wraps base (nil means Real).
func NewOffset(base Clock) *Offset {
	if base == nil {
		base = Real{}
	}
	return &Offset{base: base}
}

// Now returns base time plus the offset.
func (o *Offset) Now() time.Time {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.base.Now().Add(o.offset)
}

// NowOrNil returns nil while the offset is zero, so River uses the database
// clock; otherwise it returns the shifted time.
func (o *Offset) NowOrNil() *time.Time {
	o.mu.RLock()
	off := o.offset
	o.mu.RUnlock()
	if off == 0 {
		return nil
	}
	t := o.Now()
	return &t
}

// Offset returns the current offset.
func (o *Offset) Offset() time.Duration {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.offset
}

// SetOffset replaces the offset.
func (o *Offset) SetOffset(d time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.offset = d
}

// Set moves the clock so that Now returns t at this instant, and returns the new offset.
func (o *Offset) Set(t time.Time) time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.offset = t.Sub(o.base.Now())
	return o.offset
}

// Advance pushes the clock forward by d and returns the new offset.
func (o *Offset) Advance(d time.Duration) time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.offset += d
	return o.offset
}

// StartOfDayIST returns the start of t's IST calendar day, in UTC.
func StartOfDayIST(t time.Time) time.Time {
	l := t.In(IST)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, IST).UTC()
}

// StartOfMonthIST returns the start of t's IST calendar month, in UTC.
func StartOfMonthIST(t time.Time) time.Time {
	l := t.In(IST)
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, IST).UTC()
}
