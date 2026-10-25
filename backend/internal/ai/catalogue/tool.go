package catalogue

import (
	"context"
	"embed"
	"encoding/json"
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
	"github.com/todayz/backend/internal/platform/ids"
)

//go:embed prompt.md
var promptFS embed.FS

func init() { prompts.MustRegister(Feature, promptFS) }

// Units are the catalogue's units (AdminAiUnit).
var Units = []string{"g", "kg", "ml", "l", "pc", "pack"}

// Languages are the five display languages, English first.
var Languages = []string{"en", "hi", "kn", "te", "ta"}

// scripts is each Indian language's own script (a drafted name must be written in it).
var scripts = map[string]*unicode.RangeTable{
	"hi": unicode.Devanagari, "kn": unicode.Kannada, "te": unicode.Telugu, "ta": unicode.Tamil,
}

// The contract's bounds (CatalogueCleanupNormalise, CatalogueCleanupDuplicate, AdminAiCategoryNames).
const (
	MaxName       = 120
	MaxVariant    = 80
	MaxReason     = 300
	MaxNameLang   = 60
	MaxPack       = 1_000_000
	MaxCandidates = 5 // DuplicateCandidates' limit
)

// NoDuplicate is the duplicateRef for "none of the candidates".
const NoDuplicate = "none"

func duplicateRefs() []string {
	out := []string{NoDuplicate}
	for i := 1; i <= MaxCandidates; i++ {
		out = append(out, fmt.Sprintf("p%d", i))
	}
	return out
}

// Tool is the strict tool the model answers with.
var Tool = client.Tool{
	Name:        "clean_up_product",
	Description: "Record the pending product's clean-up: its fields, the duplicate to merge into and the category's names.",
	Schema: schema.Object("", map[string]schema.S{
		"name":            schema.String("The product's name, without brand, pack or variant", MaxName),
		"brandName":       schema.Nullable(schema.String("The brand, or null", MaxName)),
		"variant":         schema.Nullable(schema.String("The variant, or null", MaxVariant)),
		"packSize":        schema.Number("The pack size as written", 0.001, MaxPack),
		"unit":            schema.Enum("The pack's unit as written", Units...),
		"categoryRef":     schema.String("The ref of the category (c1, c2, …)", 6),
		"duplicateRef":    schema.Enum("The ref of the candidate that is the same product, or none", duplicateRefs()...),
		"duplicateReason": schema.Nullable(schema.String("Why it is the same product, or null", MaxReason)),
		"categoryNames": schema.Nullable(schema.Object("The category's names when asked, else null", map[string]schema.S{
			"en": schema.String("English", MaxNameLang),
			"hi": schema.String("Hindi, in Devanagari", MaxNameLang),
			"kn": schema.String("Kannada, in Kannada script", MaxNameLang),
			"te": schema.String("Telugu, in Telugu script", MaxNameLang),
			"ta": schema.String("Tamil, in Tamil script", MaxNameLang),
		})),
	}),
}

// Names are a category's names by language.
type Names struct {
	En string `json:"en"`
	Hi string `json:"hi"`
	Kn string `json:"kn"`
	Te string `json:"te"`
	Ta string `json:"ta"`
}

// Get is the name in one language.
func (n Names) Get(lang string) string {
	switch lang {
	case "en":
		return n.En
	case "hi":
		return n.Hi
	case "kn":
		return n.Kn
	case "te":
		return n.Te
	case "ta":
		return n.Ta
	}
	return ""
}

// Set returns n with one language's name set.
func (n Names) Set(lang, v string) Names {
	switch lang {
	case "en":
		n.En = v
	case "hi":
		n.Hi = v
	case "kn":
		n.Kn = v
	case "te":
		n.Te = v
	case "ta":
		n.Ta = v
	}
	return n
}

// ToolOutput is the tool input.
type ToolOutput struct {
	Name            string  `json:"name"`
	BrandName       *string `json:"brandName"`
	Variant         *string `json:"variant"`
	PackSize        float64 `json:"packSize"`
	Unit            string  `json:"unit"`
	CategoryRef     string  `json:"categoryRef"`
	DuplicateRef    string  `json:"duplicateRef"`
	DuplicateReason *string `json:"duplicateReason"`
	CategoryNames   *Names  `json:"categoryNames"`
}

// Validate is the bounds the strict schema only describes.
func (o *ToolOutput) Validate() error {
	if !fits(o.Name, MaxName) || !fitsOpt(o.BrandName, MaxName) || !fitsOpt(o.Variant, MaxVariant) || !fitsOpt(o.DuplicateReason, MaxReason) {
		return fmt.Errorf("length")
	}
	if o.PackSize <= 0 || o.PackSize > MaxPack || !oneOf(o.Unit, Units) {
		return fmt.Errorf("pack")
	}
	if o.CategoryNames != nil {
		for _, l := range Languages {
			if !fits(o.CategoryNames.Get(l), MaxNameLang) {
				return fmt.Errorf("category name %s", l)
			}
		}
	}
	return nil
}

func fits(s string, maxLen int) bool {
	s = strings.TrimSpace(s)
	return s != "" && utf8.RuneCountInString(s) <= maxLen
}

func fitsOpt(s *string, maxLen int) bool {
	return s == nil || strings.TrimSpace(*s) == "" || fits(*s, maxLen)
}

func oneOf(s string, vals []string) bool {
	for _, v := range vals {
		if s == v {
			return true
		}
	}
	return false
}

// InScript reports whether every letter of s is in the language's own script (en: no Indian
// script letters).
func InScript(lang, s string) bool {
	letters := 0
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.Is(unicode.Mn, r) && !unicode.Is(unicode.Mc, r) {
			continue
		}
		letters++
		if t, ok := scripts[lang]; ok {
			if !unicode.Is(t, r) {
				return false
			}
			continue
		}
		for _, t := range scripts {
			if unicode.Is(t, r) {
				return false
			}
		}
	}
	return letters > 0
}

// Product is the pending product as the model sees it.
type Product struct {
	Name        string  `json:"name"`
	BrandName   *string `json:"brandName"`
	Variant     *string `json:"variant"`
	PackSize    float64 `json:"packSize"`
	Unit        string  `json:"unit"`
	CategoryRef string  `json:"categoryRef"`
}

// Candidate is one active look-alike, by reference.
type Candidate struct {
	ID         ids.ID  `json:"-"`
	Ref        string  `json:"ref"`
	Name       string  `json:"name"`
	BrandName  *string `json:"brandName"`
	Variant    *string `json:"variant"`
	PackSize   float64 `json:"packSize"`
	Unit       string  `json:"unit"`
	Similarity float64 `json:"similarity"`
}

// Display is the candidate's name as A-08 shows it.
func (c Candidate) Display() string {
	return retail.Truncate(retail.ItemName(c.Name, c.Variant, Num(c.PackSize), c.Unit), 200) //settings:ok contract bound (productName ≤ 200)
}

// Category is one category, by reference.
type Category struct {
	ID   ids.ID `json:"-"`
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

// Input is what the model is shown.
type Input struct {
	CategoryID  ids.ID            `json:"-"`
	Product     Product           `json:"product"`
	Candidates  []Candidate       `json:"candidates"`
	Categories  []Category        `json:"categories"`
	NamesWanted bool              `json:"categoryNamesWanted"`
	NamesNow    map[string]string `json:"categoryNamesNow,omitempty"`
}

// WithNames sets the category's names now; the draft asks for the missing languages only
// while one is missing, and then shows the model the ones it has.
func (in Input) WithNames(now map[string]string) Input {
	in.NamesNow = map[string]string{}
	for _, l := range Languages {
		if v := strings.TrimSpace(now[l]); v != "" {
			in.NamesNow[l] = v
		}
	}
	in.NamesWanted = len(in.Missing()) > 0
	if !in.NamesWanted {
		in.NamesNow = nil
	}
	return in
}

// Num is a pack size as text ("500", "0.5").
func Num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// clean is scrub.Text for the shop's typed fields.
func clean(s string) string { return strings.TrimSpace(scrub.Text(s, scrub.Options{}).Text) }

func cleanOpt(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	c := clean(*s)
	return &c
}

// NewProduct is the model's view of the pending product: its typed fields scrubbed.
func NewProduct(name string, brand, variant *string, packSize float64, unit, categoryRef string) Product {
	return Product{Name: clean(name), BrandName: cleanOpt(brand), Variant: cleanOpt(variant), PackSize: packSize, Unit: unit, CategoryRef: categoryRef}
}

// JSON is the facts JSON the model is given.
func (in Input) JSON() []byte {
	b, _ := json.Marshal(in) // strings, numbers and slices of them always marshal
	return b
}

// own are the product's own strings: a drafted field's numbers must come from these.
func (in Input) own() []string {
	p := in.Product
	out := []string{p.Name, Num(p.PackSize)}
	if p.BrandName != nil {
		out = append(out, *p.BrandName)
	}
	if p.Variant != nil {
		out = append(out, *p.Variant)
	}
	return out
}

// OwnGrounded reports whether a drafted field holds only the product's own numbers.
func (in Input) OwnGrounded(s string) bool {
	return len(schema.Ungrounded(s, in.own()...)) == 0 && scrub.Clean(s)
}

// PackGrounded reports whether a pack size is the product's own or written in its name or
// variant.
func (in Input) PackGrounded(f float64) bool {
	return f == in.Product.PackSize || len(schema.Ungrounded(Num(f), in.own()...)) == 0
}

// Missing are the languages the category has no name in.
func (in Input) Missing() []string {
	var out []string
	for _, l := range Languages {
		if strings.TrimSpace(in.NamesNow[l]) == "" {
			out = append(out, l)
		}
	}
	return out
}

// The line kinds.
const (
	KindNormalise     = "normalise"
	KindDuplicate     = "duplicate"
	KindCategoryNames = "categoryNames"
)

// Line is one stored draft line: l1 normalise, l2 duplicate (when the model picked one), l3
// categoryNames (when the category lacks a language).
type Line struct {
	LineID string `json:"lineId"`
	Kind   string `json:"kind"`

	Name         string  `json:"name,omitempty"`
	BrandName    *string `json:"brandName,omitempty"`
	Variant      *string `json:"variant,omitempty"`
	PackSize     float64 `json:"packSize,omitempty"`
	Unit         string  `json:"unit,omitempty"`
	CategoryID   string  `json:"categoryId,omitempty"`
	CategoryName string  `json:"categoryName,omitempty"`

	ProductID   string  `json:"productId,omitempty"`
	ProductName string  `json:"productName,omitempty"`
	Similarity  float64 `json:"similarity,omitempty"`
	Reason      string  `json:"reason,omitempty"`

	Names *Names `json:"names,omitempty"`
}

// Output is a ready draft's output. Candidates are the look-alikes' ids: an edited duplicate
// line may pick another of them, never a product outside the list.
type Output struct {
	Lines      []Line   `json:"lines"`
	Candidates []string `json:"candidates"`
}

// Line returns the line of a kind.
func (o Output) Line(kind string) (Line, bool) {
	for _, l := range o.Lines {
		if l.Kind == kind {
			return l, true
		}
	}
	return Line{}, false
}

// Draft is what the model made of one pending product.
type Draft struct {
	Output Output
	Resp   client.Response
	Reject string
}

// Write runs the model on one pending product. A ref it was not shown or a name in the wrong
// script fails as schema; a pack size, name, variant, brand or reason with a number it was
// not given, as grounding.
func Write(ctx context.Context, llm client.LLM, model, ref string, in Input) (Draft, error) {
	sys, err := prompts.MustGet(Feature + "/prompt").Render(nil)
	if err != nil {
		return Draft{}, err
	}
	var out ToolOutput
	resp, reject, err := retail.Call(ctx, llm, client.Request{
		Feature: Feature, Ref: ref, Model: model, System: sys, Tool: &Tool, MaxTokens: 768,
		Messages: []client.Message{{Role: client.RoleUser, Content: []client.Block{
			{Text: "The pending product (JSON):\n" + string(in.JSON())},
		}}},
	}, &out)
	if err != nil || reject != "" {
		return Draft{Resp: resp, Reject: reject}, err
	}
	d, reject := Build(in, out)
	if reject != "" {
		client.RecordRejected(ctx, Feature, reject)
	}
	d.Resp = resp
	return d, nil
}

// Build turns a decoded tool output into the draft's lines (Write without the model call, for
// the eval). reject is a client reject code when the output may not be shown.
func Build(in Input, out ToolOutput) (Draft, string) {
	schemaFail := Draft{Reject: client.RejectSchema}
	groundFail := Draft{Reject: client.RejectGrounding}
	var cat *Category
	for i := range in.Categories {
		if in.Categories[i].Ref == out.CategoryRef {
			cat = &in.Categories[i]
		}
	}
	if cat == nil {
		return schemaFail, client.RejectSchema
	}
	norm := Line{LineID: "l1", Kind: KindNormalise, Name: strings.TrimSpace(out.Name), BrandName: trimOpt(out.BrandName),
		Variant: trimOpt(out.Variant), PackSize: out.PackSize, Unit: out.Unit, CategoryID: cat.ID.String(), CategoryName: cat.Name}
	if !in.PackGrounded(norm.PackSize) || !in.OwnGrounded(norm.Name) ||
		(norm.Variant != nil && !in.OwnGrounded(*norm.Variant)) || (norm.BrandName != nil && !in.OwnGrounded(*norm.BrandName)) {
		return groundFail, client.RejectGrounding
	}
	o := Output{Lines: []Line{norm}, Candidates: make([]string, 0, len(in.Candidates))}
	for _, c := range in.Candidates {
		o.Candidates = append(o.Candidates, c.ID.String())
	}
	if out.DuplicateRef != NoDuplicate {
		var c *Candidate
		for i := range in.Candidates {
			if in.Candidates[i].Ref == out.DuplicateRef {
				c = &in.Candidates[i]
			}
		}
		reason := ""
		if out.DuplicateReason != nil {
			reason = strings.TrimSpace(*out.DuplicateReason)
		}
		if c == nil || reason == "" {
			return schemaFail, client.RejectSchema
		}
		if len(schema.UngroundedFacts(reason, in.JSON())) > 0 || !scrub.Clean(reason) {
			return groundFail, client.RejectGrounding
		}
		o.Lines = append(o.Lines, Line{LineID: "l2", Kind: KindDuplicate, ProductID: c.ID.String(), ProductName: c.Display(),
			Similarity: c.Similarity, Reason: reason})
	}
	if in.NamesWanted && out.CategoryNames != nil {
		names := Names{}
		for _, l := range Languages {
			now := strings.TrimSpace(in.NamesNow[l])
			if now != "" {
				names = names.Set(l, now) // a language the category has keeps its name
				continue
			}
			v := strings.TrimSpace(out.CategoryNames.Get(l))
			if !InScript(l, v) {
				return schemaFail, client.RejectSchema
			}
			if len(schema.Ungrounded(v)) > 0 || !scrub.Clean(v) {
				return groundFail, client.RejectGrounding
			}
			names = names.Set(l, v)
		}
		o.Lines = append(o.Lines, Line{LineID: "l3", Kind: KindCategoryNames, CategoryID: in.CategoryID.String(), Names: &names})
	}
	return Draft{Output: o}, ""
}

func trimOpt(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}
