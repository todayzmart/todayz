// Package riskeval is the recorded eval suite of the risk flag summary (AI-A3, feature
// risk_summary, 11 §4, D-230): one flag per risk rule, with the subject's earlier flags. The
// bar is 100% grounded: every summary shown holds only the flag's own numbers, written as the
// flag writes them, and a summary that works out a number of its own (a sum, a ratio, a
// rounded value) or carries a phone number is refused. Every case must pass.
//
// The recorded replies are synthetic, so recorded mode proves the request, the facts the model
// is shown (ids left out) and the grounding gate; make ai-eval measures the model.
package riskeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/riskexplain"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	eval.Register(eval.Suite{Feature: riskexplain.Feature, PassBar: 1, Fixtures: fx, Run: run})
}

type input struct {
	Rule        string              `json:"rule"`
	SubjectType string              `json:"subjectType"`
	Score       int                 `json:"score"`
	Status      string              `json:"status"`
	Facts       []riskexplain.Fact  `json:"facts"`
	History     riskexplain.History `json:"otherFlags"`
}

type expect struct {
	Text   string `json:"text"`
	Reject string `json:"reject"`
}

// Flag is the model's view of a fixture, built as the service builds it.
func Flag(raw json.RawMessage) (riskexplain.Flag, error) {
	var f input
	if err := json.Unmarshal(raw, &f); err != nil {
		return riskexplain.Flag{}, err
	}
	return riskexplain.NewFlag(f.Rule, f.SubjectType, f.Score, f.Status, f.Facts, f.History), nil
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	var want expect
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return eval.Verdict{}, err
	}
	f, err := Flag(c.Input)
	if err != nil {
		return eval.Verdict{}, err
	}
	for k := range f.Facts {
		if k == "id" || len(k) > 2 && (k[len(k)-2:] == "Id" || k[len(k)-3:] == "Ids") {
			return eval.Verdict{Note: "an id reached the model"}, nil
		}
	}
	d, err := riskexplain.Write(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, f)
	if err != nil {
		return eval.Verdict{}, err
	}
	if d.Reject != want.Reject {
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", d.Reject, want.Reject)}, nil
	}
	if want.Reject != "" {
		return eval.Verdict{Pass: len(d.Output.Lines) == 0, Note: "a refused summary still has a line"}, nil
	}
	if len(d.Output.Lines) != 1 || d.Output.Lines[0].Text != want.Text {
		return eval.Verdict{Note: "the summary differs from the recorded one"}, nil
	}
	if !f.Grounded(d.Output.Lines[0].Text) {
		return eval.Verdict{Note: "a summary shown is not grounded"}, nil
	}
	return eval.Verdict{Pass: true}, nil
}
