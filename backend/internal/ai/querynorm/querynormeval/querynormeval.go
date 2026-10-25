// Package querynormeval is search assist's eval suite (feature search_assist, W3-12 AI-C2):
// 100 search sentences in English, Hindi, Kannada, Telugu and Tamil that the parser left with
// refineHint, each with the master-catalogue brands, the suggestion the customer should see and
// a recorded model reply. A case passes only when the suggested query and every chip (kind,
// value, words) are exact, or nothing is shown when the reply is invalid; a chip of a kind
// outside querynorm.Kinds fails the case whatever else matches. The suite's pass bar is 90%
// (07 W3-12). It runs querynorm.Suggest and querynorm.Validate, the code the route uses, and
// writes nothing.
package querynormeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/querynorm"
	"github.com/todayz/backend/internal/ai/scrub"
	"github.com/todayz/backend/internal/platform/settings"
)

// PassBar is the share of cases that must be exact (07 W3-12: 90% of chips correct).
const PassBar = 0.9

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	eval.Register(eval.Suite{Feature: querynorm.Feature, PassBar: PassBar, Fixtures: fx, Run: run})
}

type input struct {
	Q      string   `json:"q"`
	Brands []string `json:"brands"` // the master catalogue's slugs
}

// Chip is one expected chip; value is "" for the kinds without one.
type Chip struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Text  string `json:"text"`
}

type expect struct {
	Query string `json:"query"`
	Chips []Chip `json:"chips"`
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
	q := scrub.Text(in.Q, scrub.Options{}).Text
	out, _, reject := querynorm.Suggest(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, q)
	var sg querynorm.Suggestion
	if reject == "" {
		sg = querynorm.Validate(q, out, func(slug string) bool { return slices.Contains(in.Brands, slug) })
	}
	for _, ch := range sg.Chips {
		if !slices.Contains(querynorm.Kinds, ch.Kind) {
			return eval.Verdict{Note: "a chip outside the allowed kinds: " + ch.Kind}, nil
		}
	}
	if sg.Query != want.Query {
		return eval.Verdict{Note: "the suggested query differs"}, nil
	}
	if len(sg.Chips) != len(want.Chips) {
		return eval.Verdict{Note: fmt.Sprintf("%d chips, want %d", len(sg.Chips), len(want.Chips))}, nil
	}
	for i, ch := range sg.Chips {
		if ch.Kind != want.Chips[i].Kind || ch.Value != want.Chips[i].Value || ch.Text != want.Chips[i].Text {
			return eval.Verdict{Note: fmt.Sprintf("chip %d differs", i+1)}, nil
		}
	}
	return eval.Verdict{Pass: true}, nil
}
