package catalogueeval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/catalogue"
	"github.com/todayz/backend/internal/ai/catalogue/catalogueeval"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

func env(t *testing.T, s eval.Suite, cases []eval.Case) eval.Env {
	rp := eval.NewReplay(s.Feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	return eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}
}

// SC-AI-57: the recorded catalogue_cleanup suite meets 11 §4's duplicate precision bar (≥ 90%):
// ten right merges and one seeded wrong one (a different pack size), three right answers of
// none, an invented pack size refused as grounding and a Hindi name in Latin letters as schema.
func TestCatalogueCleanupRecorded(t *testing.T) {
	s, ok := eval.Get(catalogue.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 16)
	e := env(t, s, cases)

	rep, err := eval.Run(context.Background(), s, cases, e, "recorded")
	require.NoError(t, err)
	assert.Equal(t, 15, rep.Passed, rep.Failures)
	assert.True(t, rep.OK)
	require.Len(t, rep.Failures, 1)
	assert.Equal(t, "wrong_merge_pack_size", rep.Failures[0].Case)

	p, err := catalogueeval.Measure(context.Background(), e, cases)
	require.NoError(t, err)
	assert.Equal(t, catalogueeval.Precision{Proposed: 11, Right: 10}, p)
	assert.GreaterOrEqual(t, p.Score(), catalogueeval.PrecisionBar)
}
