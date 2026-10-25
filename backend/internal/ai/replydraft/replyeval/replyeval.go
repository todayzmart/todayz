// Package replyeval is the recorded eval suite of the review reply draft (AI-R5, feature
// review_reply, 11 §1 rule 8): reviews in the five languages, with and without text. A case
// passes when replydraft.Write drafts the recorded reply, or refuses it for the expected
// reason: a promise with a number the review does not carry, a phone number, a schema break or
// a refusal never reaches the owner's editor.
package replyeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/replydraft"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	// TODO(lead): 11 names no pass bar for review_reply; conservative reading, every case.
	eval.Register(eval.Suite{Feature: replydraft.Feature, PassBar: 1, Fixtures: fx, Run: run})
}

type input struct {
	Stars    int      `json:"stars"`
	Tags     []string `json:"tags"`
	Text     *string  `json:"text"`
	Language string   `json:"language"`
	Shop     string   `json:"shop"`
}

type expect struct {
	Text   string `json:"text"`
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
	rv := replydraft.NewReview(in.Stars, in.Tags, in.Text, in.Language, in.Shop)
	d, err := replydraft.Write(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, rv)
	if err != nil {
		return eval.Verdict{}, err
	}
	if d.Reject != want.Reject {
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", d.Reject, want.Reject)}, nil
	}
	if want.Reject != "" {
		if len(d.Output.Lines) != 0 {
			return eval.Verdict{Note: "a refused reply still has a line"}, nil
		}
		return eval.Verdict{Pass: true}, nil
	}
	if len(d.Output.Lines) != 1 || d.Output.Lines[0].Text != want.Text || d.Output.Lines[0].Language != in.Language {
		return eval.Verdict{Note: "the drafted reply differs from the recorded one"}, nil
	}
	return eval.Verdict{Pass: true}, nil
}
