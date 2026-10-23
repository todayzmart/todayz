package business

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
)

// Opening hours (01 §3, 03 stores.hours). All times are IST wall-clock
// times. A range is [open, close):
//
//   - close after open is a same-day range;
//   - close at or before open runs past midnight into the next day, so
//     22:00–02:00 is open from 22:00 to 02:00 the next morning;
//   - open equal to close is 24 hours from open (00:00–00:00 is all day);
//   - "24:00" is midnight at the end of the day (only as a close).
//
// A day with no ranges (or no entry) is closed. A holiday entry replaces the
// weekly ranges of its date; an empty holiday is closed all day. The DB
// default '{}' is a store with no hours, closed at all times.

// Range is one opening range of a day.
type Range struct {
	Open  string `json:"open"`
	Close string `json:"close"`
}

// Holiday replaces the weekly hours on one IST date (YYYY-MM-DD).
type Holiday struct {
	Date   string  `json:"date"`
	Ranges []Range `json:"ranges"`
}

// Hours is a store's weekly hours and holidays, as stored in stores.hours
// and stores.delivery_hours.
type Hours struct {
	// Weekly is keyed mon, tue, wed, thu, fri, sat, sun.
	Weekly   map[string][]Range `json:"weekly"`
	Holidays []Holiday          `json:"holidays"`
}

// Weekdays are the keys of Hours.Weekly, Monday first.
var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// dayKey maps time.Weekday (Sunday = 0) to the Weekly key.
var dayKey = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

const minutesPerDay = 24 * 60

// maxRangesPerDay and maxHolidays match the contract (HoursDay maxItems,
// StoreHours.holidays maxItems); Validate repeats them for stored data.
const (
	maxRangesPerDay = 4
	maxHolidays     = 60
)

// ParseHours reads stored hours. Empty input and '{}' are a store with no
// hours (closed).
func ParseHours(raw []byte) (Hours, error) {
	var h Hours
	if len(raw) == 0 {
		return h, nil
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return Hours{}, fmt.Errorf("business: stored hours: %w", err)
	}
	return h, nil
}

// IsEmpty reports whether no day and no holiday has a range: a store that is
// never open. A submit needs hours that are not empty.
func (h Hours) IsEmpty() bool {
	for _, rs := range h.Weekly {
		if len(rs) > 0 {
			return false
		}
	}
	for _, hd := range h.Holidays {
		if len(hd.Ranges) > 0 {
			return false
		}
	}
	return true
}

// Marshal writes the stored form: every weekday key in order and holidays
// sorted by date, so the same hours always store the same bytes.
func (h Hours) Marshal() ([]byte, error) {
	weekly := make(map[string][]Range, len(Weekdays))
	for _, d := range Weekdays {
		rs := h.Weekly[d]
		if rs == nil {
			rs = []Range{}
		}
		weekly[d] = rs
	}
	hol := slices.Clone(h.Holidays)
	if hol == nil {
		hol = []Holiday{}
	}
	for i := range hol {
		if hol[i].Ranges == nil {
			hol[i].Ranges = []Range{}
		}
	}
	slices.SortFunc(hol, func(a, b Holiday) int {
		switch {
		case a.Date < b.Date:
			return -1
		case a.Date > b.Date:
			return 1
		}
		return 0
	})
	return json.Marshal(struct {
		Weekly   map[string][]Range `json:"weekly"`
		Holidays []Holiday          `json:"holidays"`
	}{weekly, hol})
}

// FieldError is one invalid field: its path under the caller's prefix and a
// reason that never repeats the value.
type FieldError struct {
	Path   string
	Reason string
}

// Validate checks the hours and returns one entry per problem, with paths
// under prefix (e.g. "body.hours"). It checks what the contract cannot: real
// times, real and unique holiday dates, and ranges of a day that overlap.
func (h Hours) Validate(prefix string) []FieldError {
	var errs []FieldError
	for d := range h.Weekly {
		if !slices.Contains(Weekdays, d) {
			errs = append(errs, FieldError{prefix + ".weekly." + d, "unknown day"})
		}
	}
	for _, d := range Weekdays {
		errs = append(errs, validateDay(prefix+".weekly."+d, h.Weekly[d])...)
	}
	if len(h.Holidays) > maxHolidays {
		errs = append(errs, FieldError{prefix + ".holidays", "too many holidays"})
	}
	seen := map[string]bool{}
	for i, hd := range h.Holidays {
		p := prefix + ".holidays[" + strconv.Itoa(i) + "]"
		if _, err := time.Parse(time.DateOnly, hd.Date); err != nil {
			errs = append(errs, FieldError{p + ".date", "must be a real date (YYYY-MM-DD)"})
		} else if seen[hd.Date] {
			errs = append(errs, FieldError{p + ".date", "date appears twice"})
		}
		seen[hd.Date] = true
		errs = append(errs, validateDay(p+".ranges", hd.Ranges)...)
	}
	return errs
}

// span is a range in minutes from the start of its day; end may pass 1440
// when the range runs past midnight.
type span struct{ start, end int }

func validateDay(path string, rs []Range) []FieldError {
	var errs []FieldError
	if len(rs) > maxRangesPerDay {
		errs = append(errs, FieldError{path, "at most 4 ranges a day"})
	}
	spans := make([]span, 0, len(rs))
	for i, r := range rs {
		p := path + "[" + strconv.Itoa(i) + "]"
		sp, err := r.span()
		if err != nil {
			errs = append(errs, FieldError{p, err.Error()})
			continue
		}
		spans = append(spans, sp)
	}
	if len(spans) != len(rs) {
		return errs
	}
	order := make([]int, len(spans))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return spans[a].start - spans[b].start })
	for k := 1; k < len(order); k++ {
		prev, cur := spans[order[k-1]], spans[order[k]]
		if cur.start < prev.end {
			errs = append(errs, FieldError{path + "[" + strconv.Itoa(order[k]) + "]", "overlaps another range of the day"})
		}
	}
	// A range past midnight must not wrap onto the start of the same day's
	// first range (it would be open more than 24 hours in a row).
	if len(order) > 1 {
		last, first := spans[order[len(order)-1]], spans[order[0]]
		if last.end > minutesPerDay && last.end-minutesPerDay > first.start {
			errs = append(errs, FieldError{path + "[" + strconv.Itoa(order[len(order)-1]) + "]", "overlaps another range of the day"})
		}
	}
	return errs
}

// span reads a range: open 00:00–23:59, close 00:00–24:00.
func (r Range) span() (span, error) {
	o, ok := clockMinutes(r.Open, false)
	if !ok {
		return span{}, fmt.Errorf("open must be HH:MM from 00:00 to 23:59")
	}
	c, ok := clockMinutes(r.Close, true)
	if !ok {
		return span{}, fmt.Errorf("close must be HH:MM from 00:00 to 24:00")
	}
	if c <= o {
		c += minutesPerDay // past midnight (equal: 24 hours from open)
	}
	return span{o, c}, nil
}

// clockMinutes parses "HH:MM" into minutes from midnight; allow24 accepts
// "24:00".
func clockMinutes(s string, allow24 bool) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	hh, err1 := strconv.Atoi(s[:2])
	mm, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || s[0] == '+' || s[0] == '-' || s[3] == '+' || s[3] == '-' {
		return 0, false
	}
	switch {
	case hh == 24 && mm == 0 && allow24:
		return minutesPerDay, true
	case hh < 0 || hh > 23 || mm < 0 || mm > 59:
		return 0, false
	}
	return hh*60 + mm, true
}

// rangesOn returns the ranges that apply on an IST date: the holiday's if the
// date is a holiday, else the weekday's.
func (h Hours) rangesOn(date time.Time) []Range {
	key := date.Format(time.DateOnly)
	for _, hd := range h.Holidays {
		if hd.Date == key {
			return hd.Ranges
		}
	}
	return h.Weekly[dayKey[date.Weekday()]]
}

// OpenAt reports whether the hours are open at t. closedUntil, when set and
// after t, closes the store by hand (D-41 manual close). Invalid stored
// ranges count as closed.
//
// A range of the day before that runs past midnight is open into t's day even
// when t's day is a holiday: the holiday replaces the ranges that start on
// its date, not the end of the night before.
// TODO(lead): confirm that a closed holiday does not cut short the previous
// night's range (docs/qa/W1-04/REPORT.md, Questions).
func (h Hours) OpenAt(t time.Time, closedUntil *time.Time) bool {
	if closedUntil != nil && t.Before(*closedUntil) {
		return false
	}
	l := t.In(clock.IST)
	day := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, clock.IST)
	m := l.Hour()*60 + l.Minute()
	for _, r := range h.rangesOn(day) {
		sp, err := r.span()
		if err == nil && m >= sp.start && m < min(sp.end, minutesPerDay) {
			return true
		}
	}
	for _, r := range h.rangesOn(day.AddDate(0, 0, -1)) {
		sp, err := r.span()
		if err == nil && sp.end > minutesPerDay && m < sp.end-minutesPerDay {
			return true
		}
	}
	return false
}

// OpenAt is OpenAt on stored hours (stores.hours JSON). A store whose stored
// hours cannot be read is closed.
func OpenAt(storedHours []byte, closedUntil *time.Time, t time.Time) bool {
	h, err := ParseHours(storedHours)
	if err != nil {
		return false
	}
	return h.OpenAt(t, closedUntil)
}
