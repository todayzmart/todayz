package catalogue

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/platform/ids"
)

func ptr(s string) *string { return &s }

func input(t *testing.T) Input {
	t.Helper()
	in := Input{
		CategoryID: mustID(t, "01J0000000000000000000000C"),
		Product:    NewProduct("amul butter 500 g", ptr("Amul"), nil, 1, "pc", "c1"),
		Candidates: []Candidate{{ID: mustID(t, "01J0000000000000000000000P"), Ref: "p1", Name: "Butter", BrandName: ptr("Amul"), PackSize: 500, Unit: "g", Similarity: 0.71}},
		Categories: []Category{{ID: mustID(t, "01J0000000000000000000000C"), Ref: "c1", Name: "Dairy"}},
	}
	return in.WithNames(map[string]string{"en": "Dairy", "hi": "डेयरी", "kn": " ", "te": "పాలు", "ta": "பால்"})
}

func reply() ToolOutput {
	return ToolOutput{Name: "Butter", BrandName: ptr("Amul"), PackSize: 500, Unit: "g", CategoryRef: "c1", DuplicateRef: "p1",
		DuplicateReason: ptr("Same Amul butter in the same 500 g pack."),
		CategoryNames:   &Names{En: "Milk", Hi: "दूध", Kn: "ಹಾಲು", Te: "పాలు", Ta: "பால்"}}
}

func TestInScript(t *testing.T) {
	for _, c := range []struct {
		lang, s string
		ok      bool
	}{
		{"en", "Dairy", true},
		{"en", "डेयरी", false},
		{"hi", "डेयरी", true},
		{"hi", "Dairy", false},
		{"kn", "ಹಾಲು", true},
		{"te", "పాలు", true},
		{"ta", "பால்", true},
		{"ta", "పాలు", false},
		{"hi", "123", false}, // no letters at all
	} {
		assert.Equal(t, c.ok, InScript(c.lang, c.s), "%s %q", c.lang, c.s)
	}
}

func TestWithNames(t *testing.T) {
	in := input(t)
	assert.True(t, in.NamesWanted)
	assert.Equal(t, []string{"kn"}, in.Missing(), "a blank name is missing")
	full := in.WithNames(map[string]string{"en": "Dairy", "hi": "डेयरी", "kn": "ಹಾಲು", "te": "పాలు", "ta": "பால்"})
	assert.False(t, full.NamesWanted)
	assert.Nil(t, full.NamesNow, "the model is not shown names it is not asked for")
}

func TestBuild(t *testing.T) {
	in := input(t)
	d, reject := Build(in, reply())
	require.Empty(t, reject)
	require.Len(t, d.Output.Lines, 3)
	norm, _ := d.Output.Line(KindNormalise)
	assert.Equal(t, "01J0000000000000000000000C", norm.CategoryID)
	assert.Equal(t, float64(500), norm.PackSize, "a pack written in the name is grounded")
	dup, _ := d.Output.Line(KindDuplicate)
	assert.Equal(t, "01J0000000000000000000000P", dup.ProductID)
	names, _ := d.Output.Line(KindCategoryNames)
	assert.Equal(t, "ಹಾಲು", names.Names.Kn)
	assert.Equal(t, "Dairy", names.Names.En, "a language the category has keeps its name")
	assert.Equal(t, []string{"01J0000000000000000000000P"}, d.Output.Candidates)

	for name, c := range map[string]struct {
		edit   func(*ToolOutput)
		reject string
	}{
		"unknown category":     {func(o *ToolOutput) { o.CategoryRef = "c9" }, client.RejectSchema},
		"unknown duplicate":    {func(o *ToolOutput) { o.DuplicateRef = "p4" }, client.RejectSchema},
		"duplicate, no reason": {func(o *ToolOutput) { o.DuplicateReason = nil }, client.RejectSchema},
		"guessed pack":         {func(o *ToolOutput) { o.PackSize = 250 }, client.RejectGrounding},
		"number in the name":   {func(o *ToolOutput) { o.Name = "Butter 250" }, client.RejectGrounding},
		"number in the reason": {func(o *ToolOutput) { o.DuplicateReason = ptr("Same butter, 99% alike.") }, client.RejectGrounding},
		"name in Latin":        {func(o *ToolOutput) { o.CategoryNames.Kn = "Haalu" }, client.RejectSchema},
		"phone in the reason":  {func(o *ToolOutput) { o.DuplicateReason = ptr("Call 9876543210 to check.") }, client.RejectGrounding},
	} {
		out := reply()
		names := *out.CategoryNames
		out.CategoryNames = &names
		c.edit(&out)
		_, reject := Build(in, out)
		assert.Equal(t, c.reject, reject, name)
	}

	// No duplicate, and no names while the category has every language.
	out := reply()
	out.DuplicateRef, out.DuplicateReason = NoDuplicate, nil
	full := in.WithNames(map[string]string{"en": "Dairy", "hi": "डेयरी", "kn": "ಹಾಲು", "te": "పాలు", "ta": "பால்"})
	d, reject = Build(full, out)
	require.Empty(t, reject)
	require.Len(t, d.Output.Lines, 1)
}

func mustID(t *testing.T, s string) ids.ID {
	t.Helper()
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}
