// Package listparseeval is list parse's eval suite (feature list_parse, W3-12 AI-C1): 200
// shopping lists typed, pasted or spoken in English, Hindi, Kannada, Telugu, Tamil and mixed,
// each with the basket lines it should give and a recorded model reply. A case passes only
// when every line is exact (text and quantity, in order) or the list is refused for the
// expected reason; the suite's pass bar is 95% (07 W3-12) and every case's latency counts
// toward the p95 under 2.5 s. It runs listparse.Parse, the same code the draft uses, and
// writes nothing.
package listparseeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/listparse"
	"github.com/todayz/backend/internal/platform/settings"
)

// PassBar is the share of cases that must be exact (07 W3-12: 95% of items parsed).
const PassBar = 0.95

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	eval.Register(eval.Suite{Feature: listparse.Feature, PassBar: PassBar, Fixtures: fx, Run: run})
}

type input struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

// Line is one expected basket line.
type Line struct {
	Text     string `json:"text"`
	Quantity int    `json:"quantity"`
}

type expect struct {
	Lines  []Line `json:"lines"`
	Reject string `json:"reject"`
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	var in input
	var want expect
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return eval.Verdict{}, err
	}
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return eval.Verdict{}, err
	}
	d, err := listparse.Parse(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, listparse.Clean(in.Text), listparse.MaxLines)
	if err != nil {
		return eval.Verdict{}, err
	}
	if d.Reject != want.Reject {
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", d.Reject, want.Reject)}, nil
	}
	if len(d.Output.Lines) != len(want.Lines) {
		return eval.Verdict{Note: fmt.Sprintf("%d lines, want %d", len(d.Output.Lines), len(want.Lines))}, nil
	}
	for i, l := range d.Output.Lines {
		if l.Text != want.Lines[i].Text || l.Quantity != want.Lines[i].Quantity {
			return eval.Verdict{Note: fmt.Sprintf("line %d differs", i+1)}, nil
		}
	}
	return eval.Verdict{Pass: true}, nil
}
