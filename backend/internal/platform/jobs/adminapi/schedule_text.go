package adminapi

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// scheduleText reads a 5-field IST cron expression as English for A-21. It
// knows the shapes the plan's kinds use and falls back to the expression.
func scheduleText(expr string) string {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return expr + " (IST)"
	}
	minute, hour, dom, month, dow := f[0], f[1], f[2], f[3], f[4]
	if month != "*" {
		return expr + " (IST)"
	}
	num := func(s string) (int, bool) {
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
	step := func(s string) (int, bool) {
		if !strings.HasPrefix(s, "*/") {
			return 0, false
		}
		return num(strings.TrimPrefix(s, "*/"))
	}
	m, mOK := num(minute)
	h, hOK := num(hour)
	at := func() string { return fmt.Sprintf("%02d:%02d IST", h, m) }

	switch {
	case dom == "*" && dow == "*" && hour == "*" && minute == "*":
		return "Every minute"
	case dom == "*" && dow == "*" && hour == "*":
		if n, ok := step(minute); ok {
			return fmt.Sprintf("Every %d minutes", n)
		}
		if mOK {
			return fmt.Sprintf("Every hour at :%02d", m)
		}
	case dom == "*" && dow == "*" && mOK:
		if n, ok := step(hour); ok {
			return fmt.Sprintf("Every %d hours at :%02d", n, m)
		}
		if hOK {
			return "Every day at " + at()
		}
	case dom == "*" && mOK && hOK:
		if d, ok := num(dow); ok && d >= 0 && d <= 7 {
			return fmt.Sprintf("Every %s at %s", time.Weekday(d%7), at())
		}
	case dow == "*" && mOK && hOK:
		if d, ok := num(dom); ok {
			return fmt.Sprintf("Monthly on day %d at %s", d, at())
		}
	}
	return expr + " (IST)"
}
