package voicesearcheval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/voicesearch"
	"github.com/todayz/backend/internal/ai/voicesearch/voicesearcheval"
	"github.com/todayz/backend/internal/platform/settings"
)

// SC-AI-45: the recorded voice_search suite (100 speech replies, five languages and mixed)
// meets its bar: the transcript for the search box, its language and the refine chips read
// from it, with no model call.
func TestVoiceSearchRecorded(t *testing.T) {
	s, ok := eval.Get(voicesearch.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 100)
	rep, err := eval.Run(context.Background(), s, cases, eval.Env{Model: func(settings.Key) string { return "recorded" }}, "recorded")
	require.NoError(t, err)
	t.Logf("voice_search: %d/%d exact", rep.Passed, rep.Cases)
	assert.True(t, rep.OK, rep.Failures)
	assert.GreaterOrEqual(t, rep.Score, voicesearcheval.PassBar)
	assert.Zero(t, rep.CostPaise, "voice search calls no model")
}
