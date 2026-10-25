package bannercopy

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
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

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "draft_banner_copy",
	Description: "Record three banner copy options.",
	Schema: schema.Object("", map[string]schema.S{
		"options": schema.Array("Exactly three different options", schema.Object("", map[string]schema.S{
			"headline":    schema.String("Headline in the shop's language", MaxHeadline),
			"subline":     schema.String("Sub-line in the shop's language, or empty", MaxSubline),
			"headline_en": schema.String("The headline in English", MaxHeadline),
			"subline_en":  schema.String("The sub-line in English, or empty", MaxSubline),
			"tint":        schema.Enum("The banner tint", Tints...),
			"item":        schema.Nullable(schema.Integer("The number of the item for the photo, or null", 1, MaxItems)),
		}), Options),
	}),
}

// ToolOption is one option of the tool input.
type ToolOption struct {
	Headline   string `json:"headline"`
	Subline    string `json:"subline"`
	HeadlineEn string `json:"headline_en"`
	SublineEn  string `json:"subline_en"`
	Tint       string `json:"tint"`
	Item       *int   `json:"item"`
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Options []ToolOption `json:"options"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if len(o.Options) == 0 || len(o.Options) > Options {
		return fmt.Errorf("%d options", len(o.Options))
	}
	for i, op := range o.Options {
		for _, s := range []string{op.Headline, op.HeadlineEn} {
			if strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > MaxHeadline {
				return fmt.Errorf("option %d: headline", i)
			}
		}
		for _, s := range []string{op.Subline, op.SublineEn} {
			if utf8.RuneCountInString(s) > MaxSubline {
				return fmt.Errorf("option %d: subline", i)
			}
		}
		if !slices.Contains(Tints, op.Tint) {
			return fmt.Errorf("option %d: tint", i)
		}
		if op.Item != nil && (*op.Item < 1 || *op.Item > MaxItems) {
			return fmt.Errorf("option %d: item", i)
		}
	}
	return nil
}

// Offer is the offer's terms as the model sees them (amounts in rupees as text).
type Offer struct {
	Title       *string  `json:"title,omitempty"`
	Kind        string   `json:"kind"` // percent, flat, bxgy, bill_min
	PercentOff  *int64   `json:"percentOff,omitempty"`
	RupeesOff   *string  `json:"rupeesOff,omitempty"`
	Buy         *int32   `json:"buy,omitempty"`
	Get         *int32   `json:"get,omitempty"`
	MinBill     *string  `json:"minBillRupees,omitempty"`
	AllItems    bool     `json:"wholeShop"`
	EndsOn      *string  `json:"endsOn,omitempty"` // IST date, "25 Oct"
	StoreItemID []string `json:"-"`
}

// Item is one item as the model sees it, numbered from 1.
type Item struct {
	N     int     `json:"n"`
	Name  string  `json:"name"`
	Price string  `json:"priceRupees"`
	MRP   *string `json:"mrpRupees,omitempty"`
	ID    string  `json:"-"`
}

// Facts are everything the model is given.
type Facts struct {
	Shop     string `json:"shop"`
	Language string `json:"language"`
	Offer    *Offer `json:"offer,omitempty"`
	Items    []Item `json:"items"`
}

// Line is one stored option.
type Line struct {
	LineID      string  `json:"lineId"`
	Headline    string  `json:"headline"`
	Subline     string  `json:"subline"`
	HeadlineEn  string  `json:"headlineEn"`
	SublineEn   string  `json:"sublineEn"`
	Tint        string  `json:"tint"`
	StoreItemID *string `json:"storeItemId"`
}

// Output is a ready draft's output.
type Output struct {
	Language string `json:"language"`
	Lines    []Line `json:"lines"`
}

// Draft is what the model made of the facts.
type Draft struct {
	Output  Output
	Dropped int // options dropped as ungrounded
	Resp    client.Response
	Reject  string
}

// Rupees is paise as the model reads them: "120", or "99.50".
func Rupees(p int64) string {
	if p%100 == 0 {
		return strconv.FormatInt(p/100, 10)
	}
	return fmt.Sprintf("%d.%02d", p/100, p%100)
}

// Write runs the model on the facts and keeps the grounded options.
func Write(ctx context.Context, llm client.LLM, model, ref string, f Facts) (Draft, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Draft{}, err
	}
	if f.Items == nil {
		f.Items = []Item{}
	}
	facts, err := json.Marshal(f)
	if err != nil {
		return Draft{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 1024,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Text: "The facts (JSON):\n" + string(facts)},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Draft{Resp: resp, Reject: reject}, err
	}
	d := Draft{Output: Output{Language: f.Language, Lines: []Line{}}, Resp: resp}
	for _, op := range out.Options {
		texts := []string{op.Headline, op.Subline, op.HeadlineEn, op.SublineEn}
		grounded := true
		for _, t := range texts {
			if len(schema.Ungrounded(t, string(facts))) > 0 || !scrub.Clean(t) {
				grounded = false
			}
		}
		if !grounded {
			d.Dropped++
			continue
		}
		l := Line{
			LineID:   "l" + strconv.Itoa(len(d.Output.Lines)+1),
			Headline: strings.TrimSpace(op.Headline), Subline: strings.TrimSpace(op.Subline),
			HeadlineEn: strings.TrimSpace(op.HeadlineEn), SublineEn: strings.TrimSpace(op.SublineEn),
			Tint: op.Tint,
		}
		if op.Item != nil && *op.Item >= 1 && *op.Item <= len(f.Items) {
			id := f.Items[*op.Item-1].ID
			l.StoreItemID = &id
		}
		d.Output.Lines = append(d.Output.Lines, l)
	}
	if len(d.Output.Lines) == 0 {
		client.RecordRejected(ctx, Feature, client.RejectGrounding)
		return Draft{Resp: resp, Reject: client.RejectGrounding}, nil
	}
	return d, nil
}
