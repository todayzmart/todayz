package search

import (
	"testing"

	"github.com/stretchr/testify/assert"

	discoverapi "github.com/todayz/backend/gen/api/discover"
)

// D-238: refineHint also says the synonym table missed. The table is small, so a miss alone
// is not enough: the words must also be unknown to the listings, the trigram fallback, the
// category names and the master catalogue.
func TestRefineSynonymMiss(t *testing.T) {
	f := newQA(t)
	f.svc.chips = fakeChips{}
	near := f.idOf(`SELECT id FROM stores WHERE slug = 'near'`)
	maggi := f.product("Maggi Masala Noodles", nil, catGrocery, 70, "g")
	f.item(near, maggi, 1400, 6, 0, nil)
	f.product("Parle G Biscuits", nil, catGrocery, 80, "g") // in the catalogue, stocked nowhere

	// A hit: the table knows the word (with or without a result).
	assert.False(t, f.search("dahi").RefineHint, "synonym of curd")
	assert.False(t, f.search("doodh").RefineHint, "synonym of milk, nothing stocked")
	assert.False(t, f.search("paneer").RefineHint, "a term with no listing")

	// A miss that the stock or the catalogue recognises is an ordinary query.
	assert.False(t, f.search("maggi").RefineHint, "stocked nearby")
	assert.False(t, f.search("Maggi Masala Noodles").RefineHint, "a master product name")
	assert.False(t, f.search("parle g").RefineHint, "in the catalogue, stocked nowhere")
	assert.False(t, f.search("nandini").RefineHint, "a brand with stock, not in the table")
	assert.False(t, f.search("maggi", func(p *discoverapi.SearchDiscoverParams) {
		mx := 100
		p.MaxPricePaise = &mx
	}).RefineHint, "a filter that empties the page does not make the words unknown")

	// A miss nothing recognises: the hint.
	assert.True(t, f.search("kuch meetha chahiye").RefineHint)
	assert.Empty(t, f.search("kuch meetha chahiye").Results)
	assert.True(t, f.search("sasta").RefineHint, "a single unknown word")

	// parse=false (a chip removed, or a suggestion tapped): never.
	assert.False(t, f.search("kuch meetha chahiye", func(p *discoverapi.SearchDiscoverParams) { p.Parse = bp(false) }).RefineHint)

	// An unknown barcode is exact, not unknown words; a known one finds its product.
	assert.False(t, f.search("8901262150999").RefineHint)
	assert.False(t, f.search("8901262150019").RefineHint)

	// Chips: the words left after them count; nothing left is not searched for words.
	r := f.search("kuch meetha under 33")
	assert.Equal(t, "kuch meetha", r.Query)
	assert.Len(t, r.Chips, 1)
	assert.True(t, r.RefineHint, "words remain after the chip and nothing knows them")
	r = f.search("curd under 33")
	assert.False(t, r.RefineHint)
	r = f.search("open now")
	assert.Empty(t, r.Chips)
	assert.False(t, r.RefineHint, "only chips: no words to miss")

	// Only punctuation has nothing to search for.
	assert.False(t, f.search("?!").RefineHint)

	// The parser half still holds on its own: a long query with a table hit and no chip.
	assert.True(t, f.search("curd from the corner shop").RefineHint)
	assert.False(t, f.search("curd from the corner shop", func(p *discoverapi.SearchDiscoverParams) { p.Parse = bp(false) }).RefineHint)
	assert.False(t, f.search("fresh curd").RefineHint)
}
