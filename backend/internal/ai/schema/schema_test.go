package schema

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SC-AI-04: a model output that breaks the schema is rejected; SC-AI-05: a number the input
// does not hold is rejected (11 §1 rules 4 and 5).

func TestUngrounded(t *testing.T) {
	cases := []struct {
		name, text string
		inputs     []string
		want       []string
	}{
		{"same value", "Sales were ₹18,240 today", []string{`{"salesPaise":"18240"}`}, nil},
		{"indian grouping", "₹1,00,000 this month", []string{`{"month": 100000}`}, nil},
		{"decimal equal", "2.50 kg", []string{"2.5"}, nil},
		{"native digits in text", "१२ orders", []string{`{"orders": 12}`}, nil},
		{"native digits in input", "12 orders", []string{"ಆರ್ಡರ್ ೧೨"}, nil},
		{"tamil digits", "௫ items", []string{"5"}, nil},
		{"range is two numbers", "busiest 7-9 pm", []string{`{"from":7,"to":9}`}, nil},
		{"item name grounds a number", "Amul butter 100 g sold out", []string{"Amul butter 100 g"}, nil},
		{"invented", "up 12% on last week", []string{`{"sales": 18240}`}, []string{"12"}},
		{"rounded is invented", "about ₹18,000", []string{"18240"}, []string{"18000"}},
		{"trailing comma", "sold 40, then 41", []string{"40 41"}, nil},
		{"no numbers", "a quiet day", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Ungrounded(c.text, c.inputs...))
		})
	}
}

func TestHasDigits(t *testing.T) {
	assert.True(t, HasDigits("x 3"))
	assert.True(t, HasDigits("೩"))
	assert.False(t, HasDigits("none"))
}

type out struct {
	Name string `json:"name"`
	Qty  int    `json:"qty"`
}

func (o *out) Validate() error {
	if o.Qty < 1 || o.Qty > 10 {
		return errors.New("qty out of range")
	}
	return nil
}

func TestDecode(t *testing.T) {
	var o out
	require.NoError(t, Decode(json.RawMessage(`{"name":"rice","qty":3}`), &o))
	assert.Equal(t, out{Name: "rice", Qty: 3}, o)

	for name, raw := range map[string]string{
		"empty":         ``,
		"unknown field": `{"name":"rice","qty":3,"price":40}`,
		"trailing":      `{"name":"rice","qty":3}{}`,
		"wrong type":    `{"name":"rice","qty":"3"}`,
		"validate":      `{"name":"rice","qty":30}`,
	} {
		t.Run(name, func(t *testing.T) {
			var o out
			assert.ErrorIs(t, Decode(json.RawMessage(raw), &o), ErrInvalid)
		})
	}
}

func TestBuildersAreStrictSafe(t *testing.T) {
	s := Object("Stock lines", map[string]S{
		"lines": Array("lines", Object("", map[string]S{
			"name": String("item", 60),
			"qty":  Number("qty", 0, 100),
			"n":    Integer("n", 1, 9),
			"unit": Nullable(Enum("unit", "kg", "g")),
			"ok":   Bool("ok"),
		}), 20),
	})
	b, err := json.Marshal(s)
	require.NoError(t, err)
	// Strict tool schemas take no bounds: they are in the descriptions only.
	for _, k := range []string{"maxLength", "minimum", "maximum", "maxItems", "minLength", "minItems"} {
		assert.NotContains(t, string(b), `"`+k+`"`)
	}
	assert.Contains(t, string(b), "at most 60 characters")
	assert.Contains(t, string(b), "at most 20 items")
	assert.Contains(t, string(b), `"additionalProperties":false`)
	assert.Contains(t, string(b), `"required":["n","name","ok","qty","unit"]`)
	assert.Contains(t, string(b), `"enum":["kg","g",null]`)
	assert.Contains(t, string(b), `"type":["string","null"]`)
}
