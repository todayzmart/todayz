package insighttext_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/todayz/backend/internal/ai/insighttext"
	"github.com/todayz/backend/internal/insights/facts"
)

// TestFactKeyUnits is the audit of every number the Demand insights kinds emit (D-230): its
// readable key's name gives the unit grounding reads it in, so none needs a units map
// (unlike the coach's expiry_penalty threshold). A percent key says pct, an amount is
// …Paise (read as …Rupees), and everything else is a count. radiusM (metres) and
// staleAfterHours (hours) are plain counts in their own units: a sentence that says "3 km"
// or "7 days" for them is ungrounded and keeps the template (conservative). liftBp is
// basis points, also plain.
func TestFactKeyUnits(t *testing.T) {
	plain := []string{
		"soldQty", "periodDays", "available", "daysOfStock", "runOutInDays", "qty", "onHand",
		"daysNoSale", "expiringCount", "withinDays", "moreCount", "searches", "radiusM",
		"zeroDays", "expiredDaysAgo", "staleCount", "staleAfterHours", "billsA", "billsBoth",
		"oneIn", "liftBp", "count", "lapseDays", "minVisits",
	}
	percent := []string{"pct", "sharePct"}
	money := []string{"valueAtCostPaise", "pricePaise", "costPaise", "mrpPaise", "typicalPaise"}

	ground := func(key string, text string) bool {
		r := facts.Row{Kind: facts.SellingFast, Facts: map[string]any{key: int64(3700)}}
		return insighttext.Grounded(text, insighttext.Row{Facts: insighttext.Readable(r)})
	}
	for _, k := range plain {
		assert.True(t, ground(k, "3700 left"), k)
		assert.False(t, ground(k, "3700% more"), k)
		assert.False(t, ground(k, "₹3700 stuck"), k)
	}
	for _, k := range percent {
		assert.True(t, ground(k, "3700% of bills"), k)
		assert.False(t, ground(k, "₹3700 stuck"), k)
	}
	for _, k := range money {
		assert.True(t, ground(k, "₹37 stuck"), k+" is read in rupees")
		assert.False(t, ground(k, "37% more"), k)
		assert.False(t, ground(k, "₹3700 stuck"), k+" is not paise")
	}
	// An action's numbers ground under "action" with the same rules (refPaise, the
	// suggested discount pct).
	r := facts.Row{Kind: facts.SellingBelowCost, Action: facts.Action{Payload: map[string]any{"refPaise": int64(4800), "pct": int64(10)}}}
	assert.True(t, insighttext.Grounded("Price it at ₹48, 10% off", insighttext.Row{Facts: insighttext.Readable(r)}))
}
