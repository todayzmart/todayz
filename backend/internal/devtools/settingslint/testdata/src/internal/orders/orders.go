// Package orders is a seeded SC-PL-06 fixture: TestFixture expects exactly the
// findings marked "want".
package orders

import "time"

// Hold hard-codes stock_stale_after_hours (72). want
const Hold = 72 * time.Hour

// Fine: common numbers are not flagged.
const (
	Minutes = 30
	Status  = 200
	Small   = 5
)

func cap() int64 {
	return 50_000 // want: ai_daily_cost_cap_paise and admin_adjust_dual_approval_above_paise
}

func weights() []float64 {
	return []float64{4500.0, 0.5} // want: search_w_text
}

const fortnight = 14 //settings:ok an ISO fortnight, not noshow_limit_days

const noReason = 3000 //settings:ok

const hex = 0x7D0 // want: 2000, insight_wanted_nearby_radius_m
