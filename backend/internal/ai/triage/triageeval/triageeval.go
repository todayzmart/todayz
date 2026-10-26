// Package triageeval is the recorded eval suite of the ticket triage (AI-A2, feature
// ticket_triage, 11 §4): customer and business tickets with their orders. A case passes when
// triage.Write drafts the recorded category and order guess (and a summary only for a long
// thread), or refuses the draft for the expected reason: an invented amount, a phone number or
// an order the model was not shown never reaches the agent. The bar is category accuracy
// (11 §4: ≥ 85%) over the drafts shown.
//
// The recorded replies are synthetic (one seeded wrong category), so recorded mode proves the
// request, the mapping by reference, scrubbing and grounding; make ai-eval measures the model.
package triageeval

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"time"

	"github.com/todayz/backend/internal/ai/eval"
	"github.com/todayz/backend/internal/ai/triage"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

// AccuracyBar is 11 §4's category accuracy bar; the suite's case bar is the same.
const AccuracyBar = 0.85

func init() {
	fx, err := fs.Sub(fixturesFS, "fixtures")
	if err != nil {
		panic(err)
	}
	eval.Register(eval.Suite{Feature: triage.Feature, PassBar: AccuracyBar, Fixtures: fx, Run: run})
}

type order struct {
	Number     string         `json:"number"`
	Status     string         `json:"status"`
	Fulfilment string         `json:"fulfilment"`
	TotalPaise *int64         `json:"totalPaise"`
	PlacedOn   string         `json:"placedOn"`
	Linked     bool           `json:"linked"`
	Timeline   []triage.Event `json:"timeline"`
}

type input struct {
	Number   string           `json:"number"`
	Subject  string           `json:"subject"`
	Opener   string           `json:"opener"`
	Category string           `json:"category"`
	Priority string           `json:"priority"`
	Replies  []triage.Message `json:"replies"`
	Orders   []order          `json:"orders"`
}

type expect struct {
	Category string `json:"category"`
	Order    string `json:"order"` // a ref or none
	Summary  bool   `json:"summary"`
	Reject   string `json:"reject"`
}

// at is a fixed time for the fixture ids (only their uniqueness matters).
var at = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// Ticket is the model's view of a fixture, built as the service builds it (refs o1…, scrubbed).
func Ticket(raw json.RawMessage) (triage.Ticket, error) {
	var f input
	if err := json.Unmarshal(raw, &f); err != nil {
		return triage.Ticket{}, err
	}
	t := triage.Ticket{ID: ids.New(at), Number: f.Number, Subject: f.Subject, Opener: f.Opener, Category: f.Category,
		Priority: f.Priority, Status: "open", Thread: f.Replies}
	for i, o := range f.Orders {
		t.Orders = append(t.Orders, triage.Order{ID: ids.New(at), Ref: fmt.Sprintf("o%d", i+1), Number: o.Number, Status: o.Status,
			Fulfilment: o.Fulfilment, TotalPaise: o.TotalPaise, PlacedOn: o.PlacedOn, Linked: o.Linked, Timeline: o.Timeline})
	}
	return t.Finish(), nil
}

// Result is one case's outcome: the order guess as a ref (none when it named none).
type Result struct {
	Draft triage.Draft
	Order string
}

func write(ctx context.Context, env eval.Env, c eval.Case) (Result, expect, error) {
	var want expect
	if err := json.Unmarshal(c.Expect, &want); err != nil {
		return Result{}, want, err
	}
	t, err := Ticket(c.Input)
	if err != nil {
		return Result{}, want, err
	}
	d, err := triage.Write(ctx, env.LLM, env.Model(settings.AiModelFast), c.Name, t)
	if err != nil {
		return Result{}, want, err
	}
	r := Result{Draft: d, Order: triage.NoOrder}
	if l, ok := d.Output.Line(triage.KindClassify); ok && l.OrderID != nil {
		for _, o := range t.Orders {
			if o.ID.String() == *l.OrderID {
				r.Order = o.Ref
			}
		}
	}
	return r, want, nil
}

func run(ctx context.Context, env eval.Env, c eval.Case) (eval.Verdict, error) {
	r, want, err := write(ctx, env, c)
	if err != nil {
		return eval.Verdict{}, err
	}
	if r.Draft.Reject != want.Reject {
		return eval.Verdict{Note: fmt.Sprintf("rejected %q, want %q", r.Draft.Reject, want.Reject)}, nil
	}
	if want.Reject != "" {
		return eval.Verdict{Pass: len(r.Draft.Output.Lines) == 0, Note: "a refused draft still has lines"}, nil
	}
	l, _ := r.Draft.Output.Line(triage.KindClassify)
	if l.Category != want.Category {
		return eval.Verdict{Note: fmt.Sprintf("category %s, want %s", l.Category, want.Category)}, nil
	}
	if r.Order != want.Order {
		return eval.Verdict{Note: fmt.Sprintf("order %s, want %s", r.Order, want.Order)}, nil
	}
	if _, ok := r.Draft.Output.Line(triage.KindSummary); ok != want.Summary {
		return eval.Verdict{Note: fmt.Sprintf("summary line %v, want %v", ok, want.Summary)}, nil
	}
	if _, ok := r.Draft.Output.Line(triage.KindReply); !ok {
		return eval.Verdict{Note: "no reply line"}, nil
	}
	return eval.Verdict{Pass: true}, nil
}

// Accuracy counts the drafts shown and those with the right category.
type Accuracy struct{ Shown, Right int }

// Score is Right/Shown, 0 when nothing was shown.
func (a Accuracy) Score() float64 {
	if a.Shown == 0 {
		return 0
	}
	return float64(a.Right) / float64(a.Shown)
}

// Measure runs every case and adds up the categories.
func Measure(ctx context.Context, env eval.Env, cases []eval.Case) (Accuracy, error) {
	var a Accuracy
	for _, c := range cases {
		r, want, err := write(ctx, env, c)
		if err != nil {
			return Accuracy{}, fmt.Errorf("%s: %w", c.Name, err)
		}
		if r.Draft.Reject != "" {
			continue
		}
		a.Shown++
		if l, _ := r.Draft.Output.Line(triage.KindClassify); l.Category == want.Category {
			a.Right++
		}
	}
	return a, nil
}
