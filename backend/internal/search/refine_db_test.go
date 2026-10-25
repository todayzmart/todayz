package search

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	discoverapi "github.com/todayz/backend/gen/api/discover"
)

// fakeChips reads "under 33" as a ₹33 cap and "open now" as openNow, like the querynorm
// parser cmd/todayz wires (its own tests cover the patterns), and fails on "boom".
type fakeChips struct{}

func (fakeChips) ReadChips(_ context.Context, q string) (string, []RefineChip, error) {
	if strings.Contains(q, "boom") {
		return "", nil, errors.New("boom")
	}
	var chips []RefineChip
	if strings.Contains(q, "under 33") {
		chips = append(chips, RefineChip{Kind: "maxPrice", Value: "3300", Text: "under 33"})
		q = strings.Replace(q, "under 33", "", 1)
	}
	if strings.Contains(q, "open now") {
		chips = append(chips, RefineChip{Kind: "openNow", Text: "open now"})
		q = strings.Replace(q, "open now", "", 1)
	}
	return strings.Join(strings.Fields(q), " "), chips, nil
}

// D-101 (W3-12): the chips read from q apply as filters, the response names them and
// searches what is left; refineHint only when nothing was read from a long query.
func TestRefineChips(t *testing.T) {
	f := newQA(t)
	f.svc.chips = fakeChips{}

	r := f.search("curd under 33")
	assert.Equal(t, []string{"mid", "far"}, resultSlugs(r), "the cap applies")
	assert.Equal(t, "curd", r.Query)
	require.Len(t, r.Chips, 1)
	assert.Equal(t, discoverapi.DiscoverChipKindMaxPrice, r.Chips[0].Kind)
	require.NotNil(t, r.Chips[0].Value)
	assert.Equal(t, "3300", *r.Chips[0].Value)
	assert.Equal(t, "under 33", r.Chips[0].Text)
	assert.False(t, r.RefineHint)

	r = f.search("curd open now")
	require.Len(t, r.Chips, 1)
	assert.Nil(t, r.Chips[0].Value)
	assert.Equal(t, "curd", r.Query)

	// parse=false (a chip removed): q is searched as typed and nothing is read.
	r = f.search("curd under 33", func(p *discoverapi.SearchDiscoverParams) { p.Parse = bp(false) })
	assert.Empty(t, r.Chips)
	assert.Equal(t, "curd under 33", r.Query)
	assert.False(t, r.RefineHint)

	// An explicit cap wins: the chip is neither applied nor shown.
	max := 100000
	r = f.search("curd under 33", func(p *discoverapi.SearchDiscoverParams) { p.MaxPricePaise = &max })
	assert.Empty(t, r.Chips)
	assert.Equal(t, []string{"near", "mid", "far"}, resultSlugs(r))

	// Chips that leave nothing to search for are not read.
	r = f.search("under 33 open now")
	assert.Empty(t, r.Chips)
	assert.Equal(t, "under 33 open now", r.Query)

	// refineHint: no chip in a query of at least refine_ai_min_words (4) words.
	assert.True(t, f.search("curd from the corner shop").RefineHint)
	assert.False(t, f.search("fresh curd").RefineHint, "a short query")
	assert.False(t, f.search("curd from the shop", func(p *discoverapi.SearchDiscoverParams) { p.Parse = bp(false) }).RefineHint)

	// A reader error never fails the search.
	r = f.search("curd boom")
	assert.Empty(t, r.Chips)
	assert.Equal(t, "curd boom", r.Query)
}

// Without a reader (nil Deps.Chips) no chip is read, and refineHint still follows the words.
func TestRefineWithoutReader(t *testing.T) {
	f := newQA(t)
	r := f.search("curd under 33")
	assert.Empty(t, r.Chips)
	assert.Equal(t, "curd under 33", r.Query)
	assert.Empty(t, r.Results, "every word of q is searched for")
	assert.True(t, f.search("curd from the corner shop").RefineHint)
}
