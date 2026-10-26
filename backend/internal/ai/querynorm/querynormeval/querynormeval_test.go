package querynormeval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/querynorm"
	"github.com/todayz/backend/internal/ai/querynorm/querynormeval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

// SC-AI-43: the recorded search_assist suite (100 sentences, five languages) meets its 90%
// bar with no chip outside the allowed kinds; an invented price, an unknown brand, words not
// in the sentence and a kind outside the list are never shown.
func TestSearchAssistRecorded(t *testing.T) {
	s, ok := eval.Get(querynorm.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 100)
	rp := eval.NewReplay(s.Feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	env := eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}
	rep, err := eval.Run(context.Background(), s, cases, env, "recorded")
	require.NoError(t, err)
	t.Logf("search_assist: %d/%d exact, p95 %s", rep.Passed, rep.Cases, rep.LatencyP95)
	assert.True(t, rep.OK, rep.Failures)
	assert.GreaterOrEqual(t, rep.Score, querynormeval.PassBar)
	for _, f := range rep.Failures {
		assert.NotContains(t, f.Note, "outside the allowed kinds", f.Case)
	}
}
