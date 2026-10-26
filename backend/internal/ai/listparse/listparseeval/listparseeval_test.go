package listparseeval_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/listparse"
	"github.com/todayz/backend/internal/ai/listparse/listparseeval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

// SC-AI-41: the recorded list_parse suite (200 lists, five languages and mixed) meets its
// 95% bar, a list with an invented item, count, phone or nothing to buy is refused or loses
// that line, and the p95 latency is under 2.5 s (07 W3-12).
func TestListParseRecorded(t *testing.T) {
	s, ok := eval.Get(listparse.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 200)
	langs := map[string]int{}
	for _, c := range cases {
		langs[c.Name[:2]]++
	}
	for _, l := range []string{"en", "hi", "kn", "te", "ta", "mx"} {
		assert.GreaterOrEqual(t, langs[l], 20, l)
	}
	rp := eval.NewReplay(s.Feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	env := eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}
	rep, err := eval.Run(context.Background(), s, cases, env, "recorded")
	require.NoError(t, err)
	t.Logf("list_parse: %d/%d exact, p95 %s", rep.Passed, rep.Cases, rep.LatencyP95)
	assert.True(t, rep.OK, rep.Failures)
	assert.GreaterOrEqual(t, rep.Score, listparseeval.PassBar)
	assert.Less(t, rep.LatencyP95, 2500*time.Millisecond)
}
