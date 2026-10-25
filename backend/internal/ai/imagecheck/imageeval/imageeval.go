// Package imageeval is the recorded eval suite of the banner image check (AI-R10, feature
// image_check, 11 §1 rule 8): banner images with the verdict and reasons a reviewer would give.
// A case passes when Check returns that verdict and those reasons, or is refused for the
// expected reason (and then holds the banner for review). The images are made offline by
// internal/ai/retail/testdata/gen_fixtures.py and the replies are synthetic.
package imageeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/imagecheck"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json fixtures/images/*.jpg
var fixturesFS embed.FS

var fixtures fs.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	fixtures = fx
	// TODO(lead): 11 names no bar for image_check; conservative reading, every case.
	eval.Register(eval.Suite{Feature: imagecheck.Feature, PassBar: 1, Fixtures: fx, Run: run})
}

type input struct {
	Image string `json:"image"`
}

type expect struct {
	Verdict string   `json:"verdict"`
	Reasons []string `json:"reasons"`
	Reject  string   `json:"reject"`
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
	img, err := fs.ReadFile(fixtures, in.Image)
	if err != nil {
		return eval.Verdict{}, err
	}
	r, err := imagecheck.Check(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name,
		client.Image{MediaType: "image/jpeg", Data: img})
	if err != nil {
		return eval.Verdict{}, err
	}
	switch {
	case r.Reject != want.Reject:
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", r.Reject, want.Reject)}, nil
	case r.Verdict != want.Verdict:
		return eval.Verdict{Note: fmt.Sprintf("verdict %s, want %s", r.Verdict, want.Verdict)}, nil
	case !slices.Equal(r.Reasons, want.Reasons) && r.Reject == "":
		return eval.Verdict{Note: fmt.Sprintf("reasons %v, want %v", r.Reasons, want.Reasons)}, nil
	case r.HoldsForReview() != (want.Verdict != imagecheck.OK || want.Reject != ""):
		return eval.Verdict{Note: "review hold differs"}, nil
	}
	return eval.Verdict{Pass: true}, nil
}
