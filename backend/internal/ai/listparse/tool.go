package listparse

import (
	"context"
	"embed"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/ai/scrub"
)

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

func promptVersion() string { return prompts.Version(Feature) }

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "record_list_items",
	Description: "Record the items of the customer's shopping list, one line per item.",
	Schema: schema.Object("", map[string]schema.S{
		"lines": schema.Array("The items, in the list's order", schema.Object("", map[string]schema.S{
			"text":     schema.String("The item as written, with brand and pack size", MaxText),
			"quantity": schema.Integer("How many of the item; 1 when the list gives no count", 1, MaxQuantity),
		}), MaxLines),
	}),
}

// ToolLine is one line of the tool input.
type ToolLine struct {
	Text     string `json:"text"`
	Quantity int    `json:"quantity"`
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Lines []ToolLine `json:"lines"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if len(o.Lines) > MaxLines {
		return fmt.Errorf("%d lines", len(o.Lines))
	}
	for i, l := range o.Lines {
		if n := utf8.RuneCountInString(strings.TrimSpace(l.Text)); n == 0 || n > MaxText {
			return fmt.Errorf("line %d text length", i)
		}
		if l.Quantity < 1 || l.Quantity > MaxQuantity {
			return fmt.Errorf("line %d quantity", i)
		}
	}
	return nil
}

// Line is one stored draft line.
type Line struct {
	LineID   string `json:"lineId"`
	Text     string `json:"text"`
	Quantity int    `json:"quantity"`
}

// Output is a ready draft's output.
type Output struct {
	Lines []Line `json:"lines"`
}

// Draft is what the model made of one list.
type Draft struct {
	Output Output
	Resp   client.Response
	Reject string
	// Dropped counts the lines refused for a number the list does not hold, a personal
	// value, or being past maxLines.
	Dropped int
}

// Clean is the model's view of a list: scrubbed of phones, codes and ids, trimmed.
func Clean(text string) string {
	return strings.TrimSpace(scrub.Text(text, scrub.Options{}).Text)
}

// Parse runs the model on one list (already Clean). maxLines is basket_max_items.
func Parse(ctx context.Context, llm client.LLM, model, ref, list string, maxLines int) (Draft, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Draft{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 2048,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Text: "The shopping list:\n" + list},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Draft{Resp: resp, Reject: reject}, err
	}
	d := Draft{Resp: resp, Output: Output{Lines: []Line{}}}
	for _, l := range out.Lines {
		text := strings.Join(strings.Fields(l.Text), " ")
		if len(schema.Ungrounded(text, list)) > 0 || !scrub.Clean(text) || !QuantityGrounded(l.Quantity, list) {
			d.Dropped++
			continue
		}
		if len(d.Output.Lines) >= maxLines {
			d.Dropped++
			continue
		}
		d.Output.Lines = append(d.Output.Lines, Line{
			LineID: "l" + strconv.Itoa(len(d.Output.Lines)+1), Text: text, Quantity: l.Quantity,
		})
	}
	if len(d.Output.Lines) == 0 {
		d.Reject = retail.FailEmpty
		if d.Dropped > 0 {
			client.RecordRejected(ctx, Feature, client.RejectGrounding)
			d.Reject = client.RejectGrounding
		}
		d.Output = Output{}
	}
	return d, nil
}

// QuantityGrounded reports whether q is in the list: 1 (no count given) always is; any other
// count must be in it as digits (any script) or as a number word of the five languages.
func QuantityGrounded(q int, list string) bool {
	if q == 1 {
		return true
	}
	want := strconv.Itoa(q)
	for _, n := range schema.Numbers(list) {
		if n == want {
			return true
		}
	}
	for _, tok := range strings.FieldsFunc(strings.ToLower(list), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r)
	}) {
		if wordValue(tok) == q {
			return true
		}
	}
	return false
}

// numberWords are the counts 2 to 12 as people write them in a list: English, then Hindi,
// Kannada, Telugu and Tamil in Latin letters and in their own script. A word in an Indian
// script matches as a prefix (Kannada and Tamil add case endings: "ಎರಡು", "ಎರಡನ್ನು").
var numberWords = map[int][]string{
	2:  {"two", "pair", "do", "dho", "eradu", "rendu", "irandu", "दो", "ಎರಡು", "రెండు", "இரண்டு"},
	3:  {"three", "teen", "mooru", "muru", "moodu", "mudu", "moonu", "moondru", "moonru", "तीन", "ಮೂರು", "మూడు", "மூன்று"},
	4:  {"four", "char", "chaar", "naalku", "nalku", "naalugu", "nalugu", "naangu", "nangu", "चार", "ನಾಲ್ಕು", "నాలుగు", "நான்கு"},
	5:  {"five", "paanch", "panch", "aidu", "ayidu", "ainthu", "aindhu", "anju", "पांच", "पाँच", "ಐದು", "ఐదు", "ஐந்து"},
	6:  {"six", "chhe", "chhah", "chah", "aaru", "aru", "छह", "छः", "छे", "ಆರು", "ఆరు", "ஆறு"},
	7:  {"seven", "saat", "elu", "eelu", "edu", "yedu", "ezhu", "सात", "ಏಳು", "ఏడు", "ஏழு"},
	8:  {"eight", "aath", "aat", "entu", "enimidi", "ettu", "आठ", "ಎಂಟು", "ఎనిమిది", "எட்டு"},
	9:  {"nine", "nau", "ombattu", "tommidi", "onbathu", "onbadhu", "नौ", "ಒಂಬತ್ತು", "తొమ్మిది", "ஒன்பது"},
	10: {"ten", "das", "dus", "hattu", "padi", "pathu", "paththu", "दस", "ಹತ್ತು", "పది", "பத்து"},
	12: {"dozen", "darjan", "darzan", "dazan", "दर्जन", "ಡಜನ್", "డజన్", "டஜன்"},
}

var wordIndex = func() map[string]int {
	m := map[string]int{}
	for n, ws := range numberWords {
		for _, w := range ws {
			m[w] = n
		}
	}
	return m
}()

func wordValue(tok string) int {
	if n, ok := wordIndex[tok]; ok {
		return n
	}
	if isLatin(tok) {
		return 0
	}
	for w, n := range wordIndex {
		if !isLatin(w) && strings.HasPrefix(tok, w) {
			return n
		}
	}
	return 0
}

func isLatin(s string) bool {
	for _, r := range s {
		if r > unicode.MaxLatin1 {
			return false
		}
	}
	return true
}
