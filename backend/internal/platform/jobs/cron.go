package jobs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
)

// Schedule is a parsed 5-field cron expression, evaluated in IST:
// "minute hour day-of-month month day-of-week". A field is *, a number, a
// range a-b, a step */n or a-b/n, or a comma list of those. Day of week is
// 0–7 (0 and 7 are Sunday). When both day fields are restricted, a day
// matching either runs (classic cron).
//
// Schedule implements river.PeriodicSchedule (Next).
type Schedule struct {
	expr                string
	min, hour, dom, mon uint64
	dow                 uint64
	domStar, dowStar    bool
}

// searchDays bounds the search for a matching day (9 years covers a
// 29 February schedule across a skipped leap year).
const searchDays = 9 * 366

type field struct {
	name   string
	lo, hi int
	bits   *uint64
	star   *bool
}

// ParseSchedule parses expr. It refuses an expression that never fires.
func ParseSchedule(expr string) (*Schedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("jobs: schedule %q: want 5 fields (minute hour day-of-month month day-of-week), got %d", expr, len(parts))
	}
	s := &Schedule{expr: strings.Join(parts, " ")}
	var minStar, hourStar, monStar bool
	fields := []field{
		{"minute", 0, 59, &s.min, &minStar},
		{"hour", 0, 23, &s.hour, &hourStar},
		{"day-of-month", 1, 31, &s.dom, &s.domStar},
		{"month", 1, 12, &s.mon, &monStar},
		{"day-of-week", 0, 7, &s.dow, &s.dowStar},
	}
	for i, f := range fields {
		bits, err := parseField(parts[i], f.lo, f.hi)
		if err != nil {
			return nil, fmt.Errorf("jobs: schedule %q: %s: %w", expr, f.name, err)
		}
		*f.bits = bits
		*f.star = parts[i] == "*"
	}
	if s.dow&(1<<7) != 0 { // 7 is Sunday
		s.dow = s.dow&^(1<<7) | 1
	}
	if s.Next(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
		return nil, fmt.Errorf("jobs: schedule %q never fires", expr)
	}
	return s, nil
}

func parseField(f string, lo, hi int) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(f, ",") {
		if part == "" {
			return 0, errors.New("empty list item")
		}
		rng, stepS, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepS)
			if err != nil || n < 1 || n > hi-lo+1 {
				return 0, fmt.Errorf("bad step %q", stepS)
			}
			step = n
		}
		var a, b int
		switch {
		case rng == "*":
			a, b = lo, hi
		case strings.Contains(rng, "-"):
			as, bs, _ := strings.Cut(rng, "-")
			var err1, err2 error
			a, err1 = strconv.Atoi(as)
			b, err2 = strconv.Atoi(bs)
			if err1 != nil || err2 != nil || a > b {
				return 0, fmt.Errorf("bad range %q", rng)
			}
		default:
			n, err := strconv.Atoi(rng)
			if err != nil {
				return 0, fmt.Errorf("bad value %q", rng)
			}
			a, b = n, n
			if hasStep { // "5/15" means 5-hi/15
				b = hi
			}
		}
		if a < lo || b > hi {
			return 0, fmt.Errorf("%q is outside %d-%d", part, lo, hi)
		}
		for v := a; v <= b; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, nil
}

// String is the normalised expression.
func (s *Schedule) String() string { return s.expr }

func (s *Schedule) dayMatches(t time.Time) bool {
	if s.mon&(1<<uint(t.Month())) == 0 {
		return false
	}
	domOK := s.dom&(1<<uint(t.Day())) != 0
	dowOK := s.dow&(1<<uint(t.Weekday())) != 0
	if s.domStar || s.dowStar {
		return domOK && dowOK
	}
	return domOK || dowOK
}

func istDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, clock.IST)
}

// Next returns the first tick strictly after t (UTC), or the zero time if
// none is found.
func (s *Schedule) Next(t time.Time) time.Time {
	c := t.In(clock.IST).Truncate(time.Minute).Add(time.Minute)
	for range searchDays {
		if s.dayMatches(c) {
			for h := c.Hour(); h < 24; h++ {
				if s.hour&(1<<uint(h)) == 0 {
					continue
				}
				m0 := 0
				if h == c.Hour() {
					m0 = c.Minute()
				}
				for m := m0; m < 60; m++ {
					if s.min&(1<<uint(m)) != 0 {
						return time.Date(c.Year(), c.Month(), c.Day(), h, m, 0, 0, clock.IST).UTC()
					}
				}
			}
		}
		c = istDay(c).AddDate(0, 0, 1)
	}
	return time.Time{}
}

// Prev returns the last tick at or before t (UTC), or the zero time.
func (s *Schedule) Prev(t time.Time) time.Time {
	c := t.In(clock.IST).Truncate(time.Minute)
	for range searchDays {
		if s.dayMatches(c) {
			for h := c.Hour(); h >= 0; h-- {
				if s.hour&(1<<uint(h)) == 0 {
					continue
				}
				m0 := 59
				if h == c.Hour() {
					m0 = c.Minute()
				}
				for m := m0; m >= 0; m-- {
					if s.min&(1<<uint(m)) != 0 {
						return time.Date(c.Year(), c.Month(), c.Day(), h, m, 0, 0, clock.IST).UTC()
					}
				}
			}
		}
		c = istDay(c).Add(-time.Minute) // 23:59 of the day before
	}
	return time.Time{}
}

// Interval is the gap between the tick at or before t and the next tick:
// the period the watchdog allows for (2× it, or 15 minutes past a daily tick).
func (s *Schedule) Interval(t time.Time) time.Duration {
	p := s.Prev(t)
	if p.IsZero() {
		return 0
	}
	return s.Next(p).Sub(p)
}
