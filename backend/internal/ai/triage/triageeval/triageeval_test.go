package triageeval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/triage"
	"github.com/todayz/backend/internal/ai/triage/triageeval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

// SC-AI-60: the recorded ticket_triage suite meets 11 §4's category accuracy bar (≥ 85%):
// every category, a long thread with its summary and a short one whose summary is dropped,
// one seeded wrong category, and an invented refund, a phone number and an order the model
// was not shown all refused.
func TestTicketTriageRecorded(t *testing.T) {
	s, ok := eval.Get(triage.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 16)
	rp := eval.NewReplay(s.Feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	e := eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}

	rep, err := eval.Run(context.Background(), s, cases, e, "recorded")
	require.NoError(t, err)
	assert.Equal(t, 15, rep.Passed, rep.Failures)
	assert.True(t, rep.OK)
	require.Len(t, rep.Failures, 1)
	assert.Equal(t, "wrong_category_credits", rep.Failures[0].Case)

	a, err := triageeval.Measure(context.Background(), e, cases)
	require.NoError(t, err)
	assert.Equal(t, triageeval.Accuracy{Shown: 13, Right: 12}, a)
	assert.GreaterOrEqual(t, a.Score(), triageeval.AccuracyBar)
}
