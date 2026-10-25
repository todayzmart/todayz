// Package billeval is the recorded eval suite of the supplier bill scan (AI-R2, feature
// supplier_bill, 11 §1 rule 8): 60 bill photos, 36 printed and 24 handwritten, in English,
// Hindi and Kannada. Every case runs billscan.ReadBill on the photo and scores its lines
// against the bill's truth. The W3-11 bar is on lines: at least 90% of printed-bill lines and
// 75% of handwritten-bill lines read right (Score). A line is right when its name, quantity,
// cost, MRP and expiry all match; a missed line or an extra line counts as wrong.
//
// The photos and the recorded replies are made offline by
// internal/ai/retail/testdata/gen_fixtures.py. The replies are synthetic (written from the
// truth with seeded reading errors), so recorded mode proves the request, parsing, rupee to
// paise conversion and scoring; a live run (make ai-eval) measures the model on the same photos.
package billeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"unicode"

	"github.com/todayz/backend/internal/ai/billscan"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json fixtures/images/*.jpg
var fixturesFS embed.FS

// Bars are the W3-11 line bars per bill kind.
var Bars = map[string]float64{"printed": 0.90, "handwritten": 0.75}

var fixtures fs.FS

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	fixtures = fx
	// TODO(lead): the plan's bar is on lines (Score, asserted in TestSupplierBillRecorded). The
	// CLI report counts bills instead (a bill passes when its own lines meet its kind's bar);
	// one wrong line fails a short printed bill, so its pass bar is a placeholder of 60% of
	// bills until the lead sets one.
	eval.Register(eval.Suite{Feature: billscan.Feature, PassBar: 0.6, Fixtures: fx, Run: run})
}

// Input is a case's input.
type Input struct {
	Image string `json:"image"` // under fixtures/
	Kind  string `json:"kind"`  // printed | handwritten
	Lang  string `json:"lang"`
}

// Want is one true line of the bill.
type Want struct {
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	CostPaise *int64  `json:"costPaise"`
	MrpPaise  *int64  `json:"mrpPaise"`
	ExpiresOn *string `json:"expiresOn"`
}

type expect struct {
	Lines []Want `json:"lines"`
}

// Lines counts right lines out of the lines that count (the bill's lines plus extras read).
type Lines struct{ Right, Total int }

// Rate is Right/Total, 0 for none.
func (l Lines) Rate() float64 {
	if l.Total == 0 {
		return 0
	}
	return float64(l.Right) / float64(l.Total)
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	in, l, err := score(ctx, env, c)
	if err != nil {
		return eval.Verdict{}, err
	}
	note := fmt.Sprintf("%s: %d of %d lines right", in.Kind, l.Right, l.Total)
	return eval.Verdict{Pass: l.Rate() >= Bars[in.Kind], Note: note}, nil
}

// Score reads every case and returns the lines per bill kind.
func Score(ctx context.Context, env eval.Env, cases []eval.Case) (map[string]Lines, error) {
	out := map[string]Lines{}
	for _, c := range cases {
		in, l, err := score(ctx, env, c)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		t := out[in.Kind]
		t.Right += l.Right
		t.Total += l.Total
		out[in.Kind] = t
	}
	return out, nil
}

func score(ctx context.Context, env eval.Env, c eval.Case) (Input, Lines, error) {
	var in Input
	var want expect
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return in, Lines{}, err
	}
	if _, ok := Bars[in.Kind]; !ok {
		return in, Lines{}, fmt.Errorf("kind %q", in.Kind)
	}
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return in, Lines{}, err
	}
	img, err := fs.ReadFile(fixtures, in.Image)
	if err != nil {
		return in, Lines{}, err
	}
	r, err := billscan.ReadBill(ctx, env.LLM, env.Model(settings.AiModelSmart), c.Name,
		client.Image{MediaType: "image/jpeg", Data: img})
	if err != nil {
		return in, Lines{}, err
	}
	if r.Reject != "" {
		return in, Lines{Total: len(want.Lines)}, nil
	}
	return in, Match(want.Lines, r.Output.Lines), nil
}

// Match pairs each true line with the first unused read line of the same name and counts the
// pairs whose numbers and expiry match too. Unpaired read lines count as wrong.
func Match(want []Want, got []billscan.Line) Lines {
	used := make([]bool, len(got))
	l := Lines{Total: len(want)}
	for _, w := range want {
		for i, g := range got {
			if used[i] || norm(g.Name) != norm(w.Name) {
				continue
			}
			used[i] = true
			if g.Quantity == w.Quantity && eq(g.CostPaise, w.CostPaise) && eq(g.MrpPaise, w.MrpPaise) &&
				eqs(g.ExpiresOn, w.ExpiresOn) {
				l.Right++
			}
			break
		}
	}
	for _, u := range used {
		if !u {
			l.Total++
		}
	}
	return l
}

// norm compares names without case, spaces or punctuation.
func norm(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

func eq(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func eqs(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
