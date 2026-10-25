// Package voiceeval is the voice stock update's eval suite (feature voice_stock, W3-04): 100
// spoken updates in English, Hindi, Kannada, Telugu, Tamil and mixed speech, each with the
// store's candidate items, the draft lines it should give and a recorded model reply. A case
// passes only when every line is exact (kind, item, quantity, price) or the output is
// refused for the expected reason; the suite's pass bar is 90% (07 W3-04). It runs
// voice.Parse, the same code the draft uses, and writes nothing.
package voiceeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strconv"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/scrub"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/voice"
)

// PassBar is the share of cases that must be exact (07 W3-04).
const PassBar = 0.9

//go:embed fixtures/*.json
var fixturesFS embed.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	eval.Register(eval.Suite{Feature: voice.Feature, PassBar: PassBar, Fixtures: fx, Run: run})
}

// input is a case's input: the transcript after speech to text and the store's candidate
// names in rank order (refs c1, c2, …).
type input struct {
	Transcript string   `json:"transcript"`
	Language   string   `json:"language"`
	Candidates []string `json:"candidates"`
}

// Line is one expected line: item is the candidate ref, or null for "pick item".
type Line struct {
	Kind       string  `json:"kind"`
	Item       *string `json:"item"`
	Quantity   *int    `json:"quantity"`
	PricePaise *int64  `json:"pricePaise"`
}

type expect struct {
	Lines  []Line `json:"lines"`
	Reject string `json:"reject"`
}

// Candidates gives each name a stable item id, so a case's refs map back.
func Candidates(names []string) []voice.Candidate {
	out := make([]voice.Candidate, len(names))
	for i, n := range names {
		var id ids.ID
		id[0], id[15] = 0x01, byte(i+1)
		id[14] = byte((i + 1) >> 8) //settings:ok a byte index in the id
		out[i] = voice.Candidate{Ref: "c" + strconv.Itoa(i+1), ID: id, Name: n}
	}
	return out
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
	cands := Candidates(in.Candidates)
	transcript := scrub.Text(in.Transcript, scrub.Options{}).Text
	p, err := voice.Parse(ctx, env.LLM, voice.Model(env.Model), transcript, cands)
	if err != nil {
		return eval.Verdict{}, err
	}
	if p.Reject != want.Reject {
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", p.Reject, want.Reject)}, nil
	}
	if len(p.Lines) != len(want.Lines) {
		return eval.Verdict{Note: fmt.Sprintf("%d lines, want %d", len(p.Lines), len(want.Lines))}, nil
	}
	refOf := map[string]string{}
	for _, cd := range cands {
		refOf[cd.ID.String()] = cd.Ref
	}
	for i, got := range p.Lines {
		w := want.Lines[i]
		var item *string
		if got.StoreItemID != nil {
			r := refOf[*got.StoreItemID]
			item = &r
		}
		switch {
		case got.Kind != w.Kind:
			return eval.Verdict{Note: fmt.Sprintf("line %d: kind %s, want %s", i+1, got.Kind, w.Kind)}, nil
		case !eqStr(item, w.Item):
			return eval.Verdict{Note: fmt.Sprintf("line %d: item %s, want %s", i+1, show(item), show(w.Item))}, nil
		case !eqInt(got.Quantity, w.Quantity):
			return eval.Verdict{Note: fmt.Sprintf("line %d: quantity differs", i+1)}, nil
		case !eqInt64(got.PricePaise, w.PricePaise):
			return eval.Verdict{Note: fmt.Sprintf("line %d: price differs", i+1)}, nil
		}
	}
	return eval.Verdict{Pass: true}, nil
}

func eqStr(a, b *string) bool  { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
func eqInt(a, b *int) bool     { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
func eqInt64(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func show(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}
