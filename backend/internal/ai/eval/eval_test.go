package eval_test

import (
	"context"
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/eval/selftest"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

func replayEnv(t *testing.T, feature string, cases []eval.Case) eval.Env {
	t.Helper()
	rp := eval.NewReplay(feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	return eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}
}

// SC-AI-06 and SC-AI-07: the recorded selftest passes in full: the scrubber keeps a phone out
// of the prompt, an invented or phone-derived quantity is rejected by the grounding check, and
// schema, refusal, truncation and missing tool calls are rejected, never stored.
func TestSelftestRecorded(t *testing.T) {
	s, ok := eval.Get(selftest.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 12)
	rep, err := eval.Run(context.Background(), s, cases, replayEnv(t, s.Feature, cases), "recorded")
	require.NoError(t, err)
	assert.Equal(t, 12, rep.Passed, rep.Failures)
	assert.True(t, rep.OK)
	assert.Positive(t, rep.CostPaise)
}

// A case whose expectation the model does not meet fails, and one failure is under a bar of 1.
func TestBelowBar(t *testing.T) {
	s, _ := eval.Get(selftest.Feature)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	for i := range cases {
		if cases[i].Name == "en_two_items" {
			cases[i].Expect = json.RawMessage(`{"lines":[{"name":"rice","qty":11,"unit":"kg"}]}`)
		}
	}
	rep, err := eval.Run(context.Background(), s, cases, replayEnv(t, s.Feature, cases), "recorded")
	require.NoError(t, err)
	assert.False(t, rep.OK)
	require.Len(t, rep.Failures, 1)
	assert.Equal(t, "en_two_items", rep.Failures[0].Case)
}

func TestLoadCases(t *testing.T) {
	c := `{"name":"a_case","input":{},"expect":{},"reply":{}}`
	s := eval.Suite{Feature: "x", Fixtures: fstest.MapFS{"a.json": {Data: []byte(c)}, "b.json": {Data: []byte(c)}}}
	_, err := eval.LoadCases(s, true)
	assert.Error(t, err, "repeated name")

	s.Fixtures = fstest.MapFS{"a.json": {Data: []byte(`{"name":"no_reply","input":{},"expect":{}}`)}}
	_, err = eval.LoadCases(s, true)
	assert.Error(t, err, "recorded needs a reply")
	got, err := eval.LoadCases(s, false)
	require.NoError(t, err, "live does not")
	assert.Len(t, got, 1)

	s.Fixtures = fstest.MapFS{"a.json": {Data: []byte(`{"name":"Bad Name"}`)}}
	_, err = eval.LoadCases(s, false)
	assert.Error(t, err)
	s.Fixtures = fstest.MapFS{}
	_, err = eval.LoadCases(s, false)
	assert.Error(t, err)
}

func TestRegister(t *testing.T) {
	s, _ := eval.Get(selftest.Feature)
	assert.Panics(t, func() { eval.Register(s) }, "twice")
	assert.Panics(t, func() { eval.Register(eval.Suite{Feature: "list_parse", PassBar: 0, Fixtures: s.Fixtures, Run: s.Run}) })
	assert.Contains(t, eval.Features(), selftest.Feature)
}

// The replay answers only the case its request names.
func TestReplayRefusesUnknownCase(t *testing.T) {
	env := replayEnv(t, "ai_selftest", nil)
	_, err := env.LLM.Complete(context.Background(), client.Request{Feature: "ai_selftest", Ref: "nope", Model: "recorded",
		Messages: []client.Message{client.UserText("x")}, MaxTokens: 10})
	var pe *client.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 400, pe.Status)
}
