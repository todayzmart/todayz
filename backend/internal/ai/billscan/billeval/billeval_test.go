package billeval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/billscan"
	"github.com/todayz/backend/internal/ai/billscan/billeval"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

func recorded(t *testing.T, feature string) (eval.Suite, []eval.Case, eval.Env) {
	t.Helper()
	s, ok := eval.Get(feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	rp := eval.NewReplay(s.Feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	return s, cases, eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}
}

// SC-AI-10: the recorded bill scan reads at least 90% of printed-bill lines and 75% of
// handwritten-bill lines right, over 60 bills in English, Hindi and Kannada.
func TestSupplierBillRecorded(t *testing.T) {
	s, cases, env := recorded(t, billscan.Feature)
	require.Len(t, cases, 60)
	kinds := map[string]int{}
	for _, c := range cases {
		switch {
		case len(c.Name) > 8 && c.Name[:8] == "printed_":
			kinds["printed"]++
		default:
			kinds["handwritten"]++
		}
	}
	assert.Equal(t, map[string]int{"printed": 36, "handwritten": 24}, kinds)

	lines, err := billeval.Score(context.Background(), env, cases)
	require.NoError(t, err)
	for kind, bar := range billeval.Bars {
		l := lines[kind]
		t.Logf("%s: %d of %d lines right (%.1f%%)", kind, l.Right, l.Total, 100*l.Rate())
		assert.GreaterOrEqual(t, l.Rate(), bar, kind)
	}

	rep, err := eval.Run(context.Background(), s, cases, env, "recorded")
	require.NoError(t, err)
	assert.True(t, rep.OK, rep.Failures)
}

func TestMatchCountsMissedAndExtraLines(t *testing.T) {
	p := func(v int64) *int64 { return &v }
	want := []billeval.Want{
		{Name: "Amul Butter 100g", Quantity: 2, CostPaise: p(5400), MrpPaise: p(6200)},
		{Name: "Tata Salt 1kg", Quantity: 1, CostPaise: p(2400)},
	}
	got := []billscan.Line{
		{Name: "amul butter  100G", Quantity: 2, CostPaise: p(5400), MrpPaise: p(6200)},
		{Name: "Parle-G 250g", Quantity: 1},
	}
	assert.Equal(t, billeval.Lines{Right: 1, Total: 3}, billeval.Match(want, got))
	got[0].MrpPaise = nil
	assert.Equal(t, billeval.Lines{Right: 0, Total: 3}, billeval.Match(want, got))
}
