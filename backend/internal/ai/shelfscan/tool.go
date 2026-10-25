package shelfscan

import (
	"context"
	"embed"
	"fmt"
	"math"
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

const (
	maxName  = 120
	maxBrand = 80
	maxSize  = 40
)

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "record_shelf_products",
	Description: "Record the packaged products seen on one shelf photo.",
	Schema: schema.Object("", map[string]schema.S{
		"products": schema.Array("One entry per distinct product, left to right, top to bottom", schema.Object("", map[string]schema.S{
			"name":       schema.String("The product name as printed on the pack", maxName),
			"brand":      schema.Nullable(schema.String("The brand as printed, or null", maxBrand)),
			"size":       schema.Nullable(schema.String("The pack size as printed, or null", maxSize)),
			"confidence": schema.Number("How sure you are of this product", 0, 1),
		}), MaxLines),
	}),
}

// ToolProduct is one product of the tool input.
type ToolProduct struct {
	Name       string  `json:"name"`
	Brand      *string `json:"brand"`
	Size       *string `json:"size"`
	Confidence float64 `json:"confidence"`
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Products []ToolProduct `json:"products"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if len(o.Products) > MaxLines {
		return fmt.Errorf("%d products", len(o.Products))
	}
	for i, p := range o.Products {
		if strings.TrimSpace(p.Name) == "" || utf8.RuneCountInString(p.Name) > maxName {
			return fmt.Errorf("product %d: name", i)
		}
		if p.Brand != nil && utf8.RuneCountInString(*p.Brand) > maxBrand {
			return fmt.Errorf("product %d: brand", i)
		}
		if p.Size != nil && utf8.RuneCountInString(*p.Size) > maxSize {
			return fmt.Errorf("product %d: size", i)
		}
		if p.Confidence < 0 || p.Confidence > 1 || math.IsNaN(p.Confidence) {
			return fmt.Errorf("product %d: confidence", i)
		}
	}
	return nil
}

// Line is one stored draft line.
type Line struct {
	LineID     string  `json:"lineId"`
	Name       string  `json:"name"`
	Brand      *string `json:"brand"`
	Size       *string `json:"size"`
	Confidence float64 `json:"confidence"`
	ProductID  *string `json:"productId"` // the matched master product, by the server
}

// Output is a ready draft's output.
type Output struct {
	Lines []Line `json:"lines"`
}

// Read is what the model made of one photo.
type Read struct {
	Output Output // lines without matches yet
	Resp   client.Response
	Reject string
}

// ReadShelf runs the model on one shelf photo. ref is the draft id (a case name in the eval).
func ReadShelf(ctx context.Context, llm client.LLM, model, ref string, img client.Image) (Read, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Read{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 2048,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Image: &img}, {Text: "Record the products on this shelf."},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Read{Resp: resp, Reject: reject}, err
	}
	return Read{Output: toOutput(out), Resp: resp}, nil
}

// toOutput turns the tool input into draft lines, the text scrubbed (a pack can carry a
// phone number) and the same product named twice kept once.
func toOutput(o ToolOutput) Output {
	out := Output{Lines: make([]Line, 0, len(o.Products))}
	seen := map[string]bool{}
	for _, p := range o.Products {
		l := Line{
			Name:       clean(p.Name),
			Brand:      cleanp(p.Brand),
			Size:       cleanp(p.Size),
			Confidence: math.Round(p.Confidence*100) / 100,
		}
		key := strings.ToLower(MatchName(l))
		if l.Name == "" || seen[key] {
			continue
		}
		seen[key] = true
		l.LineID = "l" + strconv.Itoa(len(out.Lines)+1)
		out.Lines = append(out.Lines, l)
	}
	return out
}

// MatchName is the text a line is matched to the master catalogue by.
func MatchName(l Line) string {
	parts := []string{}
	if l.Brand != nil {
		parts = append(parts, *l.Brand)
	}
	parts = append(parts, l.Name)
	if l.Size != nil {
		parts = append(parts, *l.Size)
	}
	return strings.Join(parts, " ")
}

func clean(s string) string { return strings.TrimSpace(scrub.Text(s, scrub.Options{}).Text) }

func cleanp(s *string) *string {
	if s == nil {
		return nil
	}
	if c := clean(*s); c != "" {
		return &c
	}
	return nil
}
