// Package shelfeval is the recorded eval suite of the shelf photo scan (AI-R3, feature
// shelf_photo, 11 §1 rule 8): 40 shelf photos, each with the products on it. Every case runs
// shelfscan.ReadShelf and scores recall: the share of the shelf's products the read names
// (brand, name and size together carry every word of the product). Extra products read are
// counted separately, since the owner confirms each line before anything is created.
//
// The photos and the recorded replies are made offline by
// internal/ai/retail/testdata/gen_fixtures.py; the replies are synthetic (seeded misses), so
// recorded mode proves the request, parsing and scoring; make ai-eval measures the model.
package shelfeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/shelfscan"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json fixtures/images/*.jpg
var fixturesFS embed.FS

// RecallBar is the share of a shelf's products a read must find, over all photos.
// TODO(lead): the plan names no shelf photo bar; 80% recall is a placeholder.
const RecallBar = 0.8

var fixtures fs.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	fixtures = fx
	// A photo passes when its own recall meets the bar.
	eval.Register(eval.Suite{Feature: shelfscan.Feature, PassBar: RecallBar, Fixtures: fx, Run: run})
}

type input struct {
	Image string `json:"image"`
}

type expect struct {
	Products []string `json:"products"`
}

// Found counts the shelf's products the reads found, out of all, and the extra lines read.
type Found struct{ Found, Total, Extra int }

// Recall is Found/Total, 0 for none.
func (f Found) Recall() float64 {
	if f.Total == 0 {
		return 0
	}
	return float64(f.Found) / float64(f.Total)
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	f, err := score(ctx, env, c)
	if err != nil {
		return eval.Verdict{}, err
	}
	return eval.Verdict{Pass: f.Recall() >= RecallBar, Note: fmt.Sprintf("found %d of %d", f.Found, f.Total)}, nil
}

// Score reads every case and adds up what was found.
func Score(ctx context.Context, env eval.Env, cases []eval.Case) (Found, error) {
	var t Found
	for _, c := range cases {
		f, err := score(ctx, env, c)
		if err != nil {
			return Found{}, fmt.Errorf("%s: %w", c.Name, err)
		}
		t.Found += f.Found
		t.Total += f.Total
		t.Extra += f.Extra
	}
	return t, nil
}

func score(ctx context.Context, env eval.Env, c eval.Case) (Found, error) {
	var in input
	var want expect
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return Found{}, err
	}
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return Found{}, err
	}
	img, err := fs.ReadFile(fixtures, in.Image)
	if err != nil {
		return Found{}, err
	}
	r, err := shelfscan.ReadShelf(ctx, env.LLM, env.Model(settings.AiModelSmart), c.Name,
		client.Image{MediaType: "image/jpeg", Data: img})
	if err != nil {
		return Found{}, err
	}
	if r.Reject != "" {
		return Found{Total: len(want.Products)}, nil
	}
	return Match(want.Products, r.Output.Lines), nil
}

// Match pairs each product with the first unused line that carries all of its words.
func Match(want []string, got []shelfscan.Line) Found {
	used := make([]bool, len(got))
	f := Found{Total: len(want)}
	for _, w := range want {
		for i, g := range got {
			if !used[i] && covers(shelfscan.MatchName(g), w) {
				used[i] = true
				f.Found++
				break
			}
		}
	}
	for _, u := range used {
		if !u {
			f.Extra++
		}
	}
	return f
}

func covers(read, want string) bool {
	have := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(read)) {
		have[w] = true
	}
	for _, w := range strings.Fields(strings.ToLower(want)) {
		if !have[w] {
			return false
		}
	}
	return true
}
