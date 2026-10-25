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

// D-230: a percent needs a percent fact, an amount a money fact, and an
// item's place in the list grounds nothing.
func TestUngroundedFacts(t *testing.T) {
	banner := `{"shop":"Om Sai","offer":{"kind":"percent","percentOff":10,"endsOn":"25 Oct"},` +
		`"items":[{"n":1,"name":"Tata Salt 1kg","priceRupees":"28"},{"n":2,"name":"Lux 100g","priceRupees":"38","mrpRupees":"40"}]}`
	cases := []struct {
		name, text, facts string
		extra             []string
		want              []string
	}{
		{"percent fact", "10% off everything", banner, nil, nil},
		{"percent word", "10 percent off", banner, nil, nil},
		{"money fact", "Tata Salt at ₹28", banner, nil, nil},
		{"rs prefix", "Lux at Rs.38, MRP Rs 40", banner, nil, nil},
		{"rupees suffix", "only 28 rupees", banner, nil, nil},
		{"slash dash", "Salt 28/-", banner, nil, nil},
		{"plain matches any unit", "10 off, 28 each", banner, nil, nil},
		{"percent is not money", "₹10 off", banner, nil, []string{"10"}},
		{"money is not percent", "28% off", banner, nil, []string{"28"}},
		{"index grounds nothing", "2% off Lux", banner, nil, []string{"2"}},
		{"index plain", "Buy 2", banner, nil, []string{"2"}},
		{"date is not percent", "25% off", banner, nil, []string{"25"}},
		{"date plain", "till 25 Oct", banner, nil, nil},
		{"name number plain", "1kg packs", banner, nil, nil},
		{"name number is not money", "₹1 only", banner, nil, []string{"1"}},
		{"paise as rupees", "₹146 nearby", `{"refPaise":14600}`, nil, nil},
		{"paise string", "sales ₹182.40", `{"salesPaise":"18240"}`, nil, nil},
		{"coach pct", "up 12%, ₹4,200 in sales", `{"pct":12,"sales":"4200","bills":31}`, nil, nil},
		{"count is not percent", "31% more", `{"pct":12,"sales":"4200","bills":31}`, nil, []string{"31"}},
		{"buy get plain", "Buy 2 get 1", `{"kind":"bxgy","buy":2,"get":1}`, nil, nil},
		{"hours is not rs", "open 5 hours 5 days", `{"days":5}`, nil, nil},
		{"native digits", "१०% छूट", banner, nil, nil},
		{"kannada percent", "ಶೇ 10 ರಿಯಾಯಿತಿ", banner, nil, nil},
		{"extra review text", "sorry about the ₹50 charge", `{"stars":2}`, []string{"charged me ₹50"}, nil},
		{"extra plain is not money", "₹2 back", `{"stars":2}`, []string{"waited 2 days"}, []string{"2"}},
		{"bad facts ground nothing", "3 left", `{`, nil, []string{"3"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, UngroundedFacts(c.text, []byte(c.facts), c.extra...))
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
