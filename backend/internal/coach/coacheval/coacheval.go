// Package coacheval is the recorded eval suite of the coach's wording (feature coach_card,
// 11 §2.1 step 4, 11 §1 rule 8): every case runs coach.Rewrite against a recorded reply and
// passes when the output is used exactly as recorded, or is refused for the expected reason.
// The bar is that insight and coach text has 0 ungrounded numbers (W3-11), so an invented
// percent, a sum the model worked out or a number not in the fact is always rejected.
package coacheval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/coach"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	// TODO(lead): 11 names no pass bar for coach_card; conservative reading, every case.
	eval.Register(eval.Suite{Feature: coach.Feature, PassBar: 1, Fixtures: fx, Run: run})
}

type expect struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Reject string `json:"reject"`
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	var in coach.Input
	var want expect
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return eval.Verdict{}, err
	}
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return eval.Verdict{}, err
	}
	got, _, reject, err := coach.Rewrite(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, in)
	if err != nil {
		return eval.Verdict{}, err
	}
	if reject != want.Reject {
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", reject, want.Reject)}, nil
	}
	if got.Title != want.Title || got.Body != want.Body {
		return eval.Verdict{Note: "the used text differs from the recorded one"}, nil
	}
	return eval.Verdict{Pass: true}, nil
}
