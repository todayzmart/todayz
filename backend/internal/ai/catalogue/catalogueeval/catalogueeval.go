// Package catalogueeval is the recorded eval suite of the catalogue clean-up (AI-A1, feature
// catalogue_cleanup, 11 §4): pending products with their look-alikes. A case passes when
// catalogue.Write proposes the recorded duplicate (or none), or refuses the draft for the
// expected reason. The bar is duplicate precision (11 §4: ≥ 90%): of the merges the drafts
// propose, the share that are the same product. A wrong merge is worse than a missed one, so a
// case that should merge and answers none still passes the precision bar but fails its case.
//
// The recorded replies are synthetic (one seeded wrong merge), so recorded mode proves the
// request, the mapping by reference, grounding and the scoring; make ai-eval measures the model.
package catalogueeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"time"

	"github.com/todayz/backend/internal/ai/catalogue"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

// PrecisionBar is 11 §4's duplicate precision bar; the suite's case bar is the same.
const PrecisionBar = 0.9

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	eval.Register(eval.Suite{Feature: catalogue.Feature, PassBar: PrecisionBar, Fixtures: fx, Run: run})
}

type product struct {
	Name      string  `json:"name"`
	BrandName *string `json:"brandName"`
	Variant   *string `json:"variant"`
	PackSize  float64 `json:"packSize"`
	Unit      string  `json:"unit"`
}

type candidate struct {
	product
	Similarity float64 `json:"similarity"`
}

type input struct {
	Product    product           `json:"product"`
	Category   string            `json:"category"`
	Candidates []candidate       `json:"candidates"`
	Categories []string          `json:"categories"`
	NamesNow   map[string]string `json:"namesNow"`
}

type expect struct {
	Duplicate string `json:"duplicate"` // a candidate ref or none
	Names     bool   `json:"names"`     // a categoryNames line
	Reject    string `json:"reject"`
}

// at is a fixed time for the fixture ids (only their uniqueness matters).
var at = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// Input is the model's view of a fixture, built as the service builds it.
func Input(raw json.RawMessage) (catalogue.Input, error) {
	var f input
	if err := json.Unmarshal(raw, &f); err != nil {
		return catalogue.Input{}, err
	}
	in := catalogue.Input{Candidates: []catalogue.Candidate{}, Categories: []catalogue.Category{}}
	current := ""
	for i, name := range f.Categories {
		c := catalogue.Category{ID: ids.New(at), Ref: fmt.Sprintf("c%d", i+1), Name: name}
		if name == f.Category {
			in.CategoryID, current = c.ID, c.Ref
		}
		in.Categories = append(in.Categories, c)
	}
	p := f.Product
	in.Product = catalogue.NewProduct(p.Name, p.BrandName, p.Variant, p.PackSize, p.Unit, current)
	for i, c := range f.Candidates {
		in.Candidates = append(in.Candidates, catalogue.Candidate{ID: ids.New(at), Ref: fmt.Sprintf("p%d", i+1), Name: c.Name,
			BrandName: c.BrandName, Variant: c.Variant, PackSize: c.PackSize, Unit: c.Unit, Similarity: c.Similarity})
	}
	return in.WithNames(f.NamesNow), nil
}

// Result is one case's outcome: the duplicate the draft proposed (a ref, none, or empty when
// refused).
type Result struct {
	Draft     catalogue.Draft
	Duplicate string
}

func write(ctx context.Context, env eval.Env, c eval.Case) (Result, expect, error) {
	var want expect
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return Result{}, want, err
	}
	in, err := Input(c.Input)
	if err != nil {
		return Result{}, want, err
	}
	d, err := catalogue.Write(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, in)
	if err != nil {
		return Result{}, want, err
	}
	r := Result{Draft: d}
	if d.Reject != "" {
		return r, want, nil
	}
	r.Duplicate = catalogue.NoDuplicate
	if l, ok := d.Output.Line(catalogue.KindDuplicate); ok {
		for _, cand := range in.Candidates {
			if cand.ID.String() == l.ProductID {
				r.Duplicate = cand.Ref
			}
		}
	}
	return r, want, nil
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	r, want, err := write(ctx, env, c)
	if err != nil {
		return eval.Verdict{}, err
	}
	if r.Draft.Reject != want.Reject {
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", r.Draft.Reject, want.Reject)}, nil
	}
	if want.Reject != "" {
		return eval.Verdict{Pass: len(r.Draft.Output.Lines) == 0, Note: "a refused draft still has lines"}, nil
	}
	if r.Duplicate != want.Duplicate {
		return eval.Verdict{Note: fmt.Sprintf("duplicate %s, want %s", r.Duplicate, want.Duplicate)}, nil
	}
	if _, ok := r.Draft.Output.Line(catalogue.KindCategoryNames); ok != want.Names {
		return eval.Verdict{Note: fmt.Sprintf("category names line %v, want %v", ok, want.Names)}, nil
	}
	return eval.Verdict{Pass: true}, nil
}

// Precision counts the merges the drafts proposed and the right ones among them.
type Precision struct{ Proposed, Right int }

// Score is Right/Proposed, 1 when nothing was proposed.
func (p Precision) Score() float64 {
	if p.Proposed == 0 {
		return 1
	}
	return float64(p.Right) / float64(p.Proposed)
}

// Measure runs every case and adds up the proposed merges.
func Measure(ctx context.Context, env eval.Env, cases []eval.Case) (Precision, error) {
	var p Precision
	for _, c := range cases {
		r, want, err := write(ctx, env, c)
		if err != nil {
			return Precision{}, fmt.Errorf("%s: %w", c.Name, err)
		}
		if r.Draft.Reject != "" || r.Duplicate == catalogue.NoDuplicate {
			continue
		}
		p.Proposed++
		if r.Duplicate == want.Duplicate {
			p.Right++
		}
	}
	return p, nil
}
