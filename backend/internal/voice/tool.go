package voice

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/prompts"
	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

// LowConfidence caps a line's confidence when a number on it is not in the transcript the
// model saw (11 §1 rule 6). Spoken numbers are often words ("teen", "barah"), so an
// ungrounded number marks the line for a careful look instead of refusing the draft.
const LowConfidence = 0.5

// maxHeard and maxUnmatched bound the line's words (VoiceStockLine.heard is 120).
const (
	maxHeard     = 120
	maxUnmatched = 60
	// MaxTranscript is the transcript the model sees and the draft keeps (VoiceStockDraft).
	MaxTranscript = 2000 //settings:ok the contract's maxLength for VoiceStockDraft.transcript
	// maxPriceRupees is MaxPricePaise in rupees.
	maxPriceRupees = MaxPricePaise / 100
)

// Tool is the strict tool the model answers with (02 §8 step 3). Quantities are in the item's
// own selling units (a pack, a piece, a kg as the item is sold), so the line carries no unit.
var Tool = client.Tool{
	Name:        "record_voice_stock_lines",
	Description: "Record each stock change the shop owner spoke: a sale, a restock, a count or a new price.",
	Schema: schema.Object("", map[string]schema.S{
		"lines": schema.Array("One line per item and change, in the order spoken", schema.Object("", map[string]schema.S{
			"kind":         schema.Enum("sale: sold; restock: received; count: the number on the shelf now (out of stock is 0); set_price: a new selling price", Kinds...),
			"item":         schema.Nullable(schema.String("The candidate ref (c1, c2, …) of the item, or null when no candidate matches", 6)),
			"unmatched":    schema.Nullable(schema.String("When item is null: the item's name as spoken; otherwise null", maxUnmatched)),
			"heard":        schema.String("The words of the transcript this line came from", maxHeard),
			"quantity":     schema.Nullable(schema.Integer("For sale and restock from 1, for count from 0; null for set_price", 0, MaxQuantity)),
			"price_rupees": schema.Nullable(schema.Number("For set_price only: the new price in rupees; otherwise null", 0.01, maxPriceRupees)),
			"confidence":   schema.Number("How sure you are of the item, kind and number together", 0, 1),
		}), MaxLines),
	}),
}

// ToolLine is one line of the tool input.
type ToolLine struct {
	Kind        string   `json:"kind"`
	Item        *string  `json:"item"`
	Unmatched   *string  `json:"unmatched"`
	Heard       string   `json:"heard"`
	Quantity    *int     `json:"quantity"`
	PriceRupees *float64 `json:"price_rupees"`
	Confidence  float64  `json:"confidence"`
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Lines []ToolLine `json:"lines"`
}

var reRef = regexp.MustCompile(`^c[1-9][0-9]{0,3}$`)

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if len(o.Lines) > MaxLines {
		return fmt.Errorf("%d lines", len(o.Lines))
	}
	for i, l := range o.Lines {
		if !slices.Contains(Kinds, l.Kind) {
			return fmt.Errorf("line %d: kind", i)
		}
		if l.Item != nil && !reRef.MatchString(*l.Item) {
			return fmt.Errorf("line %d: item ref", i)
		}
		if l.Item == nil && (l.Unmatched == nil || strings.TrimSpace(*l.Unmatched) == "") {
			return fmt.Errorf("line %d: neither item nor unmatched", i)
		}
		if l.Unmatched != nil && utf8.RuneCountInString(*l.Unmatched) > maxUnmatched {
			return fmt.Errorf("line %d: unmatched length", i)
		}
		if l.Heard == "" || utf8.RuneCountInString(l.Heard) > maxHeard {
			return fmt.Errorf("line %d: heard length", i)
		}
		if l.Confidence < 0 || l.Confidence > 1 || math.IsNaN(l.Confidence) {
			return fmt.Errorf("line %d: confidence", i)
		}
		if err := checkNumbers(l.Kind, l.Quantity, l.PriceRupees); err != nil {
			return fmt.Errorf("line %d: %w", i, err)
		}
	}
	return nil
}

// checkNumbers is the per-kind rule for a line's numbers.
func checkNumbers(kind string, qty *int, rupees *float64) error {
	switch kind {
	case KindSale, KindRestock, KindCount:
		lo := 1
		if kind == KindCount {
			lo = 0
		}
		if qty == nil || *qty < lo || *qty > MaxQuantity {
			return errors.New("quantity out of range")
		}
		if rupees != nil {
			return errors.New("price on a " + kind + " line")
		}
	case KindSetPrice:
		if qty != nil {
			return errors.New("quantity on a set_price line")
		}
		if rupees == nil || math.IsNaN(*rupees) || *rupees < 0.01 || *rupees > maxPriceRupees {
			return errors.New("price out of range")
		}
		if p := *rupees * 100; math.Abs(p-math.Round(p)) > 1e-6 {
			return errors.New("price has more than two decimals")
		}
	}
	return nil
}

// Candidate is one item the model may match.
type Candidate struct {
	Ref  string // c1, c2, … in list order
	ID   ids.ID
	Name string // the shop's name, variant and pack ("Amul Butter 100 g")
}

// Line is one stored draft line (the output's "lines"; VoiceStockLine without the values
// read at review time).
type Line struct {
	LineID      string  `json:"lineId"`
	Kind        string  `json:"kind"`
	StoreItemID *string `json:"storeItemId"`
	ItemName    *string `json:"itemName"` // the matched name, or the unmatched words
	Heard       string  `json:"heard"`
	Quantity    *int    `json:"quantity"`
	PricePaise  *int64  `json:"pricePaise"`
	Confidence  float64 `json:"confidence"`
}

// Matched reports whether the line names an item.
func (l Line) Matched() bool { return l.StoreItemID != nil }

// Output is a ready draft's output.
type Output struct {
	Transcript string `json:"transcript"`
	Language   string `json:"language,omitempty"`
	Lines      []Line `json:"lines"`
}

// Parsed is what Parse made of one transcript.
type Parsed struct {
	Lines  []Line
	Resp   client.Response
	Reject string // "" or why the output was refused: a client outcome or schema
}

// Model is how the model id is read (the ai_model_fast setting; eval.Env.Model).
type Model func(settings.Key) string

// Parse runs the model on a scrubbed transcript with the store's candidates and turns the
// tool call into draft lines. A provider error the request caused (4xx other than 429) is
// returned as an error; every other failure is a Reject so the draft fails with its code.
func Parse(ctx context.Context, llm client.LLM, model Model, transcript string, cands []Candidate) (Parsed, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Parsed{}, err
	}
	resp, err := llm.Complete(ctx, client.Request{
		Feature: Feature, Model: model(settings.AiModelFast), System: sys,
		Messages: []client.Message{client.UserText(userText(transcript, cands))}, Tool: &Tool, MaxTokens: 2048,
	})
	if err != nil {
		var pe *client.ProviderError
		if errors.As(err, &pe) && !pe.Retryable() && pe.Status != 429 {
			return Parsed{Resp: resp}, err
		}
		return Parsed{Resp: resp, Reject: client.Outcome(err)}, nil
	}
	var out ToolOutput
	if derr := schema.Decode(resp.ToolInput, &out); derr != nil {
		// A reply outside the tool's schema is a rejected draft, not a server error.
		client.RecordRejected(ctx, Feature, client.RejectSchema)
		return Parsed{Resp: resp, Reject: client.RejectSchema}, nil //nolint:nilerr // the draft fails with "schema"
	}
	return Parsed{Lines: toLines(out, transcript, cands), Resp: resp}, nil
}

// userText is the user message: the candidates, then the transcript.
func userText(transcript string, cands []Candidate) string {
	var b strings.Builder
	b.WriteString("<items>\n")
	for _, c := range cands {
		b.WriteString(c.Ref)
		b.WriteString(" | ")
		b.WriteString(c.Name)
		b.WriteByte('\n')
	}
	b.WriteString("</items>\n<transcript>\n")
	b.WriteString(transcript)
	b.WriteString("\n</transcript>")
	return b.String()
}

// toLines maps refs to items and rupees to paise, and caps the confidence of a line whose
// number is not in the transcript or the item's name.
func toLines(out ToolOutput, transcript string, cands []Candidate) []Line {
	byRef := make(map[string]Candidate, len(cands))
	for _, c := range cands {
		byRef[c.Ref] = c
	}
	lines := make([]Line, 0, len(out.Lines))
	for i, tl := range out.Lines {
		l := Line{
			LineID: "l" + strconv.Itoa(i+1), Kind: tl.Kind, Heard: tl.Heard,
			Quantity: tl.Quantity, Confidence: math.Round(tl.Confidence*100) / 100,
		}
		grounding := []string{transcript}
		if tl.Item != nil {
			if c, ok := byRef[*tl.Item]; ok {
				id, name := c.ID.String(), c.Name
				l.StoreItemID, l.ItemName = &id, &name
				grounding = append(grounding, c.Name)
			}
		}
		if l.StoreItemID == nil {
			// No candidate, or a ref the list does not have: the person picks the item.
			name := strings.TrimSpace(tl.Heard)
			if tl.Unmatched != nil && strings.TrimSpace(*tl.Unmatched) != "" {
				name = strings.TrimSpace(*tl.Unmatched)
			}
			l.ItemName = &name
			l.Confidence = 0
		}
		var nums []string
		if tl.Quantity != nil && *tl.Quantity > 1 {
			nums = append(nums, strconv.Itoa(*tl.Quantity))
		}
		if tl.PriceRupees != nil {
			p := int64(math.Round(*tl.PriceRupees * 100))
			l.PricePaise = &p
			nums = append(nums, strconv.FormatFloat(*tl.PriceRupees, 'f', -1, 64))
		}
		// A number is checked only when the transcript has digits at all: a transcript in
		// words has nothing to compare with.
		if len(nums) > 0 && len(schema.Numbers(transcript)) > 0 &&
			len(schema.Ungrounded(strings.Join(nums, " "), grounding...)) > 0 {
			l.Confidence = min(l.Confidence, LowConfidence)
		}
		lines = append(lines, l)
	}
	return lines
}
