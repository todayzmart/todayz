package shelfeval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/shelfscan"
	"github.com/todayz/backend/internal/ai/shelfscan/shelfeval"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/settings"
)

// SC-AI-15: the recorded shelf scan finds at least RecallBar of the products on 40 photos.
func TestShelfPhotoRecorded(t *testing.T) {
	s, ok := eval.Get(shelfscan.Feature)
	require.True(t, ok)
	cases, err := eval.LoadCases(s, true)
	require.NoError(t, err)
	require.Len(t, cases, 40)
	rp := eval.NewReplay(s.Feature, cases)
	t.Cleanup(rp.Close)
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true})
	env := eval.Env{
		LLM:   client.NewMetered(client.NewAnthropic(rp.URL(), config.Secret{}, hc), clock.Real{}),
		Model: func(settings.Key) string { return "recorded" },
	}
	f, err := shelfeval.Score(context.Background(), env, cases)
	require.NoError(t, err)
	t.Logf("found %d of %d products (%.1f%%), %d extra", f.Found, f.Total, 100*f.Recall(), f.Extra)
	assert.GreaterOrEqual(t, f.Recall(), shelfeval.RecallBar)

	rep, err := eval.Run(context.Background(), s, cases, env, "recorded")
	require.NoError(t, err)
	assert.True(t, rep.OK, rep.Failures)
}

func TestMatchNeedsEveryWord(t *testing.T) {
	str := func(s string) *string { return &s }
	got := []shelfscan.Line{
		{Name: "Noodles", Brand: str("Maggi"), Size: str("70g")},
		{Name: "Horlicks", Brand: str("Horlicks"), Size: str("1kg")},
	}
	f := shelfeval.Match([]string{"Maggi Noodles 70g", "Horlicks 500g"}, got)
	assert.Equal(t, shelfeval.Found{Found: 1, Total: 2, Extra: 1}, f)
}
