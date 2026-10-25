package riskeval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/riskexplain"
	_ "github.com/todayz/backend/internal/ai/riskexplain/riskeval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

// SC-AI-63: the recorded risk_summary suite is 100% grounded: one summary per risk rule, with
// the subject's earlier flags, no id reaching the model, and a worked-out difference, a
// rounded percent, a phone number and an empty answer all refused.
func TestRiskSummaryRecorded(t *testing.T) {
	s, ok := eval.Get(riskexplain.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 11)
	rp := eval.NewReplay(s.Feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	env := eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}
	rep, err := eval.Run(context.Background(), s, cases, env, "recorded")
	require.NoError(t, err)
	assert.Equal(t, 11, rep.Passed, rep.Failures)
	assert.True(t, rep.OK)
}
