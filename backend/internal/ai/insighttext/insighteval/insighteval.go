// Package insighteval is the recorded eval suite of the Demand insights sentences (AI-R4,
// feature insight_text, 11 §1 rule 8): rows of facts in the five languages, each with the
// sentences that may be used. A case passes when insighttext.Write keeps exactly the grounded
// sentences and drops the rest. The bar is W3-11's: insight text has 0 ungrounded numbers, so
// an invented percent, a sum the model worked out or a phone number is always dropped.
package insighteval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/insighttext"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	// TODO(lead): 11 names no pass bar for insight_text; conservative reading, every case.
	eval.Register(eval.Suite{Feature: insighttext.Feature, PassBar: 1, Fixtures: fx, Run: run})
}

type input struct {
	Lang string            `json:"lang"`
	Rows []insighttext.Row `json:"rows"`
}

type expect struct {
	Texts   map[string]string `json:"texts"`
	Dropped int               `json:"dropped"`
	Reject  string            `json:"reject"`
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
	w, err := insighttext.Write(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, in.Lang, in.Rows)
	if err != nil {
		return eval.Verdict{}, err
	}
	switch {
	case w.Reject != want.Reject:
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", w.Reject, want.Reject)}, nil
	case w.Dropped != want.Dropped:
		return eval.Verdict{Note: fmt.Sprintf("dropped %d, want %d", w.Dropped, want.Dropped)}, nil
	case len(w.Texts) != len(want.Texts) || !maps.Equal(w.Texts, want.Texts):
		return eval.Verdict{Note: "the kept sentences differ from the recorded ones"}, nil
	}
	return eval.Verdict{Pass: true}, nil
}
