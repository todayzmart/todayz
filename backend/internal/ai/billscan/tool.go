package billscan

import (
	"context"
	"embed"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
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
	maxName     = 120
	maxSupplier = 80
	maxRupees   = MaxPaise / 100
	dateLayout  = "2006-01-02"
)

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "read_supplier_bill",
	Description: "Record the item lines of one supplier bill photo.",
	Schema: schema.Object("", map[string]schema.S{
		"supplier":  schema.Nullable(schema.String("The seller's business name only, or null", maxSupplier)),
		"bill_date": schema.Nullable(schema.String("The bill's date as YYYY-MM-DD, or null", 10)),
		"lines": schema.Array("One line per item on the bill, in bill order", schema.Object("", map[string]schema.S{
			"name":        schema.String("The item as written, with brand and pack size when shown", maxName),
			"quantity":    schema.Integer("Selling units received, free units included", 1, MaxQuantity),
			"cost_rupees": schema.Nullable(schema.Number("Cost of one unit in rupees (the rate), or null", 0.01, maxRupees)),
			"mrp_rupees":  schema.Nullable(schema.Number("Printed MRP of one unit in rupees, or null", 0.01, maxRupees)),
			"expires_on":  schema.Nullable(schema.String("Expiry date as YYYY-MM-DD, or null", 10)),
			"confidence":  schema.Number("How sure you are of this line's name and numbers", 0, 1),
		}), MaxLines),
	}),
}

// ToolLine is one line of the tool input.
type ToolLine struct {
	Name       string   `json:"name"`
	Quantity   int      `json:"quantity"`
	CostRupees *float64 `json:"cost_rupees"`
	MrpRupees  *float64 `json:"mrp_rupees"`
	ExpiresOn  *string  `json:"expires_on"`
	Confidence float64  `json:"confidence"`
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Supplier *string    `json:"supplier"`
	BillDate *string    `json:"bill_date"`
	Lines    []ToolLine `json:"lines"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if len(o.Lines) > MaxLines {
		return fmt.Errorf("%d lines", len(o.Lines))
	}
	if o.Supplier != nil && utf8.RuneCountInString(*o.Supplier) > maxSupplier {
		return fmt.Errorf("supplier length")
	}
	if o.BillDate != nil && !validDate(*o.BillDate) {
		return fmt.Errorf("bill_date")
	}
	for i, l := range o.Lines {
		if strings.TrimSpace(l.Name) == "" || utf8.RuneCountInString(l.Name) > maxName {
			return fmt.Errorf("line %d: name", i)
		}
		if l.Quantity < 1 || l.Quantity > MaxQuantity {
			return fmt.Errorf("line %d: quantity", i)
		}
		for _, r := range []*float64{l.CostRupees, l.MrpRupees} {
			if r != nil && (math.IsNaN(*r) || *r < 0.01 || *r > maxRupees) {
				return fmt.Errorf("line %d: rupees", i)
			}
		}
		if l.ExpiresOn != nil && !validDate(*l.ExpiresOn) {
			return fmt.Errorf("line %d: expires_on", i)
		}
		if l.Confidence < 0 || l.Confidence > 1 || math.IsNaN(l.Confidence) {
			return fmt.Errorf("line %d: confidence", i)
		}
	}
	return nil
}

func validDate(s string) bool {
	_, err := time.Parse(dateLayout, s)
	return err == nil
}

// Line is one stored draft line (the output's "lines").
type Line struct {
	LineID      string  `json:"lineId"`
	Name        string  `json:"name"`
	Quantity    int     `json:"quantity"`
	CostPaise   *int64  `json:"costPaise"`
	MrpPaise    *int64  `json:"mrpPaise"`
	ExpiresOn   *string `json:"expiresOn"`
	Confidence  float64 `json:"confidence"`
	StoreItemID *string `json:"storeItemId"`
	ItemName    *string `json:"itemName"`
}

// Output is a ready draft's output.
type Output struct {
	Supplier *string `json:"supplier"`
	BillDate *string `json:"billDate"`
	Lines    []Line  `json:"lines"`
}

// Read is what the model made of one photo.
type Read struct {
	Output Output // lines without matches yet
	Resp   client.Response
	Reject string // "" or a client outcome or schema
}

// ReadBill runs the model on one bill photo. ref is the draft id (a case name in the eval).
// A provider error the request caused is returned as an error.
func ReadBill(ctx context.Context, llm client.LLM, model, ref string, img client.Image) (Read, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Read{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 4096,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Image: &img}, {Text: "Record the item lines of this supplier bill."},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Read{Resp: resp, Reject: reject}, err
	}
	return Read{Output: toOutput(out), Resp: resp}, nil
}

// toOutput turns the tool input into draft lines: rupees to paise, the supplier scrubbed
// (a bill header often carries a phone or GSTIN), names scrubbed and trimmed.
func toOutput(o ToolOutput) Output {
	out := Output{BillDate: o.BillDate, Lines: make([]Line, 0, len(o.Lines))}
	if o.Supplier != nil {
		if s := strings.TrimSpace(scrub.Text(*o.Supplier, scrub.Options{}).Text); s != "" {
			out.Supplier = &s
		}
	}
	for i, tl := range o.Lines {
		out.Lines = append(out.Lines, Line{
			LineID:     "l" + strconv.Itoa(i+1),
			Name:       strings.TrimSpace(scrub.Text(tl.Name, scrub.Options{}).Text),
			Quantity:   tl.Quantity,
			CostPaise:  paise(tl.CostRupees),
			MrpPaise:   paise(tl.MrpRupees),
			ExpiresOn:  tl.ExpiresOn,
			Confidence: math.Round(tl.Confidence*100) / 100,
		})
	}
	return out
}

func paise(r *float64) *int64 {
	if r == nil {
		return nil
	}
	p := int64(math.Round(*r * 100))
	return &p
}
