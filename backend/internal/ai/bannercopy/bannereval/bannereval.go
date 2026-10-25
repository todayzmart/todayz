// Package bannereval is the recorded eval suite of the banner copy draft (AI-R9, feature
// banner_copy, 11 §1 rule 8): offers and items in the five languages. A case passes when
// bannercopy.Write keeps the expected number of options and drops the rest: an option with a
// number the facts do not carry (an invented discount, a worked-out price) or a phone number is
// dropped, and a draft with no option left is refused.
package bannereval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/todayz/backend/internal/ai/bannercopy"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	// TODO(lead): 11 names no pass bar for banner_copy; conservative reading, every case.
	eval.Register(eval.Suite{Feature: bannercopy.Feature, PassBar: 1, Fixtures: fx, Run: run})
}

type expect struct {
	Kept    int    `json:"kept"`
	Dropped int    `json:"dropped"`
	Reject  string `json:"reject"`
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	var f bannercopy.Facts
	var want expect
	if err := json.Unmarshal(c.Input, &f); err != nil {
		return eval.Verdict{}, err
	}
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return eval.Verdict{}, err
	}
	for i := range f.Items {
		f.Items[i].ID = fmt.Sprintf("item-%d", f.Items[i].N)
	}
	d, err := bannercopy.Write(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, f)
	if err != nil {
		return eval.Verdict{}, err
	}
	switch {
	case d.Reject != want.Reject:
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", d.Reject, want.Reject)}, nil
	case len(d.Output.Lines) != want.Kept || d.Dropped != want.Dropped && want.Reject == "":
		return eval.Verdict{Note: fmt.Sprintf("kept %d dropped %d, want %d and %d", len(d.Output.Lines), d.Dropped, want.Kept, want.Dropped)}, nil
	}
	for _, l := range d.Output.Lines {
		if l.StoreItemID != nil && *l.StoreItemID == "" {
			return eval.Verdict{Note: "an option points at no item"}, nil
		}
	}
	return eval.Verdict{Pass: true}, nil
}
