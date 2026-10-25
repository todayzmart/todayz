package coacheval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/coach"
	_ "github.com/todayz/backend/internal/coach/coacheval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

// SC-AI-14: the recorded coach_card suite passes in full: grounded rewrites in all five
// languages (native digits too) are used, and an invented number, a worked-out sum, a schema
// break, a missing tool call and a refusal are rejected, so the card keeps its template.
func TestCoachCardRecorded(t *testing.T) {
	s, ok := eval.Get(coach.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 14)
	rp := eval.NewReplay(s.Feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	env := eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}
	rep, err := eval.Run(context.Background(), s, cases, env, "recorded")
	require.NoError(t, err)
	assert.Equal(t, 14, rep.Passed, rep.Failures)
	assert.True(t, rep.OK)
}
