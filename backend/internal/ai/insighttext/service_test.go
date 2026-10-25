package insighttext_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/insighttext"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/retail/retailtest"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func newService(e *retailtest.Env) *insighttext.Service {
	return insighttext.New(insighttext.Deps{
		DB: e.DB, Clock: e.Clock, Settings: e.Settings, LLM: e.LLM, Guard: e.Guard,
		Go: func(f func()) { f() },
	})
}

func row(subject string, days, units int) facts.Row {
	return facts.Row{Kind: facts.SellingFast, SubjectRef: subject, Facts: map[string]any{"daysLeft": days, "units": units}}
}

// next moves past the per-store run gap, so the next Texts may run again.
func next(e *retailtest.Env) { e.Clock.Advance(11 * time.Minute) }

func texts(out []*string) []string {
	s := make([]string, len(out))
	for i, t := range out {
		if t != nil {
			s[i] = *t
		}
	}
	return s
}

// SC-AI-17, SC-AI-18 and D-228: the sentence is written in the background
// and served once kept; an ungrounded one is cached as a miss and not asked
// again until its facts change.
func TestTextsNegativeCache(t *testing.T) {
	e := retailtest.New(t)
	e.Set(t, settings.AiInsighttextEnabled, "true")
	svc := newService(e)
	rows := []facts.Row{row("a", 3, 12), row("b", 2, 40)}
	e.LLM.Reply = insighttext.ToolOutput{Texts: []insighttext.ToolText{
		{Ref: "r1", Text: "Only 3 days of stock left after 12 sold."},
		{Ref: "r2", Text: "Sold 99 units this week."}, // 99 is not a fact
	}}

	// The first list shows the templates; the run happens behind it.
	assert.Equal(t, []string{"", ""}, texts(svc.Texts(e.Ctx, retailtest.Store, rows)))
	require.Equal(t, 1, e.LLM.Calls())
	assert.Equal(t, 1, e.Count(t, `SELECT count(*) FROM insight_texts WHERE store_id = $1`, ids.UUID(retailtest.Store)))
	assert.Equal(t, 1, e.Count(t, `SELECT count(*) FROM ai_text_misses WHERE feature = 'insight_text' AND reason = 'grounding'`))

	// The kept sentence is served; the missed row is not sent again.
	next(e)
	assert.Equal(t, []string{"Only 3 days of stock left after 12 sold.", ""}, texts(svc.Texts(e.Ctx, retailtest.Store, rows)))
	assert.Equal(t, 1, e.LLM.Calls(), "a cached miss is not asked again")

	// A changed number asks once more.
	next(e)
	rows[1] = row("b", 2, 41)
	e.LLM.Reply = insighttext.ToolOutput{Texts: []insighttext.ToolText{{Ref: "r1", Text: "41 sold, 2 days left."}}}
	svc.Texts(e.Ctx, retailtest.Store, rows)
	assert.Equal(t, 2, e.LLM.Calls())
	next(e)
	assert.Equal(t, []string{"Only 3 days of stock left after 12 sold.", "41 sold, 2 days left."}, texts(svc.Texts(e.Ctx, retailtest.Store, rows)))

	// An ungrounded miss outlives retail.MissTTL; only new facts ask again.
	e.Clock.Advance(retail.MissTTL + time.Minute)
	rows[1] = row("b", 2, 40)
	svc.Texts(e.Ctx, retailtest.Store, rows)
	assert.Equal(t, 2, e.LLM.Calls(), "a grounding miss holds while its facts do")

	// Background wording never takes from the calls the owner starts.
	assert.Equal(t, 0, e.Count(t, `SELECT count(*) FROM ai_usage_daily WHERE scope = 'store'`))
	assert.Equal(t, 2, e.Count(t, `SELECT coalesce(sum(calls), 0)::int FROM ai_usage_daily WHERE scope = 'store_background'`))
}

// A schema break is cached for every row of the call, but only for
// retail.MissTTL (a later call may not repeat it); a provider error is not.
func TestTextsMissOnReject(t *testing.T) {
	e := retailtest.New(t)
	e.Set(t, settings.AiInsighttextEnabled, "true")
	svc := newService(e)
	rows := []facts.Row{row("a", 3, 12)}

	e.LLM.Reply = map[string]any{"texts": "not a list"}
	svc.Texts(e.Ctx, retailtest.Store, rows)
	require.Equal(t, 1, e.LLM.Calls())
	assert.Equal(t, 1, e.Count(t, `SELECT count(*) FROM ai_text_misses WHERE reason = 'schema'`))
	next(e)
	svc.Texts(e.Ctx, retailtest.Store, rows)
	assert.Equal(t, 1, e.LLM.Calls())

	e.Clock.Advance(retail.MissTTL)
	e.LLM.Reply = insighttext.ToolOutput{Texts: []insighttext.ToolText{{Ref: "r1", Text: "3 days left."}}}
	svc.Texts(e.Ctx, retailtest.Store, rows)
	assert.Equal(t, 2, e.LLM.Calls(), "the same facts are asked again after MissTTL")
	next(e)
	assert.Equal(t, []string{"3 days left."}, texts(svc.Texts(e.Ctx, retailtest.Store, rows)))
}

// A failed read of the misses still serves the kept texts, and asks for nothing.
func TestTextsMissesUnreadable(t *testing.T) {
	e := retailtest.New(t)
	e.Set(t, settings.AiInsighttextEnabled, "true")
	svc := newService(e)
	rows := []facts.Row{row("a", 3, 12)}
	e.LLM.Reply = insighttext.ToolOutput{Texts: []insighttext.ToolText{{Ref: "r1", Text: "3 days left."}}}
	svc.Texts(e.Ctx, retailtest.Store, rows)
	require.Equal(t, 1, e.LLM.Calls())

	_, err := e.TDB.OwnerPool.Exec(e.Ctx, `REVOKE SELECT ON ai_text_misses FROM todayz_app`)
	require.NoError(t, err)
	next(e)
	rows = append(rows, row("b", 2, 40))
	assert.Equal(t, []string{"3 days left.", ""}, texts(svc.Texts(e.Ctx, retailtest.Store, rows)))
	assert.Equal(t, 1, e.LLM.Calls(), "no call without the misses")
}

func TestTextsOffAndBudget(t *testing.T) {
	e := retailtest.New(t)
	svc := newService(e)
	rows := []facts.Row{row("a", 3, 12)}
	e.LLM.Reply = insighttext.ToolOutput{Texts: []insighttext.ToolText{{Ref: "r1", Text: "3 days left."}}}

	// Off: templates, no model call.
	assert.Equal(t, []string{""}, texts(svc.Texts(e.Ctx, retailtest.Store, rows)))
	assert.Equal(t, 0, e.LLM.Calls())

	// The background budget spent: templates, no model call, no error, and
	// the owner's own cap is not touched.
	e.Set(t, settings.AiInsighttextEnabled, "true")
	e.Set(t, settings.AiDailyCallsStoreBackground, "0")
	next(e)
	assert.Equal(t, []string{""}, texts(svc.Texts(e.Ctx, retailtest.Store, rows)))
	assert.Equal(t, 0, e.LLM.Calls())
	assert.Equal(t, 0, e.Count(t, `SELECT count(*) FROM ai_usage_daily`))
	assert.Equal(t, 0, e.Count(t, `SELECT count(*) FROM ai_text_misses`), "a spent budget is not a miss")

	// The owner's cap at 0 does not stop background wording (its own budget).
	e.Set(t, settings.AiDailyCallsStoreBackground, "20")
	e.Set(t, settings.AiDailyCallsStore, "0")
	next(e)
	svc.Texts(e.Ctx, retailtest.Store, rows)
	assert.Equal(t, 1, e.LLM.Calls())
}
